package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestStreamLatestReducesLargeJournalWithoutChangingFinalState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "items.jsonl")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	encoder := json.NewEncoder(file)
	const items = 2050
	for index := 0; index < items; index++ {
		if err := encoder.Encode(ItemEntry{Identity: fmt.Sprintf("message-%04d", index), Kind: "message", Status: "in_progress"}); err != nil {
			t.Fatal(err)
		}
	}
	for index := 0; index < items; index += 2 {
		if err := encoder.Encode(ItemEntry{Identity: fmt.Sprintf("message-%04d", index), Kind: "message", Status: "exported"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	count := 0
	previous := ""
	err = StreamLatest(path, func(entry ItemEntry) error {
		if entry.Identity <= previous {
			return fmt.Errorf("journal identities are not sorted: %q after %q", entry.Identity, previous)
		}
		previous = entry.Identity
		want := "in_progress"
		if count%2 == 0 {
			want = "exported"
		}
		if entry.Status != want {
			return fmt.Errorf("status for %s = %q, want %q", entry.Identity, entry.Status, want)
		}
		count++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != items {
		t.Fatalf("streamed latest entries = %d, want %d", count, items)
	}
}
