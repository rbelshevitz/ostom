package gopst

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ostom/internal/recovery/session"
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
	for _, reportError := range report.Errors {
		if strings.Contains(reportError, "folder has no messages") {
			t.Fatalf("empty folder was reported as an export error: %q", reportError)
		}
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
	if !strings.HasPrefix(string(b), "From ") || !strings.Contains(string(b), "Date: ") || !strings.Contains(string(b), "MIME-Version: 1.0") {
		t.Fatalf("exported mbox does not look like an RFC 5322 message: %q", string(b))
	}
	emlFiles, err := filepath.Glob(filepath.Join(out, "OSTOM EML", "Top of Personal Folders", "myInbox", "*.eml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(emlFiles) != 1 {
		t.Fatalf("indexed EML files = %v, want exactly one", emlFiles)
	}
	eml, err := os.ReadFile(emlFiles[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(string(eml), "From ") || !strings.Contains(string(eml), "MIME-Version: 1.0") {
		t.Fatalf("indexed EML is not raw RFC 5322 output: %q", string(eml))
	}
}

func TestExportFixtureMboxOnlyDoesNotDuplicateEML(t *testing.T) {
	input := filepath.Join("..", "..", "..", "testdata", "fixtures", "sample.ost")
	if _, err := os.Stat(input); err != nil {
		t.Skipf("local integration fixture unavailable: %v", err)
	}
	out := t.TempDir()
	report, err := ExportWithProgressMode(input, out, nil, 0, false, nil, EMLNone)
	if err != nil {
		t.Fatal(err)
	}
	if report.Messages == 0 || report.EMLWritten != 0 {
		t.Fatalf("unexpected mbox-only report: %+v", report)
	}
	if _, err := os.Stat(filepath.Join(out, "OSTOM EML")); !os.IsNotExist(err) {
		t.Fatalf("mbox-only export created EML tree: err=%v", err)
	}
}

func TestExportFixtureResumeDoesNotRewriteSkippedJournalState(t *testing.T) {
	input := filepath.Join("..", "..", "..", "testdata", "fixtures", "sample.ost")
	if _, err := os.Stat(input); err != nil {
		t.Skipf("local integration fixture unavailable: %v", err)
	}
	out := t.TempDir()
	first, err := ExportWithProgressMode(input, out, nil, 0, false, nil, EMLNone)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.MessageStatus) != 1 || first.MessageStatus[0].Identity == "" {
		t.Fatalf("first export status = %+v", first.MessageStatus)
	}
	mbox := filepath.Join(out, "Local Folders", "Top of Personal Folders.sbd", "myInbox")
	before, err := os.ReadFile(mbox)
	if err != nil {
		t.Fatal(err)
	}
	var callbacks []MessageStatus
	second, err := ExportWithProgressModeAndCallbacks(input, out, nil, 0, false, nil, EMLNone,
		func(status MessageStatus) { callbacks = append(callbacks, status) },
		func(identity string) bool { return identity == first.MessageStatus[0].Identity })
	if err != nil {
		t.Fatal(err)
	}
	if second.SkippedMessages != 1 || len(callbacks) != 0 {
		t.Fatalf("resume skipped=%d callbacks=%+v", second.SkippedMessages, callbacks)
	}
	after, err := os.ReadFile(mbox)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("resume changed the mbox for a durably skipped message")
	}
}

func TestExportFixtureIndexedResumeRepairsTornMessage(t *testing.T) {
	input := filepath.Join("..", "..", "..", "testdata", "fixtures", "sample.ost")
	if _, err := os.Stat(input); err != nil {
		t.Skipf("local integration fixture unavailable: %v", err)
	}
	out := t.TempDir()
	journalPath := filepath.Join(out, "items.jsonl")
	journal, err := session.OpenJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	first, err := ExportWithProgressModeAndCallbacks(input, out, nil, 0, false, nil, EMLNone,
		func(status MessageStatus) {
			if appendErr := journal.Append(session.ItemEntry{
				Identity: status.Identity, Kind: "message", Route: "indexed", Status: status.Outcome,
				OutputPath: status.OutputPath, OutputOffset: status.OutputOffset,
			}); appendErr != nil {
				t.Fatal(appendErr)
			}
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.MessageStatus) != 1 || first.MessageStatus[0].Outcome != "exported" {
		t.Fatalf("first export status = %+v", first.MessageStatus)
	}
	status := first.MessageStatus[0]
	if err := journal.Append(session.ItemEntry{
		Identity: status.Identity, Kind: "message", Route: "indexed", Status: "in_progress",
		OutputPath: status.OutputPath, OutputOffset: status.OutputOffset,
	}); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(status.OutputPath, append(mustReadFile(t, status.OutputPath), []byte("torn-tail")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.RepairIndexedOutputs(journalPath); err != nil {
		t.Fatal(err)
	}
	completed, err := session.Completed(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if completed[status.Identity] {
		t.Fatal("torn message was still considered completed")
	}

	journal, err = session.OpenJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ExportWithProgressModeAndCallbacks(input, out, nil, 0, false, nil, EMLNone,
		func(status MessageStatus) {
			if appendErr := journal.Append(session.ItemEntry{Identity: status.Identity, Kind: "message", Route: "indexed", Status: status.Outcome, OutputPath: status.OutputPath, OutputOffset: status.OutputOffset}); appendErr != nil {
				t.Fatal(appendErr)
			}
		}, func(identity string) bool { return completed[identity] })
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	if second.Messages != 1 || second.SkippedMessages != 0 {
		t.Fatalf("resume report = %+v", second)
	}
	data := mustReadFile(t, status.OutputPath)
	if bytes.Count(data, []byte("\nFrom "))+boolToInt(bytes.HasPrefix(data, []byte("From "))) != 1 {
		t.Fatalf("resumed mbox contains unexpected message count: %d", bytes.Count(data, []byte("From ")))
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
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
	var emlFiles int
	var checkedEML bool
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
		if strings.HasSuffix(path, ".eml") {
			emlFiles++
			if !checkedEML {
				checkedEML = true
				if strings.HasPrefix(string(data), "From ") || !bytes.Contains(data, []byte("MIME-Version: 1.0")) {
					return fmt.Errorf("invalid raw EML fallback %q", path)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if encodedParts < 2 {
		t.Fatalf("base64 MIME attachment parts = %d, want at least 2", encodedParts)
	}
	if emlFiles != report.EMLWritten || emlFiles != report.Messages {
		t.Fatalf("indexed EML files = %d, report EML=%d messages=%d", emlFiles, report.EMLWritten, report.Messages)
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
