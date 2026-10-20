// Package native exports bounded native-recovery message candidates.
package native

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"ostom/internal/export/mbox"
	"ostom/internal/format"
	"ostom/internal/recovery/manifest"
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
}

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
	collection, err := format.CollectIndexedRecoveredMessagesWithAttachmentLimit(
		name,
		h,
		fileSize,
		limits.MaxNodes,
		limits.MaxBlocks,
		limits.MaxBytes,
		limits.MaxRecords,
		limits.MaxMessages,
		limits.MaxBodyBytes,
		attachmentLimit(limits),
		limits.MaxErrorSamples,
		progress,
	)
	report := PipelineReport{Mode: collection.Mode, Collection: collection}
	if err != nil {
		if collection.Cleanup != nil {
			_ = collection.Cleanup()
		}
		return report, err
	}
	defer func() {
		if collection.Cleanup != nil {
			_ = collection.Cleanup()
		}
	}()
	report.Export, err = ExportWithProgressMode(root, collection.Messages, nil, limits.WriteEML)
	markSalvageManifest(&report)
	return report, err
}

// RecoverAndExportWithPhaseProgress reports collection and output separately
// so a user-facing progress indicator never mistakes export work for a stall.
func RecoverAndExportWithPhaseProgress(name, root string, h format.Header, fileSize int64, limits Limits, progress func(phase string, done, total int)) (PipelineReport, error) {
	collection, err := format.CollectIndexedRecoveredMessagesWithAttachmentLimit(
		name, h, fileSize, limits.MaxNodes, limits.MaxBlocks, limits.MaxBytes,
		limits.MaxRecords, limits.MaxMessages, limits.MaxBodyBytes, attachmentLimit(limits), limits.MaxErrorSamples,
		func(done, total int) {
			if progress != nil {
				progress("collecting native candidates", done, total)
			}
		},
	)
	report := PipelineReport{Mode: collection.Mode, Collection: collection}
	if err != nil {
		if collection.Cleanup != nil {
			_ = collection.Cleanup()
		}
		return report, err
	}
	defer func() {
		if collection.Cleanup != nil {
			_ = collection.Cleanup()
		}
	}()
	report.Export, err = ExportWithProgressMode(root, collection.Messages, func(done, total int) {
		if progress != nil {
			progress("writing native recovery output", done, total)
		}
	}, limits.WriteEML)
	markSalvageManifest(&report)
	return report, err
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
