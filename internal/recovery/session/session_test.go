package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenAndUpdatePersistsHiddenSessionState(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "sample.ost")
	if err := os.WriteFile(source, []byte("ostom-session-test"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(filepath.Join(root, "export"), source, Options{FullEML: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update("indexed", "folder:Inbox/message:12"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Open(filepath.Join(root, "export"), source, Options{FullEML: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.State().Cursor; got != "folder:Inbox/message:12" {
		t.Fatalf("cursor = %q", got)
	}
	data, err := os.ReadFile(filepath.Join(root, "export", ".ostom", "sessions", store.State().SessionID, "session.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	if state.Phase != "indexed" || !state.Options.FullEML {
		t.Fatalf("unexpected state: %+v", state)
	}
}

func TestBatchRecordAndArtifactVerification(t *testing.T) {
	root := t.TempDir()
	storeDir := filepath.Join(root, "batch")
	if err := os.MkdirAll(storeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	content := []byte("verified batch\n")
	if err := os.WriteFile(filepath.Join(storeDir, "items.bin"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(content)
	batch := Batch{Number: 1, Status: "committed", Artifact: "items.bin", Bytes: int64(len(content)), SHA256: hex.EncodeToString(hash[:])}
	if err := WriteBatchRecord(storeDir, batch); err != nil {
		t.Fatal(err)
	}
	got, err := ReadBatchRecord(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyArtifact(got, storeDir); err != nil {
		t.Fatal(err)
	}
}

func TestBatchDirectoryArtifactVerification(t *testing.T) {
	root := t.TempDir()
	artifact := filepath.Join(root, "output")
	if err := os.MkdirAll(filepath.Join(artifact, "Inbox.sbd"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(artifact, "Inbox"), []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(artifact, "Inbox.sbd", "Old"), []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	batch := Batch{Number: 2, Status: "committed", Artifact: "output", Bytes: 11}
	if err := VerifyArtifact(batch, root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(artifact, "Inbox"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyArtifact(batch, root); err == nil {
		t.Fatal("changed directory artifact was accepted")
	}
}

func TestJournalAppendsItemOutcomes(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "items.jsonl")
	journal, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(ItemEntry{Source: "sample.ost", Identity: "message-1", Kind: "message", Route: "indexed", Status: "exported", ReadableBytes: 12, ExportedBytes: 12, SHA256: "abc123", OutputPath: "Local Folders/Inbox"}); err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(ItemEntry{Identity: "message-2", Kind: "message", Status: "partial", Error: "truncated"}); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	completed, err := Completed(path)
	if err != nil {
		t.Fatal(err)
	}
	if !completed["message-1"] || completed["message-2"] {
		t.Fatalf("unexpected completed map: %+v", completed)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if lines := len(string(data)); lines == 0 || string(data)[len(string(data))-1] != '\n' {
		t.Fatal("journal is not newline-delimited")
	}
	if !strings.Contains(string(data), `"sha256":"abc123"`) || !strings.Contains(string(data), `"route":"indexed"`) {
		t.Fatalf("journal lost provenance fields: %s", data)
	}
}

func TestJournalRejectsUnknownStateAndNegativeBytes(t *testing.T) {
	journal, err := OpenJournal(filepath.Join(t.TempDir(), "items.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	for _, entry := range []ItemEntry{
		{Identity: "message-1", Kind: "message", Status: "unknown"},
		{Identity: "message-2", Kind: "message", Status: "exported", ReadableBytes: -1},
	} {
		if err := journal.Append(entry); err == nil {
			t.Fatalf("invalid journal entry was accepted: %+v", entry)
		}
	}
}

func TestRepairIndexedOutputsKeepsCompletedAndTruncatesPending(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "items.jsonl")
	completedOutput := filepath.Join(root, "completed.mbox")
	pendingOutput := filepath.Join(root, "pending.mbox")
	if err := os.WriteFile(completedOutput, []byte("completed-output"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pendingOutput, []byte("pending-output"), 0o600); err != nil {
		t.Fatal(err)
	}
	journal, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	entries := []ItemEntry{
		{Identity: "completed", Kind: "message", Status: "in_progress", OutputPath: completedOutput, OutputOffset: 2},
		{Identity: "completed", Kind: "message", Status: "exported", OutputPath: completedOutput, OutputOffset: 2},
		{Identity: "pending", Kind: "message", Status: "in_progress", OutputPath: pendingOutput, OutputOffset: 2},
	}
	for _, entry := range entries {
		if err := journal.Append(entry); err != nil {
			t.Fatal(err)
		}
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	if err := RepairIndexedOutputs(path); err != nil {
		t.Fatal(err)
	}
	completed, err := os.ReadFile(completedOutput)
	if err != nil {
		t.Fatal(err)
	}
	if string(completed) != "completed-output" {
		t.Fatalf("completed output was truncated: %q", completed)
	}
	pending, err := os.ReadFile(pendingOutput)
	if err != nil {
		t.Fatal(err)
	}
	if string(pending) != "pe" {
		t.Fatalf("pending output was not truncated: %q", pending)
	}
}

func TestJournalSmokeIgnoresCrashTornTail(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "items.jsonl")
	journal, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(ItemEntry{Identity: "done", Kind: "message", Status: "exported"}); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"identity":"crashed","kind":"message","status":"in_progress"`); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	completed, err := Completed(path)
	if err != nil {
		t.Fatal(err)
	}
	if !completed["done"] || len(completed) != 1 {
		t.Fatalf("completed state after torn tail = %+v", completed)
	}
}

func TestLatestJournalStateCollapsesItemHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "items.jsonl")
	journal, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(ItemEntry{Source: "sample.ost", Identity: "message-1", Kind: "message", Route: "indexed", Status: "in_progress", OutputPath: "Inbox"}); err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(ItemEntry{Source: "sample.ost", Identity: "message-1", Kind: "message", Route: "indexed", Status: "exported", ReadableBytes: 10, ExportedBytes: 10, OutputPath: "Inbox"}); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	latest, err := Latest(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(latest) != 1 || latest["message-1"].Status != "exported" || latest["message-1"].ExportedBytes != 10 || latest["message-1"].Source != "sample.ost" {
		t.Fatalf("latest journal state = %+v", latest)
	}
}

func TestLongJournalResumeSmoke(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "items.jsonl")
	output := filepath.Join(root, "long.mbox")
	if err := os.WriteFile(output, []byte("prefix-corrupt-tail"), 0o600); err != nil {
		t.Fatal(err)
	}
	journal, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	const completedItems = 2048
	for index := 0; index < completedItems; index++ {
		if err := journal.Append(ItemEntry{Identity: fmt.Sprintf("message-%05d", index), Kind: "message", Route: "indexed", Status: "exported", ExportedBytes: 12}); err != nil {
			t.Fatal(err)
		}
	}
	if err := journal.Append(ItemEntry{Identity: "message-pending", Kind: "message", Route: "indexed", Status: "in_progress", OutputPath: output, OutputOffset: 6}); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"identity":"torn-tail","kind":"message","status":"in_progress"`); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := RepairIndexedOutputs(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "prefix" {
		t.Fatalf("pending output after long resume = %q", data)
	}
	latest, err := Latest(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(latest) != completedItems+1 || latest["message-pending"].Status != "in_progress" {
		t.Fatalf("long journal inventory = %d items, pending=%+v", len(latest), latest["message-pending"])
	}
	completed, err := Completed(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(completed) != completedItems+1 || !completed["message-00000"] || completed["message-pending"] {
		t.Fatalf("long resume skip set = %d items, first=%v pending=%v", len(completed), completed["message-00000"], completed["message-pending"])
	}
}
