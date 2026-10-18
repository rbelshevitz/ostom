package native

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ostom/internal/format"
)

func TestExportRecoveredMessages(t *testing.T) {
	root := t.TempDir()
	var progress [][2]int
	report, err := ExportWithProgress(root, []format.RecoveredMessage{{
		BID:         0x200,
		Subject:     "Recovered subject",
		Sender:      "sender@example.test",
		DateUnix:    1704067200,
		Body:        []byte("hello\nFrom body"),
		Incomplete:  true,
		Attachments: []format.RecoveredAttachment{{Name: "a.txt", MimeType: "text/plain", ContentID: "cid-1", Inline: true, Data: []byte("abc")}},
	}}, func(done, total int) { progress = append(progress, [2]int{done, total}) })
	if err != nil {
		t.Fatal(err)
	}
	if report.Attempted != 1 || report.Written != 1 || report.EMLWritten != 1 || report.EMLFailed != 0 || report.Incomplete != 1 || report.Failed != 0 || report.Status != "completed" || len(report.Summary) != 3 {
		t.Fatalf("unexpected native export report: %+v", report)
	}
	if len(report.ManifestRecords) != 2 {
		t.Fatalf("native manifest records = %+v, want message and attachment", report.ManifestRecords)
	}
	messageRecord, attachmentRecord := report.ManifestRecords[0], report.ManifestRecords[1]
	if messageRecord.Kind != "message" || messageRecord.Status != "partial" || messageRecord.BID != "0x200" || messageRecord.SHA256 == "" || messageRecord.OutputPath == "" {
		t.Fatalf("unexpected native message manifest record: %+v", messageRecord)
	}
	if attachmentRecord.Kind != "attachment" || attachmentRecord.Status != "recovered" || attachmentRecord.ReadableBytes != 3 {
		t.Fatalf("unexpected native attachment manifest record: %+v", attachmentRecord)
	}
	if len(progress) != 2 || progress[0] != [2]int{0, 1} || progress[1] != [2]int{1, 1} {
		t.Fatalf("unexpected native export progress: %+v", progress)
	}
	data, err := os.ReadFile(filepath.Join(root, "OSTOM Native Recovered"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"Subject: Recovered subject", "Date: Mon, 01 Jan 2024 00:00:00 +0000", "X-Ostom-Source: native-recovered", "a.txt", "Content-Id: <cid-1>", "Content-Disposition: inline", ">From body", "Message-ID: <ostom-native-200@local>"} {
		if !strings.Contains(text, want) {
			t.Fatalf("native mbox missing %q: %s", want, text)
		}
	}
	eml, err := os.ReadFile(filepath.Join(root, "OSTOM Native Recovered EML", "0000000000000200.eml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Subject: Recovered subject", "X-Ostom-Source: native-recovered", "a.txt", "From body"} {
		if !strings.Contains(string(eml), want) {
			t.Fatalf("native EML missing %q: %s", want, eml)
		}
	}
}
