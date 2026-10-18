package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"ostom/internal/format"
)

// TestSyntheticOSTRecoverySmoke runs the same bounded recovery stages used by
// the CLI against one parser-validated synthetic OST and several isolated
// damage modes. The fixture is generated from the dependency corpus and is
// intentionally not committed.
func TestSyntheticOSTRecoverySmoke(t *testing.T) {
	source := filepath.Join(runtime.GOROOT(), "pkg", "mod", "github.com", "grokify", "outlook-pst-go@v0.1.0", "sample.pst")
	var err error
	if _, statErr := os.Stat(source); statErr != nil {
		source, err = resolveSource("")
	}
	if err != nil {
		t.Skipf("synthetic OST source unavailable: %v", err)
	}

	baseDir := t.TempDir()
	base := filepath.Join(baseDir, "healthy.ost")
	building := base + ".building.pst"
	if err := copyFile(source, building); err != nil {
		t.Fatal(err)
	}
	if err := convertToSyntheticOST(building, base); err != nil {
		t.Fatal(err)
	}

	baseInfo, err := os.Stat(base)
	if err != nil {
		t.Fatal(err)
	}
	baseHeader := format.ReadHeader(base)
	if !baseHeader.OK || baseHeader.ContentType != "SO" {
		t.Fatalf("synthetic OST header is not readable: %+v", baseHeader)
	}
	baseRoots := format.ScanIndexRoots(base, baseHeader, baseInfo.Size())
	if baseRoots.Status != "completed" || baseRoots.NBTRefs == 0 || baseRoots.BBTRefs == 0 {
		t.Fatalf("synthetic OST indexes are not readable: %+v", baseRoots)
	}
	baseScan, err := format.CollectIndexedRecoveredMessages(
		base, baseHeader, baseInfo.Size(), 64, 32, 1<<20, 256, 32, 256<<10, 16,
	)
	if err != nil || baseScan.Status != "completed" || baseScan.NodesAttempted == 0 {
		t.Fatalf("healthy recovery pipeline did not run: scan=%+v err=%v", baseScan, err)
	}

	damages := []string{
		"header-crc",
		"declared-size",
		"truncate",
		"btree-root",
		"block-trailer",
		"xblock-level",
		"xblock-count",
		"subnode-entry",
	}
	for _, damage := range damages {
		damage := damage
		t.Run(damage, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "damaged.ost")
			if err := copyFile(base, path); err != nil {
				t.Fatal(err)
			}
			if err := damageFile(path, damage); err != nil {
				t.Skipf("fixture does not contain a %s structure: %v", damage, err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}

			assertNoRecoveryPanic(t, func() {
				h := format.ReadHeader(path)
				if !h.OK {
					t.Fatalf("damaged file lost basic PFF identity: %+v", h)
				}
				if damage == "header-crc" && h.HeaderCRC != "invalid" {
					t.Fatalf("header CRC damage was not diagnosed: %+v", h)
				}
				if damage == "declared-size" && h.DeclaredSize == uint64(info.Size()) {
					t.Fatalf("declared-size damage was not preserved: %+v", h)
				}
				if damage == "btree-root" && h.Roots != "missing NBT/BBT root" {
					t.Fatalf("root damage was not diagnosed: %+v", h)
				}

				_ = format.ScanIndexRoots(path, h, info.Size())
				_ = format.ScanAllocationPages(path, h, info.Size())
				_, _ = format.WalkIndexTrees(path, h, info.Size(), 100000, 64)
				_ = format.ScanIndexedPayloads(path, h, info.Size(), 256)
				_ = format.ScanIndexedPropertyContexts(path, h, info.Size(), 64, 32, 1<<20, 256, 16)
				_, _ = format.CollectIndexedRecoveredMessages(path, h, info.Size(), 64, 32, 1<<20, 256, 32, 256<<10, 16)
			})
		})
	}
}

func assertNoRecoveryPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("recovery routine panicked on damaged fixture: %v", recovered)
		}
	}()
	fn()
}
