package native

import (
	"path/filepath"
	"strings"
	"testing"

	"ostom/internal/format"
)

func TestRecordNativeManifestIncludesAttachmentEvidence(t *testing.T) {
	root := t.TempDir()
	data := []byte("attachment payload")
	message := format.RecoveredMessage{
		BID: 0x42,
		Attachments: []format.RecoveredAttachment{{
			NID: 0x99, Name: "payload.bin", Data: data, ReadableSize: int64(len(data)), Status: "recovered",
		}},
	}
	var report Report
	emlPath := filepath.Join(root, "OSTOM Native Recovered EML", "Inbox", "0000000000000042.eml")
	recordNativeManifest(&report, root, message, "recovered", emlPath, 100, "message-checksum", nil)
	if len(report.ManifestRecords) != 2 {
		t.Fatalf("manifest records = %d, want 2", len(report.ManifestRecords))
	}
	record := report.ManifestRecords[1]
	if record.Kind != "attachment" || record.ReadableBytes != int64(len(data)) || record.ExportedBytes != int64(len(data)) {
		t.Fatalf("unexpected attachment accounting: %+v", record)
	}
	if len(record.SHA256) != 64 || !strings.Contains(record.OutputPath, "0000000000000042.eml") {
		t.Fatalf("missing attachment output evidence: %+v", record)
	}
}
