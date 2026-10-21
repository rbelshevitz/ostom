package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/grokify/outlook-pst-go/pkg/disk"
)

func TestDamageFileHeaderCRC(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.pst")
	original := make([]byte, 64)
	for i := range original {
		original[i] = byte(i)
	}
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := damageFile(path, "header-crc"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got[4] == original[4] {
		t.Fatal("header damage did not change the CRC bytes")
	}
	if got[8] != original[8] || got[63] != original[63] {
		t.Fatal("header damage changed bytes outside the CRC")
	}
}

func TestConvertToSyntheticOSTChangesMagicAndCRCs(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source.pst")
	dst := filepath.Join(dir, "source.ost")
	b := make([]byte, 1024)
	b[8], b[9] = 'S', 'M'
	if err := os.WriteFile(src, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := convertToSyntheticOST(src, dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got[8:10]) != "SO" {
		t.Fatalf("client magic = %q", got[8:10])
	}
	if binary.LittleEndian.Uint32(got[520:524]) != disk.ComputeCRC(got[8:520]) {
		t.Fatal("full header CRC was not recomputed")
	}
}

func TestParseSize(t *testing.T) {
	tests := map[string]int64{
		"100":   100,
		"100MB": 100 << 20,
		"2 GiB": 2 << 30,
	}
	for input, want := range tests {
		got, err := parseSize(input)
		if err != nil {
			t.Fatalf("parseSize(%q): %v", input, err)
		}
		if got != want {
			t.Errorf("parseSize(%q) = %d, want %d", input, got, want)
		}
	}
}

func TestPadFileToSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.pst")
	if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := padFileToSize(path, 2<<20); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 2<<20 {
		t.Fatalf("padded size = %d, want %d", info.Size(), 2<<20)
	}
}
