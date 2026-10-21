package main

import (
	"os"
	"testing"

	"ostom/internal/format"
)

// TestLargePSTRecoverySmoke is opt-in so normal CI never downloads or creates
// a large binary fixture. Prepare a real PST externally and set
// OSTOM_LARGE_PST to its path before running this test.
func TestLargePSTRecoverySmoke(t *testing.T) {
	name := os.Getenv("OSTOM_LARGE_PST")
	if name == "" {
		t.Skip("OSTOM_LARGE_PST is not set")
	}
	info, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	if info.IsDir() {
		t.Fatalf("large PST path is a directory: %s", name)
	}
	if info.Size() < 64<<20 {
		t.Fatalf("large PST is only %d bytes; expected at least 64 MiB", info.Size())
	}

	h := format.ReadHeader(name)
	if !h.OK || h.ContentType != "SM" {
		t.Fatalf("large PST header is not readable: %+v", h)
	}
	roots := format.ScanIndexRoots(name, h, info.Size())
	if roots.Status != "completed" || roots.NBTRefs == 0 || roots.BBTRefs == 0 {
		t.Fatalf("large PST indexes are not readable: %+v", roots)
	}

	scan, err := format.CollectIndexedRecoveredMessages(
		name, h, info.Size(),
		100000, 100000, 64<<20,
		256, 32, 1<<20, 32,
	)
	if err != nil {
		t.Fatal(err)
	}
	if scan.Status != "completed" || len(scan.Messages) == 0 {
		t.Fatalf("large PST message recovery did not complete: %+v", scan)
	}
}
