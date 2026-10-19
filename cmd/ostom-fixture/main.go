// ostom-fixture creates deterministic PFF fixtures for tests.
// The OST output is a parser-validated synthetic OST container, not a mailbox
// created by Outlook or synchronized with Exchange.
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	outlookpst "github.com/grokify/outlook-pst-go"
	"github.com/grokify/outlook-pst-go/pkg/disk"
	"ostom/internal/format"
)

func main() {
	out := flag.String("out", "testdata/fixtures/sample.pst", "fixture path")
	kind := flag.String("kind", "pst", "pst, ost, or eml")
	damage := flag.String("damage", "", "none, header-crc, declared-size, truncate, btree-root, block-trailer, xblock-level, xblock-count, or subnode-entry")
	source := flag.String("source", "", "known-good PST source; defaults to the dependency's sample.pst")
	messages := flag.Int("messages", 0, "minimum indexed messages required in the source fixture")
	emlCount := flag.Int("eml-count", 6, "number of deterministic EML messages when -kind eml")
	flag.Parse()
	if *kind != "pst" && *kind != "ost" && *kind != "eml" {
		fatalf("-kind must be pst, ost, or eml")
	}
	if *kind == "eml" {
		if *emlCount <= 0 {
			fatalf("-eml-count must be positive")
		}
		if *damage != "" && *damage != "none" {
			fatalf("-damage is not supported with -kind eml")
		}
		if err := generateEMLCorpus(*out, *emlCount); err != nil {
			fatalf("generate EML corpus: %v", err)
		}
		fmt.Printf("created %s (eml, count=%d)\n", *out, *emlCount)
		return
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fatalf("create fixture directory: %v", err)
	}
	if *messages < 0 {
		fatalf("-messages must be non-negative")
	}
	base, err := resolveSource(*source)
	if err != nil {
		fatalf("find source PST: %v", err)
	}
	building := *out + ".building.pst"
	defer os.Remove(building)
	if err := copyFile(base, building); err != nil {
		fatalf("copy source PST: %v", err)
	}
	if *messages > 0 {
		actual, err := countMessages(building)
		if err != nil {
			fatalf("count source messages: %v", err)
		}
		if actual < *messages {
			fatalf("source contains %d indexed messages, need at least %d; pass -source to a larger PST", actual, *messages)
		}
	}
	if *kind == "ost" {
		if err := convertToSyntheticOST(building, *out); err != nil {
			fatalf("write synthetic OST: %v", err)
		}
	} else if err := copyFile(building, *out); err != nil {
		fatalf("rename fixture: %v", err)
	}
	if err := validateFixture(*out, *kind); err != nil {
		fatalf("generated fixture failed parser validation: %v", err)
	}
	if err := damageFile(*out, *damage); err != nil {
		fatalf("damage fixture: %v", err)
	}
	fmt.Printf("created %s (%s, damage=%s)\n", *out, *kind, valueOrNone(*damage))
}

func countMessages(filename string) (int, error) {
	pst, err := outlookpst.Open(filename)
	if err != nil {
		return 0, err
	}
	defer pst.Close()
	root, err := pst.RootFolder()
	if err != nil {
		return 0, err
	}
	return countFolderMessages(root)
}

func countFolderMessages(folder *outlookpst.Folder) (int, error) {
	total := 0
	for _, err := range folder.Messages() {
		if err == nil {
			total++
		}
	}
	for child, err := range folder.Subfolders() {
		if err != nil {
			continue
		}
		n, err := countFolderMessages(child)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func validateFixture(filename, kind string) error {
	pst, err := outlookpst.Open(filename)
	if err != nil {
		return err
	}
	defer pst.Close()
	if kind == "ost" && !pst.IsOST() {
		return fmt.Errorf("parser opened the file but did not identify it as OST")
	}
	if kind == "pst" && pst.IsOST() {
		return fmt.Errorf("parser identified PST fixture as OST")
	}
	return nil
}

func resolveSource(explicit string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return "", err
		}
		return explicit, nil
	}
	candidates := []string{
		filepath.Join(".gomodcache", "github.com", "useti", "go-pst", "v6@v6.0.8", "data", "enron.pst"),
		filepath.Join(".gomodcache", "github.com", "grokify", "outlook-pst-go@v0.1.0", "sample.pst"),
		filepath.Join(runtime.GOROOT(), "pkg", "mod", "github.com", "useti", "go-pst", "v6@v6.0.8", "data", "enron.pst"),
		filepath.Join(runtime.GOROOT(), "pkg", "mod", "github.com", "grokify", "outlook-pst-go@v0.1.0", "sample.pst"),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("pass -source path to a known-good small PST fixture")
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}

func convertToSyntheticOST(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if len(b) < 524 {
		return fmt.Errorf("fixture is shorter than Unicode header")
	}
	// wMagicClient: PST "SM" -> OST "SO". Recompute the two Unicode
	// header CRCs using the same algorithm as the library/spec fixture.
	b[8], b[9] = 'S', 'O'
	binary.LittleEndian.PutUint32(b[4:8], disk.ComputeCRC(b[8:524]))
	binary.LittleEndian.PutUint32(b[520:524], disk.ComputeCRC(b[8:520]))
	return os.WriteFile(dst, b, 0o644)
}

func damageFile(filename, damage string) error {
	if damage == "" || damage == "none" {
		return nil
	}
	b, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	switch damage {
	case "header-crc":
		if len(b) < 8 {
			return fmt.Errorf("file too short")
		}
		b[4] ^= 0xff
	case "declared-size":
		if len(b) < 524 || binary.LittleEndian.Uint16(b[10:12]) < 23 {
			return fmt.Errorf("file is not a supported Unicode fixture")
		}
		declared := uint64(len(b) - 1)
		binary.LittleEndian.PutUint64(b[184:192], declared)
		// Keep this scenario isolated to the declared-size field by rebuilding
		// the two Unicode header CRC values used by ReadHeader.
		for i := 520; i < 524; i++ {
			b[i] = 0
		}
		binary.LittleEndian.PutUint32(b[4:8], disk.ComputeCRC(b[8:524]))
		binary.LittleEndian.PutUint32(b[520:524], disk.ComputeCRC(b[8:520]))
	case "truncate":
		if len(b) < 2 {
			return fmt.Errorf("file too short")
		}
		b = b[:len(b)/2]
	case "btree-root":
		// Unicode ROOT BREFNBT/BREFBBT are in the header at offsets 216..248
		// in the published layout. Zeroing them simulates a lost root index.
		if len(b) < 248 {
			return fmt.Errorf("file too short")
		}
		for i := 216; i < 248; i++ {
			b[i] = 0
		}
	case "block-trailer":
		if err := damageFirstBlockTrailer(filename, b); err != nil {
			return err
		}
	case "xblock-level":
		if err := mutateFirstInternalPayload(filename, b, func(payload []byte) bool {
			return len(payload) >= 8 && payload[0] == 0x01 && (payload[1] == 1 || payload[1] == 2)
		}, func(payload []byte) error {
			payload[1] = 3
			return nil
		}); err != nil {
			return err
		}
	case "xblock-count":
		if err := mutateFirstInternalPayload(filename, b, func(payload []byte) bool {
			return len(payload) >= 8 && payload[0] == 0x01 && (payload[1] == 1 || payload[1] == 2)
		}, func(payload []byte) error {
			binary.LittleEndian.PutUint16(payload[2:4], 0xffff)
			return nil
		}); err != nil {
			return err
		}
	case "subnode-entry":
		if err := mutateFirstInternalPayload(filename, b, func(payload []byte) bool {
			return len(payload) >= 32 && payload[0] == 0x02 && payload[1] == 0 && binary.LittleEndian.Uint16(payload[2:4]) > 0
		}, func(payload []byte) error {
			binary.LittleEndian.PutUint64(payload[16:24], 0)
			return nil
		}); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown damage %q", damage)
	}
	return os.WriteFile(filename, b, 0o644)
}

func damageFirstBlockTrailer(filename string, b []byte) error {
	h := format.ReadHeader(filename)
	locations, err := format.IndexedBlockLocations(filename, h, int64(len(b)), 100000, 64)
	if err != nil {
		return fmt.Errorf("locate BBT blocks: %w", err)
	}
	var selected format.IndexedBlockLocation
	found := false
	for _, location := range locations {
		aligned := (uint64(location.Size) + 63) &^ 63
		trailerCRC := location.Offset + aligned + 4
		if location.Size == 0 || trailerCRC+4 > uint64(len(b)) {
			continue
		}
		if !found || location.Offset < selected.Offset {
			selected, found = location, true
		}
	}
	if !found {
		return fmt.Errorf("no valid indexed block trailer found")
	}
	aligned := (uint64(selected.Size) + 63) &^ 63
	b[selected.Offset+aligned+4] ^= 0xff
	return nil
}

func mutateFirstInternalPayload(filename string, b []byte, match func([]byte) bool, mutate func([]byte) error) error {
	h := format.ReadHeader(filename)
	locations, err := format.IndexedBlockLocations(filename, h, int64(len(b)), 100000, 64)
	if err != nil {
		return fmt.Errorf("locate BBT blocks: %w", err)
	}
	var selected format.IndexedBlockLocation
	var selectedPayload []byte
	found := false
	for _, location := range locations {
		if location.BID&0x2 == 0 || location.Size == 0 {
			continue
		}
		end := location.Offset + uint64(location.Size)
		if end < location.Offset || end > uint64(len(b)) {
			continue
		}
		payload := disk.DecryptBlock(b[location.Offset:end], h.CryptMethod, location.BID)
		if !match(payload) {
			continue
		}
		if !found || location.Offset < selected.Offset {
			selected, selectedPayload, found = location, payload, true
		}
	}
	if !found {
		return fmt.Errorf("no matching internal payload found")
	}
	if err := mutate(selectedPayload); err != nil {
		return err
	}
	copy(b[selected.Offset:selected.Offset+uint64(selected.Size)], selectedPayload)
	return nil
}

func valueOrNone(value string) string {
	if strings.TrimSpace(value) == "" {
		return "none"
	}
	return value
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(2)
}
