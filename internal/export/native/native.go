// Package native exports bounded native-recovery message candidates.
package native

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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
	committedBatches, completedIdentities := loadCommittedBatches(limits.WorkDir)
	exportReport := Report{Backend: "native-recovered", EMLMode: "none"}
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
		var batchDir string
		for {
			batchNumber++
			batchDir = filepath.Join(limits.WorkDir, "batches", fmt.Sprintf("%06d", batchNumber))
			if _, statErr := os.Stat(batchDir); os.IsNotExist(statErr) {
				break
			}
		}
		if err := os.MkdirAll(batchDir, 0o700); err != nil {
			return fmt.Errorf("create native batch: %w", err)
		}
		batchRoot := filepath.Join(batchDir, "output", filepath.Base(root))
		one, err := ExportWithProgressMode(batchRoot, batch, nil, limits.WriteEML)
		if err != nil {
			return err
		}
		bytes, checksum, err := recoverySession.DigestArtifact(batchDir, "output")
		if err != nil {
			return fmt.Errorf("verify native batch: %w", err)
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
		}
		if err := recoverySession.WriteBatchRecord(batchDir, record); err != nil {
			return fmt.Errorf("commit native batch: %w", err)
		}
		if err := recoverySession.VerifyArtifact(record, batchDir); err != nil {
			return fmt.Errorf("recheck native batch: %w", err)
		}
		committedBatches = append(committedBatches, batchDir)
		mergeReports(&exportReport, &one)
		exported += len(batch)
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
		if collection.Cleanup != nil {
			_ = collection.Cleanup()
		}
		return report, err
	}
	if err := flush(); err != nil {
		if collection.Cleanup != nil {
			_ = collection.Cleanup()
		}
		return report, err
	}
	if err := publishBatches(root, limits.WorkDir, committedBatches, limits.WriteEML); err != nil {
		return report, err
	}
	if collection.Cleanup != nil {
		_ = collection.Cleanup()
	}
	finishReport(&report.Export)
	markSalvageManifest(&report)
	return report, nil
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
		if err != nil || record.Status != "committed" || recoverySession.VerifyArtifact(record, directory) != nil {
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

func mergeReports(dst, src *Report) {
	dst.Attempted += src.Attempted
	dst.Written += src.Written
	dst.Incomplete += src.Incomplete
	dst.Attachments += src.Attachments
	dst.IncompleteAttachments += src.IncompleteAttachments
	dst.EMLWritten += src.EMLWritten
	dst.EMLFailed += src.EMLFailed
	dst.Failed += src.Failed
	dst.ManifestRecords = append(dst.ManifestRecords, src.ManifestRecords...)
	for _, message := range src.Errors {
		appendExportErrorText(dst, message)
	}
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

func markSalvageManifest(report *PipelineReport) {
	if report == nil || report.Mode != "salvage" {
		return
	}
	for index := range report.Export.ManifestRecords {
		report.Export.ManifestRecords[index].Source = "native-salvage"
		report.Export.ManifestRecords[index].Route = "native-salvage"
	}
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
		raw, renderErr := writer.RenderWithSource(
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
		if renderErr != nil {
			report.EMLFailed++
			appendExportError(&report, message.BID, "render EML", renderErr)
			recordNativeManifest(&report, root, message, "partial", "", 0, "", []string{"render EML: " + renderErr.Error()})
			if progress != nil {
				progress(index+1, len(messages))
			}
			continue
		}
		emlPath, err := writeEML(root, message.BID, raw)
		if err != nil {
			report.EMLFailed++
			appendExportError(&report, message.BID, "write EML", err)
			recordNativeManifest(&report, root, message, "partial", "", 0, "", []string{"write EML: " + err.Error()})
			if progress != nil {
				progress(index+1, len(messages))
			}
			continue
		}
		report.EMLWritten++
		sum := sha256.Sum256(raw)
		status := "recovered"
		if message.Incomplete {
			status = "partial"
		}
		recordNativeManifest(&report, root, message, status, emlPath, int64(len(raw)), fmt.Sprintf("%x", sum[:]), nil)
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

func writeEML(root string, bid uint64, data []byte) (string, error) {
	dir := filepath.Join(root, "OSTOM Native Recovered EML")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("%016X.eml", bid))
	return path, os.WriteFile(path, data, 0o644)
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
		Kind: "message", Source: "native", Identity: fmt.Sprintf("native-bid-%016X", message.BID), Route: "native-recovered",
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
