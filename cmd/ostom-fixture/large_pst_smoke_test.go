package main

import (
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

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

	started := time.Now()
	var phase atomic.Value
	phase.Store("starting")
	var progressDone atomic.Int64
	var progressTotal atomic.Int64
	heartbeatPath := os.Getenv("OSTOM_LARGE_PST_SMOKE_HEARTBEAT")
	var heartbeatFile *os.File
	if heartbeatPath != "" {
		heartbeatFile, err = os.OpenFile(heartbeatPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			t.Fatalf("open large-PST heartbeat log: %v", err)
		}
		defer heartbeatFile.Close()
	}
	heartbeatDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				line := fmt.Sprintf("large-pst-smoke heartbeat phase=%s elapsed=%s progress=%d/%d size=%d\n",
					phase.Load().(string), time.Since(started).Round(time.Second),
					progressDone.Load(), progressTotal.Load(), info.Size())
				fmt.Fprint(os.Stderr, line)
				if heartbeatFile != nil {
					_, _ = heartbeatFile.WriteString(line)
					_ = heartbeatFile.Sync()
				}
			case <-heartbeatDone:
				return
			}
		}
	}()
	defer close(heartbeatDone)

	phase.Store("read-header")
	h := format.ReadHeader(name)
	if !h.OK || h.ContentType != "SM" {
		t.Fatalf("large PST header is not readable: %+v", h)
	}
	phase.Store("scan-index-roots")
	roots := format.ScanIndexRoots(name, h, info.Size())
	if roots.Status != "completed" || roots.NBTRefs == 0 || roots.BBTRefs == 0 {
		t.Fatalf("large PST indexes are not readable: %+v", roots)
	}

	phase.Store("collect-indexed-messages")
	scan, err := format.CollectIndexedRecoveredMessagesWithProgress(
		name, h, info.Size(),
		100000, 100000, 64<<20,
		256, 32, 1<<20, 32,
		func(done, total int) {
			progressDone.Store(int64(done))
			progressTotal.Store(int64(total))
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if scan.Status != "completed" || len(scan.Messages) == 0 {
		t.Fatalf("large PST message recovery did not complete: %+v", scan)
	}
	phase.Store("completed")
	line := fmt.Sprintf("large-pst-smoke completed elapsed=%s messages=%d\n", time.Since(started).Round(time.Second), len(scan.Messages))
	fmt.Fprint(os.Stderr, line)
	if heartbeatFile != nil {
		_, _ = heartbeatFile.WriteString(line)
		_ = heartbeatFile.Sync()
	}
}
