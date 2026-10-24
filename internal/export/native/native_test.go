package native

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ostom/internal/format"
	"ostom/internal/recovery/manifest"
	"ostom/internal/recovery/session"
)

func TestAppendJournalRecordUsesDurableItemStates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "items.jsonl")
	journal, err := session.OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := appendJournalRecord(journal, manifest.Record{
		Kind: "message", Source: "native", Identity: "native-bid-1", Route: "native-recovered", Status: "recovered",
		ReadableBytes: 12, ExportedBytes: 12,
	}); err != nil {
		t.Fatal(err)
	}
	if err := appendJournalRecord(journal, manifest.Record{
		Kind: "attachment", Source: "native", Identity: "native-bid-1-nid-2", Route: "native-recovered", Status: "unavailable",
		Errors: []string{"missing payload"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	latest, err := session.Latest(path)
	if err != nil {
		t.Fatal(err)
	}
	if latest["native-bid-1"].Status != "exported" || latest["native-bid-1-nid-2"].Status != "failed" {
		t.Fatalf("unexpected native journal states: %+v", latest)
	}
}

func TestReconcileCommittedJournalAfterBatchCommit(t *testing.T) {
	root := t.TempDir()
	batchDir := filepath.Join(root, "batch-000001")
	if err := os.MkdirAll(batchDir, 0o700); err != nil {
		t.Fatal(err)
	}
	record := manifest.Record{
		Kind: "message", Source: "native", Identity: "native-bid-0001", Route: "native-recovered",
		Status: "recovered", ReadableBytes: 7, ExportedBytes: 7, OutputPath: "Local Folders\\OSTOM Recovered",
	}
	manifestPath := filepath.Join(batchDir, "manifest.jsonl")
	manifestFile, err := os.Create(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(manifestFile).Encode(record); err != nil {
		t.Fatal(err)
	}
	if err := manifestFile.Close(); err != nil {
		t.Fatal(err)
	}
	outputDir := filepath.Join(batchDir, "output")
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "message"), []byte("output"), 0o600); err != nil {
		t.Fatal(err)
	}
	outputBytes, outputSHA, err := session.DigestArtifact(batchDir, "output")
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes, manifestSHA, err := session.DigestArtifact(batchDir, "manifest.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if err := session.WriteBatchRecord(batchDir, session.Batch{
		Number: 1, Status: "committed", Artifact: "output", Bytes: outputBytes, SHA256: outputSHA,
		Manifest: "manifest.jsonl", ManifestBytes: manifestBytes, ManifestSHA256: manifestSHA,
		Identities: []string{record.Identity},
	}); err != nil {
		t.Fatal(err)
	}

	journalPath := filepath.Join(root, "items.jsonl")
	journal, err := session.OpenJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(session.ItemEntry{Identity: record.Identity, Kind: "message", Route: "native-recovered", Status: "in_progress"}); err != nil {
		t.Fatal(err)
	}
	completed := map[string]bool{record.Identity: false}
	if err := reconcileCommittedJournal(journal, []string{batchDir}, completed); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	latest, err := session.Latest(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if latest[record.Identity].Status != "exported" || !completed[record.Identity] {
		t.Fatalf("committed batch did not reconcile journal: latest=%+v completed=%v", latest, completed)
	}
}

func TestExportStreamsLazyLargeAttachment(t *testing.T) {
	root := t.TempDir()
	payload := bytes.Repeat([]byte("lazy-attachment-payload-"), 32*1024)
	sum := sha256.Sum256(payload)
	report, err := ExportWithProgressMode(root, []format.RecoveredMessage{{
		BID:     0x202,
		Subject: "Lazy attachment",
		Body:    []byte("body"),
		Attachments: []format.RecoveredAttachment{{
			Name: "large.bin", MimeType: "application/octet-stream", ReadableSize: int64(len(payload)),
			Open: func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(payload)), nil },
		}},
	}}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if report.EMLWritten != 1 || len(report.ManifestRecords) != 2 {
		t.Fatalf("unexpected lazy attachment report: %+v", report)
	}
	attachment := report.ManifestRecords[1]
	if attachment.Kind != "attachment" || attachment.ReadableBytes != int64(len(payload)) || attachment.SHA256 != fmt.Sprintf("%x", sum[:]) || attachment.ExportedBytes != int64(len(payload)) {
		t.Fatalf("lazy attachment was not streamed and accounted: %+v", attachment)
	}
	eml, err := os.ReadFile(filepath.Join(root, "OSTOM Native Recovered EML", "0000000000000202.eml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(eml, []byte("large.bin")) || len(eml) <= len(payload) {
		t.Fatalf("lazy attachment missing from EML: bytes=%d", len(eml))
	}
}

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

func TestExportUsesRecoveredFolderPath(t *testing.T) {
	root := t.TempDir()
	_, err := Export(root, []format.RecoveredMessage{{
		BID: 0x201, Subject: "Foldered", Body: []byte("body"), FolderPath: []string{"Inbox", "Archive"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "OSTOM Native Recovered.sbd", "Inbox.sbd", "Archive")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("folder-preserving mbox missing: %v", err)
	}
}

func TestDiscardUncommittedBatches(t *testing.T) {
	workDir := t.TempDir()
	batches := filepath.Join(workDir, "batches")
	if err := os.MkdirAll(filepath.Join(batches, ".writing-crash", "output"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(batches, "000001"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := discardUncommittedBatches(workDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(batches, ".writing-crash")); !os.IsNotExist(err) {
		t.Fatalf("incomplete batch still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(batches, "000001")); err != nil {
		t.Fatalf("committed batch directory was removed: %v", err)
	}
}

func TestResumeReusesCommittedAndDiscardsInterruptedBatch(t *testing.T) {
	root := t.TempDir()
	workDir := filepath.Join(root, "work")
	committedDir := filepath.Join(workDir, "batches", "000001")
	if err := os.MkdirAll(filepath.Join(committedDir, "output"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(committedDir, "output", "message"), []byte("committed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(committedDir, "manifest.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outputBytes, outputSHA, err := session.DigestArtifact(committedDir, "output")
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes, manifestSHA, err := session.DigestArtifact(committedDir, "manifest.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	committed := session.Batch{
		Number: 1, Status: "committed", Artifact: "output", Bytes: outputBytes, SHA256: outputSHA,
		Manifest: "manifest.jsonl", ManifestBytes: manifestBytes, ManifestSHA256: manifestSHA,
		Identities: []string{"native-bid-0001"},
	}
	if err := session.WriteBatchRecord(committedDir, committed); err != nil {
		t.Fatal(err)
	}
	interruptedDir := filepath.Join(workDir, "batches", ".writing-crash", "output")
	if err := os.MkdirAll(interruptedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(interruptedDir, "partial"), []byte("discard me"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := discardUncommittedBatches(workDir); err != nil {
		t.Fatal(err)
	}
	batches, completed := loadCommittedBatches(workDir)
	if len(batches) != 1 || !completed["native-bid-0001"] {
		t.Fatalf("resume did not retain committed batch: batches=%v completed=%v", batches, completed)
	}
	if _, err := os.Stat(filepath.Join(workDir, "batches", ".writing-crash")); !os.IsNotExist(err) {
		t.Fatalf("interrupted batch still exists: %v", err)
	}
}
