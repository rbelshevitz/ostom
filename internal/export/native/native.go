// Package native exports bounded native-recovery message candidates.
package native

import (
	"fmt"
	"time"

	"ostom/internal/export/mbox"
	"ostom/internal/format"
)

// Report describes the handoff from native collection to mbox export.
type Report struct {
	Backend               string   `json:"backend"`
	Attempted             int      `json:"attempted"`
	Written               int      `json:"written"`
	Incomplete            int      `json:"incomplete"`
	Attachments           int      `json:"attachments"`
	IncompleteAttachments int      `json:"incomplete_attachments"`
	Failed                int      `json:"failed"`
	Errors                []string `json:"errors,omitempty"`
}

// Limits keeps the native collection pass explicitly bounded at the pipeline
// boundary. It is intentionally separate from the exporter implementation.
type Limits struct {
	MaxNodes        int
	MaxBlocks       int
	MaxBytes        uint64
	MaxRecords      int
	MaxMessages     int
	MaxBodyBytes    int
	MaxErrorSamples int
}

type PipelineReport struct {
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
	collection, err := format.CollectIndexedRecoveredMessagesWithProgress(
		name,
		h,
		fileSize,
		limits.MaxNodes,
		limits.MaxBlocks,
		limits.MaxBytes,
		limits.MaxRecords,
		limits.MaxMessages,
		limits.MaxBodyBytes,
		limits.MaxErrorSamples,
		progress,
	)
	report := PipelineReport{Collection: collection}
	if err != nil {
		return report, err
	}
	report.Export, err = Export(root, collection.Messages)
	return report, err
}

// Export writes collected native candidates into a separate recovery folder.
// The input records are already bounded by the collector; this function does
// not modify the source store and continues after one export error.
func Export(root string, messages []format.RecoveredMessage) (Report, error) {
	report := Report{Backend: "native-recovered"}
	writer, err := mbox.New(root, 0)
	if err != nil {
		return report, fmt.Errorf("create native export: %w", err)
	}
	for _, message := range messages {
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
			attachments = append(attachments, mbox.Attachment{Name: attachment.Name, MimeType: attachment.MimeType, Data: attachment.Data})
		}
		date := time.Time{}
		if message.DateUnix > 0 {
			date = time.Unix(message.DateUnix, 0).UTC()
		}
		err := writer.WriteWithSource(
			[]string{"OSTOM Native Recovered"},
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
			continue
		}
		report.Written++
	}
	return report, nil
}
