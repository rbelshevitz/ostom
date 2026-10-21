package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
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
	if err := journal.Append(ItemEntry{Identity: "message-1", Kind: "message", Status: "exported"}); err != nil {
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
}
