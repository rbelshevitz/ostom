// Package native exports bounded native-recovery message candidates.
package native

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ostom/internal/export/mbox"
	"ostom/internal/format"
	"ostom/internal/recovery/manifest"
	recoverySession "ostom/internal/recovery/session"
)

// Report describes the handoff from native collection to mbox export.
type Report struct {
	Backend               string            `json:"backend"`
	EMLMode               string            `json:"eml_mode"`
	Attempted             int               `json:"attempted"`
	Written               int               `json:"written"`
	Incomplete            int               `json:"incomplete"`
	Attachments           int               `json:"attachments"`
	IncompleteAttachments int               `json:"incomplete_attachments"`
	EMLWritten            int               `json:"eml_written"`
	EMLFailed             int               `json:"eml_failed"`
	Failed                int               `json:"failed"`
	Status                string            `json:"status"`
	Summary               []string          `json:"summary,omitempty"`
	Errors                []string          `json:"errors,omitempty"`
	ManifestRecords       []manifest.Record `json:"-"`
}

// Limits keeps the native collection pass explicitly bounded at the pipeline
// boundary. It is intentionally separate from the exporter implementation.
type Limits struct {
	MaxNodes           int
	MaxBlocks          int
	MaxBytes           uint64
	MaxRecords         int
	MaxMessages        int
	MaxBodyBytes       int
	MaxAttachmentBytes int
	MaxErrorSamples    int
	WriteEML           bool
	WorkDir            string
	JournalPath        string
}

const (
	streamBatchMessages = 64
	streamBatchBytes    = 8 << 20
)

type PipelineReport struct {
	Mode       string                             `json:"mode"`
	Collection format.IndexedRecoveredMessageScan `json:"collection"`
	Export     Report                             `json:"export"`
}

// RecoverAndExport is the native vertical pipeline: collect bounded message
// candidates from the indexed store, then write them to a separate mbox tree.
func RecoverAndExport(name, root string, h format.Header, fileSize int64, limits Limits) (PipelineReport, error) {
	return RecoverAndExportWithProgress(name, root, h, fileSize, limits, nil)
}

// RecoverAndExportWithProgress is the CLI-facing form with native collection
// progress checkpoints.
func RecoverAndExportWithProgress(name, root string, h format.Header, fileSize int64, limits Limits, progress func(done, total int)) (PipelineReport, error) {
	return recoverAndExportStreaming(name, root, h, fileSize, limits, func(done, total int) {
		if progress != nil {
			progress(done, total)
		}
	}, nil)
}

// RecoverAndExportWithPhaseProgress reports collection and output separately
// so a user-facing progress indicator never mistakes export work for a stall.
func RecoverAndExportWithPhaseProgress(name, root string, h format.Header, fileSize int64, limits Limits, progress func(phase string, done, total int)) (PipelineReport, error) {
	return recoverAndExportStreaming(name, root, h, fileSize, limits, nil, progress)
}

// recoverAndExportStreaming keeps at most one recovered message in memory.
// The legacy collection API remains available for callers that need the full
// result list, while the CLI uses this path for large stores.
func recoverAndExportStreaming(name, root string, h format.Header, fileSize int64, limits Limits, simpleProgress func(done, total int), phaseProgress func(phase string, done, total int)) (PipelineReport, error) {
	if limits.WorkDir == "" {
		limits.WorkDir = filepath.Join(filepath.Dir(root), ".ostom", "native-work")
	}
	if err := os.MkdirAll(limits.WorkDir, 0o700); err != nil {
		return PipelineReport{}, fmt.Errorf("create native work area: %w", err)
	}
	if err := discardUncommittedBatches(limits.WorkDir); err != nil {
		return PipelineReport{}, err
	}
	committedBatches, completedIdentities := loadCommittedBatches(limits.WorkDir)
	var journal *recoverySession.Journal
	var err error
	if limits.JournalPath != "" {
		journal, err = recoverySession.OpenJournal(limits.JournalPath)
		if err != nil {
			return PipelineReport{}, err
		}
		defer journal.Close()
		completed, completedErr := recoverySession.Completed(limits.JournalPath)
		if completedErr != nil {
			return PipelineReport{}, completedErr
		}
		for identity, done := range completed {
			if done {
				completedIdentities[identity] = true
			}
		}
		if err := reconcileCommittedJournal(journal, committedBatches, completed); err != nil {
			return PipelineReport{}, err
		}
	}
	exportReport := Report{Backend: "native-recovered", EMLMode: "none"}
	manifestPath := filepath.Join(limits.WorkDir, "native-manifest.jsonl")
	if limits.WriteEML {
		exportReport.EMLMode = "all"
	}
	exported := 0
	batch := make([]format.RecoveredMessage, 0, streamBatchMessages)
	batchBytes := 0
	batchNumber := 0
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		batchesDir := filepath.Join(limits.WorkDir, "batches")
		if err := os.MkdirAll(batchesDir, 0o700); err != nil {
			return fmt.Errorf("create native batch area: %w", err)
		}
		var batchDir string
		for {
			batchNumber++
			batchDir = filepath.Join(batchesDir, fmt.Sprintf("%06d", batchNumber))
			if _, statErr := os.Stat(batchDir); os.IsNotExist(statErr) {
				break
			}
		}
		stagingDir, err := os.MkdirTemp(batchesDir, ".writing-")
		if err != nil {
			return fmt.Errorf("create native batch staging area: %w", err)
		}
		defer os.RemoveAll(stagingDir)
		batchRoot := filepath.Join(stagingDir, "output", filepath.Base(root))
		one, err := ExportWithProgressMode(batchRoot, batch, nil, limits.WriteEML)
		if err != nil {
			return err
		}
		bytes, checksum, err := recoverySession.DigestArtifact(stagingDir, "output")
		if err != nil {
			return fmt.Errorf("verify native batch: %w", err)
		}
		if err := writeManifestFile(filepath.Join(stagingDir, "manifest.jsonl"), one.ManifestRecords); err != nil {
			return fmt.Errorf("write native batch manifest: %w", err)
		}
		manifestBytes, manifestChecksum, err := recoverySession.DigestArtifact(stagingDir, "manifest.jsonl")
		if err != nil {
			return fmt.Errorf("verify native batch manifest: %w", err)
		}
		identities := make([]string, 0, len(batch))
		for _, record := range one.ManifestRecords {
			if record.Kind == "message" && record.Status != "skipped" && record.Identity != "" {
				identities = append(identities, record.Identity)
			}
		}
		if len(identities) == 0 {
			identities = append(identities, "")
		}
		record := recoverySession.Batch{
			Number: batchNumber, Status: "committed", Artifact: "output",
			FirstIdentity: identities[0], LastIdentity: identities[len(identities)-1],
			Identities: identities, Items: len(batch), Bytes: bytes, SHA256: checksum,
			Manifest: "manifest.jsonl", ManifestBytes: manifestBytes, ManifestSHA256: manifestChecksum,
		}
		if err := recoverySession.WriteBatchRecord(stagingDir, record); err != nil {
			return fmt.Errorf("commit native batch: %w", err)
		}
		if err := recoverySession.VerifyArtifact(record, stagingDir); err != nil {
			return fmt.Errorf("recheck native batch: %w", err)
		}
		if err := os.Rename(stagingDir, batchDir); err != nil {
			return fmt.Errorf("finalize native batch: %w", err)
		}
		if journal != nil {
			for _, item := range one.ManifestRecords {
				if err := appendJournalRecord(journal, item); err != nil {
					return err
				}
			}
		}
		committedBatches = append(committedBatches, batchDir)
		mergeReports(&exportReport, &one)
		exported += len(batch)
		for index := range batch {
			batch[index] = format.RecoveredMessage{}
		}
		batch = batch[:0]
		batchBytes = 0
		if phaseProgress != nil {
			phaseProgress("writing native recovery output", exported, 0)
		} else if simpleProgress != nil {
			simpleProgress(exported, 0)
		}
		return err
	}
	consume := func(message format.RecoveredMessage) error {
		identity := fmt.Sprintf("native-bid-%016X", message.BID)
		if completedIdentities[identity] {
			return nil
		}
		if journal != nil {
			if err := journal.Append(recoverySession.ItemEntry{
				Source: name, Identity: identity, Kind: "message", Route: "native-recovered",
				Folder: filepath.Join(message.FolderPath...), MessageID: message.MessageID, Status: "in_progress",
			}); err != nil {
				return err
			}
		}
		messageBytes := len(message.Body)
		for _, attachment := range message.Attachments {
			messageBytes += len(attachment.Data)
		}
		batch = append(batch, message)
		batchBytes += messageBytes
		if len(batch) >= streamBatchMessages || batchBytes >= streamBatchBytes {
			return flush()
		}
		return nil
	}
	collectionProgress := func(done, total int) {
		if phaseProgress != nil {
			phaseProgress("collecting native candidates", done, total)
		} else if simpleProgress != nil {
			simpleProgress(done, total)
		}
	}
	collection, err := format.CollectIndexedRecoveredMessagesWithConsumerInDir(
		name, h, fileSize, limits.MaxNodes, limits.MaxBlocks, limits.MaxBytes,
		limits.MaxRecords, limits.MaxMessages, limits.MaxBodyBytes, attachmentLimit(limits),
		limits.MaxErrorSamples, collectionProgress, limits.WorkDir, consume,
	)
	report := PipelineReport{Mode: collection.Mode, Collection: collection, Export: exportReport}
	if err != nil {
		_ = flush()
		_ = assembleManifest(manifestPath, committedBatches)
		if collection.Cleanup != nil {
			_ = collection.Cleanup()
		}
		return report, err
	}
	if err := flush(); err != nil {
		_ = assembleManifest(manifestPath, committedBatches)
		if collection.Cleanup != nil {
			_ = collection.Cleanup()
		}
		return report, err
	}
	if err := publishBatches(root, limits.WorkDir, committedBatches, limits.WriteEML); err != nil {
		_ = assembleManifest(manifestPath, committedBatches)
		return report, err
	}
	if err := assembleManifest(manifestPath, committedBatches); err != nil {
		return report, err
	}
	if collection.Cleanup != nil {
		_ = collection.Cleanup()
	}
	finishReport(&report.Export)
	if report.Mode == "salvage" {
		if err := markSalvageManifestFiles(manifestPath, committedBatches); err != nil {
			return report, err
		}
	}
	return report, nil
}

func appendJournalRecord(journal *recoverySession.Journal, record manifest.Record) error {
	status := "failed"
	switch record.Status {
	case "recovered", "exported":
		status = "exported"
	case "partial":
		status = "partial"
	case "skipped":
		status = "skipped"
	}
	errText := ""
	if len(record.Errors) > 0 {
		errText = strings.Join(record.Errors, "; ")
	}
	return journal.Append(recoverySession.ItemEntry{
		Source: record.Source, Identity: record.Identity, Kind: record.Kind, Route: record.Route,
		Folder: record.FolderPath, MessageID: record.MessageID, Status: status,
		DeclaredBytes: record.DeclaredBytes, ReadableBytes: record.ReadableBytes,
		ExportedBytes: record.ExportedBytes, SHA256: record.SHA256, OutputPath: record.OutputPath,
		Error: errText,
	})
}

func reconcileCommittedJournal(journal *recoverySession.Journal, batches []string, completed map[string]bool) error {
	for _, batchDir := range batches {
		record, err := recoverySession.ReadBatchRecord(batchDir)
		if err != nil {
			return fmt.Errorf("read committed native batch: %w", err)
		}
		records, err := ReadManifest(filepath.Join(batchDir, record.Manifest))
		if err != nil {
			return fmt.Errorf("read committed native batch manifest: %w", err)
		}
		for _, item := range records {
			if item.Identity == "" || completed[item.Identity] {
				continue
			}
			if err := appendJournalRecord(journal, item); err != nil {
				return err
			}
			completed[item.Identity] = item.Status == "recovered" || item.Status == "exported"
		}
	}
	return nil
}

func loadCommittedBatches(workDir string) ([]string, map[string]bool) {
	completed := make(map[string]bool)
	paths := make([]string, 0)
	entries, err := os.ReadDir(filepath.Join(workDir, "batches"))
	if err != nil {
		return paths, completed
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		directory := filepath.Join(workDir, "batches", entry.Name())
		record, err := recoverySession.ReadBatchRecord(directory)
		if err != nil || record.Status != "committed" || record.Manifest == "" || recoverySession.VerifyArtifact(record, directory) != nil {
			continue
		}
		paths = append(paths, directory)
		for _, identity := range record.Identities {
			completed[identity] = true
		}
	}
	sort.Strings(paths)
	return paths, completed
}

func discardUncommittedBatches(workDir string) error {
	entries, err := os.ReadDir(filepath.Join(workDir, "batches"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect native batches: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || len(entry.Name()) < len(".writing-") || entry.Name()[:len(".writing-")] != ".writing-" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(workDir, "batches", entry.Name())); err != nil {
			return fmt.Errorf("discard incomplete native batch: %w", err)
		}
	}
	return nil
}

func mergeReports(dst, src *Report) {
	dst.Attempted += src.Attempted
	dst.Written += src.Written
	dst.Incomplete += src.Incomplete
	dst.Attachments += src.Attachments
	dst.IncompleteAttachments += src.IncompleteAttachments
	dst.EMLWritten += src.EMLWritten
	dst.EMLFailed += src.EMLFailed
	dst.Failed += src.Failed
	for _, message := range src.Errors {
		appendExportErrorText(dst, message)
	}
}

func writeManifestFile(path string, records []manifest.Record) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	for _, record := range records {
		if err := encoder.Encode(record); err != nil {
			_ = file.Close()
			return err
		}
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func assembleManifest(path string, batches []string) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".native-manifest-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	for _, batchDir := range batches {
		input, err := os.Open(filepath.Join(batchDir, "manifest.jsonl"))
		if err != nil {
			_ = temporary.Close()
			return err
		}
		_, copyErr := io.Copy(temporary, input)
		closeErr := input.Close()
		if copyErr != nil {
			_ = temporary.Close()
			return copyErr
		}
		if closeErr != nil {
			_ = temporary.Close()
			return closeErr
		}
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

// ReadManifest loads the durable native manifest only when the caller needs
// to create the final combined report. Export itself never retains these rows.
func ReadManifest(path string) ([]manifest.Record, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var records []manifest.Record
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		var record manifest.Record
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return nil, err
		}
		record.Errors = append([]string(nil), record.Errors...)
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

// StreamManifest visits native manifest records one at a time. It keeps the
// final inventory bounded when the caller is assembling the user-facing
// manifest for a large recovery.
func StreamManifest(path string, visit func(manifest.Record) error) error {
	if visit == nil {
		return fmt.Errorf("native manifest visitor is nil")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		var record manifest.Record
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return err
		}
		if err := visit(record); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func appendExportErrorText(report *Report, message string) {
	if len(report.Errors) < 32 {
		report.Errors = append(report.Errors, message)
	}
}

func finishReport(report *Report) {
	report.Status = "completed"
	if report.Failed > 0 || report.EMLFailed > 0 {
		report.Status = "completed with output errors"
	}
	report.Summary = []string{
		fmt.Sprintf("found %d native message candidates", report.Attempted),
		fmt.Sprintf("wrote %d messages to mbox and %d messages to EML", report.Written, report.EMLWritten),
		fmt.Sprintf("found %d attachments; %d were partial or unavailable", report.Attachments, report.IncompleteAttachments),
	}
}

func publishBatches(root, workDir string, batches []string, includeEML bool) error {
	if len(batches) == 0 {
		return nil
	}
	publishDir, err := os.MkdirTemp(workDir, "native-publish-")
	if err != nil {
		return fmt.Errorf("create native publish area: %w", err)
	}
	defer os.RemoveAll(publishDir)
	stagedRoot := filepath.Join(publishDir, filepath.Base(root))
	for _, batchDir := range batches {
		source := filepath.Join(batchDir, "output", filepath.Base(root))
		if err := appendTree(source, stagedRoot); err != nil {
			return fmt.Errorf("assemble native batches: %w", err)
		}
	}
	if _, _, err := recoverySession.DigestArtifact(publishDir, filepath.Base(root)); err != nil {
		return fmt.Errorf("verify assembled native output: %w", err)
	}
	names := []string{"OSTOM Native Recovered"}
	if includeEML {
		names = append(names, "OSTOM Native Recovered EML")
	}
	for _, name := range names {
		staged := filepath.Join(stagedRoot, name)
		if _, err := os.Stat(staged); os.IsNotExist(err) {
			continue
		}
		if err := replaceTree(filepath.Join(root, name), staged, workDir); err != nil {
			return fmt.Errorf("publish native result: %w", err)
		}
	}
	return nil
}

func appendTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := destination
		if relative != "." {
			target = filepath.Join(destination, relative)
		}
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			_ = input.Close()
			return err
		}
		_, copyErr := io.Copy(output, input)
		syncErr := output.Sync()
		closeOutErr := output.Close()
		closeInErr := input.Close()
		for _, candidate := range []error{copyErr, syncErr, closeOutErr, closeInErr} {
			if candidate != nil {
				return candidate
			}
		}
		return nil
	})
}

func replaceTree(target, staged, workDir string) error {
	backup := filepath.Join(workDir, ".native-old-"+filepath.Base(target))
	_ = os.RemoveAll(backup)
	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, backup); err != nil {
			return err
		}
	}
	if err := os.Rename(staged, target); err != nil {
		if _, backupErr := os.Stat(backup); backupErr == nil {
			_ = os.Rename(backup, target)
		}
		return err
	}
	return os.RemoveAll(backup)
}

func markSalvageManifestFiles(path string, batches []string) error {
	for _, batchDir := range batches {
		manifestFile := filepath.Join(batchDir, "manifest.jsonl")
		if err := rewriteSalvageManifestFile(manifestFile); err != nil {
			return err
		}
		record, err := recoverySession.ReadBatchRecord(batchDir)
		if err != nil {
			return err
		}
		record.ManifestBytes, record.ManifestSHA256, err = recoverySession.DigestArtifact(batchDir, "manifest.jsonl")
		if err != nil {
			return err
		}
		if err := recoverySession.WriteBatchRecord(batchDir, record); err != nil {
			return err
		}
		if err := recoverySession.VerifyArtifact(record, batchDir); err != nil {
			return err
		}
	}
	return assembleManifest(path, batches)
}

func rewriteSalvageManifestFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	temporary, err := os.CreateTemp(filepath.Dir(path), ".native-salvage-manifest-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	encoder := json.NewEncoder(temporary)
	for scanner.Scan() {
		var record manifest.Record
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			_ = temporary.Close()
			return err
		}
		record.Source = "native-salvage"
		record.Route = "native-salvage"
		if err := encoder.Encode(record); err != nil {
			_ = temporary.Close()
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func attachmentLimit(limits Limits) int {
	if limits.MaxAttachmentBytes > 0 {
		return limits.MaxAttachmentBytes
	}
	return limits.MaxBodyBytes
}

// Export writes collected native candidates into a separate recovery folder.
// The input records are already bounded by the collector; this function does
// not modify the source store and continues after one export error.
func Export(root string, messages []format.RecoveredMessage) (Report, error) {
	return ExportWithProgress(root, messages, nil)
}

// ExportWithProgress writes mbox and per-message EML output. Each callback
// checkpoint is one message attempt, including partial and failed output.
func ExportWithProgress(root string, messages []format.RecoveredMessage, progress func(done, total int)) (Report, error) {
	return ExportWithProgressMode(root, messages, progress, true)
}

// ExportWithProgressMode keeps mbox as the primary output and optionally
// writes the per-message EML mirror. The legacy function above preserves the
// original library behavior for callers that explicitly use it.
func ExportWithProgressMode(root string, messages []format.RecoveredMessage, progress func(done, total int), includeEML bool) (Report, error) {
	report := Report{Backend: "native-recovered", EMLMode: "none"}
	if includeEML {
		report.EMLMode = "all"
	}
	writer, err := mbox.New(root, 0)
	if err != nil {
		return report, fmt.Errorf("create native export: %w", err)
	}
	if progress != nil {
		progress(0, len(messages))
	}
	for index, message := range messages {
		report.Attempted++
		if message.Incomplete {
			report.Incomplete++
		}
		attachments := make([]mbox.Attachment, 0, len(message.Attachments))
		for _, attachment := range message.Attachments {
			report.Attachments++
			if attachment.Incomplete {
				report.IncompleteAttachments++
			}
			attachments = append(attachments, mbox.Attachment{Name: attachment.Name, MimeType: attachment.MimeType, ContentID: attachment.ContentID, Inline: attachment.Inline, Data: attachment.Data, Open: attachment.Open, Size: attachment.ReadableSize})
		}
		date := time.Time{}
		if message.DateUnix > 0 {
			date = time.Unix(message.DateUnix, 0).UTC()
		}
		folderPath := append([]string{"OSTOM Native Recovered"}, message.FolderPath...)
		err := writer.WriteWithSource(
			folderPath,
			message.Subject,
			message.Sender,
			"",
			"",
			fmt.Sprintf("<ostom-native-%X@local>", message.BID),
			date,
			string(message.Body),
			attachments,
			"native-recovered",
		)
		if err != nil {
			report.Failed++
			if len(report.Errors) < 32 {
				report.Errors = append(report.Errors, fmt.Sprintf("BID 0x%X: %v", message.BID, err))
			}
			recordNativeManifest(&report, root, message, "skipped", "", 0, "", []string{err.Error()})
			if progress != nil {
				progress(index+1, len(messages))
			}
			continue
		}
		report.Written++
		if !includeEML {
			status := "recovered"
			if message.Incomplete {
				status = "partial"
			}
			recordNativeManifest(&report, root, message, status, "", 0, "", nil)
			if progress != nil {
				progress(index+1, len(messages))
			}
			continue
		}
		var checksum string
		emlPath, exportedBytes, writeErr := writeEML(root, message.BID, func(out io.Writer) error {
			digest := sha256.New()
			hashWriter := io.MultiWriter(out, digest)
			err := writer.WriteEMLWithSource(
				hashWriter,
				message.Subject,
				message.Sender,
				"",
				"",
				fmt.Sprintf("<ostom-native-%X@local>", message.BID),
				date,
				string(message.Body),
				attachments,
				"native-recovered",
			)
			if err == nil {
				checksum = fmt.Sprintf("%x", digest.Sum(nil))
			}
			return err
		})
		if writeErr != nil {
			report.EMLFailed++
			appendExportError(&report, message.BID, "write EML", writeErr)
			recordNativeManifest(&report, root, message, "partial", "", 0, "", []string{"write EML: " + writeErr.Error()})
			if progress != nil {
				progress(index+1, len(messages))
			}
			continue
		}
		report.EMLWritten++
		status := "recovered"
		if message.Incomplete {
			status = "partial"
		}
		recordNativeManifest(&report, root, message, status, emlPath, exportedBytes, checksum, nil)
		if progress != nil {
			progress(index+1, len(messages))
		}
	}
	report.Status = "completed"
	if report.Failed > 0 || report.EMLFailed > 0 {
		report.Status = "completed with output errors"
	}
	report.Summary = []string{
		fmt.Sprintf("found %d native message candidates", report.Attempted),
		fmt.Sprintf("wrote %d messages to mbox and %d messages to EML", report.Written, report.EMLWritten),
		fmt.Sprintf("found %d attachments; %d were partial or unavailable", report.Attachments, report.IncompleteAttachments),
	}
	return report, nil
}

func writeEML(root string, bid uint64, write func(io.Writer) error) (string, int64, error) {
	dir := filepath.Join(root, "OSTOM Native Recovered EML")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, err
	}
	path := filepath.Join(dir, fmt.Sprintf("%016X.eml", bid))
	temp, err := os.CreateTemp(dir, fmt.Sprintf(".%016X-*.eml.tmp", bid))
	if err != nil {
		return "", 0, err
	}
	tempPath := temp.Name()
	cleanup := func() {
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}
	if err := write(temp); err != nil {
		cleanup()
		return "", 0, err
	}
	if err := temp.Sync(); err != nil {
		cleanup()
		return "", 0, err
	}
	info, err := temp.Stat()
	if err != nil {
		cleanup()
		return "", 0, err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return "", 0, err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		_ = os.Remove(tempPath)
		return "", 0, err
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return "", 0, err
	}
	return path, info.Size(), nil
}

func appendExportError(report *Report, bid uint64, action string, err error) {
	if len(report.Errors) < 32 {
		report.Errors = append(report.Errors, fmt.Sprintf("BID 0x%X %s: %v", bid, action, err))
	}
}

func recordNativeManifest(report *Report, root string, message format.RecoveredMessage, status, emlPath string, exportedBytes int64, checksum string, errors []string) {
	messageErrors := append([]string(nil), message.Errors...)
	messageErrors = append(messageErrors, errors...)
	outputPath := ""
	if emlPath != "" {
		outputPath = filepath.Join(filepath.Base(root), "OSTOM Native Recovered EML", filepath.Base(emlPath))
	}
	report.ManifestRecords = append(report.ManifestRecords, manifest.Record{
		Kind: "message", Source: "native", Identity: fmt.Sprintf("native-bid-%016X", message.BID), Route: "native-recovered", MessageID: message.MessageID,
		NID: fmt.Sprintf("0x%X", message.NID), BID: fmt.Sprintf("0x%X", message.BID), Status: status,
		FolderPath: filepath.Join(message.FolderPath...), ReadableBytes: int64(len(message.Body)), ExportedBytes: exportedBytes, SHA256: checksum, OutputPath: outputPath, Errors: messageErrors,
	})
	for _, attachment := range message.Attachments {
		attachmentStatus := attachment.Status
		readableBytes := attachment.ReadableSize
		if readableBytes == 0 && len(attachment.Data) > 0 {
			readableBytes = int64(len(attachment.Data))
		}
		if attachmentStatus == "" {
			if readableBytes == 0 {
				attachmentStatus = "unavailable"
			} else if attachment.Incomplete {
				attachmentStatus = "partial"
			} else {
				attachmentStatus = "recovered"
			}
		}
		var checksum string
		if len(attachment.Data) > 0 {
			sum := sha256.Sum256(attachment.Data)
			checksum = fmt.Sprintf("%x", sum[:])
		} else if attachment.Open != nil {
			if opened, err := attachment.Open(); err == nil {
				hash := sha256.New()
				if _, err := io.Copy(hash, opened); err == nil {
					checksum = fmt.Sprintf("%x", hash.Sum(nil))
				}
				_ = opened.Close()
			}
		}
		exportedBytes := int64(0)
		outputPath := ""
		if emlPath != "" && readableBytes > 0 && attachmentStatus != "unavailable" {
			exportedBytes = readableBytes
			outputPath = filepath.Join(filepath.Base(root), "OSTOM Native Recovered EML", filepath.Base(emlPath))
		}
		recordErrors := append([]string(nil), attachment.Errors...)
		report.ManifestRecords = append(report.ManifestRecords, manifest.Record{
			Kind: "attachment", Source: "native", Identity: fmt.Sprintf("native-bid-%016X-nid-%X", message.BID, attachment.NID), Route: "native-recovered",
			NID: fmt.Sprintf("0x%X", attachment.NID), BID: fmt.Sprintf("0x%X", message.BID), Status: attachmentStatus,
			DeclaredBytes: attachment.DeclaredSize, ReadableBytes: readableBytes, ExportedBytes: exportedBytes,
			SHA256: checksum, OutputPath: outputPath, Errors: recordErrors,
		})
	}
}
