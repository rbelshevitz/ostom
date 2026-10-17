package gopst

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This test uses the local parser-validated fixture when it is available.
// The fixture is intentionally ignored by git because it is generated from
// the dependency corpus; clean checkouts skip this optional end-to-end test.
func TestExportFixturePreservesHierarchy(t *testing.T) {
	input := filepath.Join("..", "..", "..", "testdata", "fixtures", "sample.ost")
	if _, err := os.Stat(input); err != nil {
		t.Skipf("local integration fixture unavailable: %v", err)
	}
	out := t.TempDir()
	report, err := Export(input, out)
	if err != nil {
		t.Fatal(err)
	}
	if report.Folders == 0 {
		t.Fatal("export reported no folders")
	}
	if report.Messages != 1 {
		t.Fatalf("exported messages = %d, want 1", report.Messages)
	}
	if report.MessagesAttempted != 1 || len(report.MessageStatus) != 1 || report.MessageStatus[0].Outcome != "exported" {
		t.Fatalf("message status = %+v, attempted=%d", report.MessageStatus, report.MessagesAttempted)
	}
	nested := filepath.Join(out, "Local Folders", "Top of Personal Folders.sbd", "myInbox")
	if _, err := os.Stat(nested); err != nil {
		t.Fatalf("nested Thunderbird mbox missing: %v", err)
	}
	b, err := os.ReadFile(nested)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), "From ") || !strings.Contains(string(b), "MIME-Version: 1.0") {
		t.Fatalf("exported mbox does not look like an RFC 5322 message: %q", string(b))
	}
}

func TestExportCorpusPreservesAttachments(t *testing.T) {
	input := filepath.Join("..", "..", "..", ".gomodcache", "github.com", "useti", "go-pst", "v6@v6.0.8", "data", "support.pst")
	if _, err := os.Stat(input); err != nil {
		t.Skipf("attachment corpus unavailable: %v", err)
	}
	out := t.TempDir()
	report, err := Export(input, out)
	if err != nil {
		t.Fatal(err)
	}
	if report.Attachments != 2 {
		t.Fatalf("exported attachments = %d, want 2", report.Attachments)
	}
	if report.MessagesAttempted == 0 || len(report.MessageStatus) == 0 {
		t.Fatal("export did not produce bounded message statuses")
	}
	var encodedParts int
	err = filepath.WalkDir(out, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		encodedParts += bytes.Count(data, []byte("Content-Transfer-Encoding: base64"))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if encodedParts < 2 {
		t.Fatalf("base64 MIME attachment parts = %d, want at least 2", encodedParts)
	}
}

func TestExportRecoveredMessagesUsesSeparateFolder(t *testing.T) {
	input := filepath.Join("..", "..", "..", "testdata", "fixtures", "sample.ost")
	if _, err := os.Stat(input); err != nil {
		t.Skipf("local integration fixture unavailable: %v", err)
	}
	out := t.TempDir()
	report, err := ExportRecoveredMessages(input, out, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !report.RecoveryMode {
		t.Fatal("recovery export did not mark recovery mode")
	}
	path := filepath.Join(out, "Local Folders", "OSTOM Recovered")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("recovery mbox missing: %v", err)
	}
}
