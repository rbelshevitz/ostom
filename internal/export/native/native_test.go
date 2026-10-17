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
	report, err := Export(root, []format.RecoveredMessage{{
		BID:         0x200,
		Subject:     "Recovered subject",
		Sender:      "sender@example.test",
		DateUnix:    1704067200,
		Body:        []byte("hello\nFrom body"),
		Incomplete:  true,
		Attachments: []format.RecoveredAttachment{{Name: "a.txt", MimeType: "text/plain", Data: []byte("abc")}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Attempted != 1 || report.Written != 1 || report.Incomplete != 1 || report.Failed != 0 {
		t.Fatalf("unexpected native export report: %+v", report)
	}
	data, err := os.ReadFile(filepath.Join(root, "OSTOM Native Recovered"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"Subject: Recovered subject", "Date: Mon, 01 Jan 2024 00:00:00 +0000", "X-Ostom-Source: native-recovered", "a.txt", ">From body", "Message-ID: <ostom-native-200@local>"} {
		if !strings.Contains(text, want) {
			t.Fatalf("native mbox missing %q: %s", want, text)
		}
	}
}
