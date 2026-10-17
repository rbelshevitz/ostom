package format

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/grokify/outlook-pst-go/pkg/disk"
)

func TestReadHeaderVariants(t *testing.T) {
	tests := []struct {
		name        string
		version     uint16
		pageSize    int
		variant     string
		compression string
	}{
		{"ansi", 14, 512, "32-bit ANSI", "not determined"},
		{"unicode", 23, 512, "64-bit Unicode, 512-byte pages", "not determined"},
		{"compressed-capable", 36, 4096, "64-bit Unicode, 4 KiB pages", "DEFLATE blocks possible; block scan pending"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sample.pff")
			buf := make([]byte, 16)
			copy(buf[:4], "!BDN")
			copy(buf[8:10], "SO")
			binary.LittleEndian.PutUint16(buf[10:12], tc.version)
			if err := os.WriteFile(path, buf, 0o600); err != nil {
				t.Fatal(err)
			}
			h := ReadHeader(path)
			if !h.OK || h.ContentType != "SO" || h.NDBVersion != tc.version || h.PageSize != tc.pageSize || h.Variant != tc.variant || h.Compression != tc.compression {
				t.Fatalf("unexpected header: %+v", h)
			}
		})
	}
}

func TestDecodeBlockPayloadHonorsPFFEncodingAndInternalBID(t *testing.T) {
	plain := []byte{0xEC, 0x00, 0xBC, 0x01, 0xAA}
	const externalBID uint64 = 0x12345670
	encoded := disk.EncryptBlock(plain, disk.CryptMethodPermute, externalBID)
	decoded := decodeBlockPayload(encoded, externalBID, disk.CryptMethodPermute)
	if string(decoded) != string(plain) {
		t.Fatalf("cyclic payload decode mismatch: got %x want %x", decoded, plain)
	}
	internalBID := externalBID | 0x2
	if got := decodeBlockPayload(encoded, internalBID, disk.CryptMethodPermute); string(got) == string(plain) {
		t.Fatal("internal BID was incorrectly decrypted")
	}
}

func TestReadHeaderChecksUnicodeCRCAndDeclaredSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unicode.pff")
	buf := make([]byte, 528)
	copy(buf[:4], "!BDN")
	copy(buf[8:10], "SO")
	binary.LittleEndian.PutUint16(buf[10:12], 23)
	binary.LittleEndian.PutUint64(buf[184:192], uint64(len(buf)))
	binary.LittleEndian.PutUint32(buf[4:8], disk.ComputeCRC(buf[8:524]))
	binary.LittleEndian.PutUint32(buf[520:524], disk.ComputeCRC(buf[8:520]))
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		t.Fatal(err)
	}
	h := ReadHeader(path)
	if h.HeaderCRC != "valid" || h.DeclaredSize != uint64(len(buf)) {
		t.Fatalf("header metadata was not validated: %+v", h)
	}
	buf[100] = 1
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ReadHeader(path).HeaderCRC; got != "invalid" {
		t.Fatalf("CRC status = %q, want invalid", got)
	}
}

func TestValidateRoots(t *testing.T) {
	tests := []struct {
		name, want     string
		nbt, bbt, size uint64
	}{
		{"plausible", "plausible", 0x2000, 0x4000, 0x10000},
		{"missing", "missing NBT/BBT root", 0, 0x4000, 0x10000},
		{"outside", "root outside declared file size", 0x2000, 0x20000, 0x10000},
		{"unaligned", "root offset is not page-aligned", 0x2100, 0x4000, 0x10000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := validateRoots(tc.nbt, tc.bbt, tc.size, 512); got != tc.want {
				t.Fatalf("validateRoots() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestScanAllocationPagesChecks4KAllocationPage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "compressed.ost")
	data := make([]byte, 0x22000+4096)
	page := data[0x22000:]
	page[4072] = 0x84
	page[4073] = 0x84
	binary.LittleEndian.PutUint32(page[4076:4080], disk.ComputeCRC(page[:4072]))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	scan := ScanAllocationPages(path, Header{NDBVersion: 36}, 0x23000)
	if scan.Status != "completed" || scan.PagesChecked != 1 || scan.PagesValid != 1 || scan.PagesInvalid != 0 {
		t.Fatalf("unexpected valid 4K scan result: %+v", scan)
	}
	page[100] = 1
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	scan = ScanAllocationPages(path, Header{NDBVersion: 36}, int64(len(data)))
	if scan.PagesInvalid != 1 {
		t.Fatalf("unexpected damaged 4K scan result: %+v", scan)
	}
}

func TestReadHeaderDoesNotCallUnknownVariantCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unknown.pff")
	buf := make([]byte, 16)
	copy(buf[:4], "!BDN")
	copy(buf[8:10], "SO")
	binary.LittleEndian.PutUint16(buf[10:12], 99)
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		t.Fatal(err)
	}
	h := ReadHeader(path)
	if !h.OK || h.Variant != "unknown PFF variant" || h.PageSize != 0 {
		t.Fatalf("unknown variant was not preserved: %+v", h)
	}
}

func TestWalkIndexTreeExtractsBBTLeafBlockRefs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bbt.pff")
	page := make([]byte, 512)
	// One Unicode BBT leaf entry: BID, IB, CB, cRef/reserved.
	binary.LittleEndian.PutUint64(page[8:16], 64)
	binary.LittleEndian.PutUint16(page[16:18], 10)
	page[488] = 1  // entries
	page[489] = 1  // max entries
	page[490] = 24 // entry size
	page[491] = 0  // leaf
	page[496], page[497] = 0x80, 0x80
	binary.LittleEndian.PutUint32(page[500:504], disk.ComputeCRC(page[:496]))
	data := make([]byte, 1024)
	copy(data[512:], page)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	scan := walkIndexTree(path, 512, 512, int64(len(data)), 0x80, 10, 2)
	if scan.BlockRefs != 1 || scan.BlockBytes != 10 || scan.BadBlocks != 0 || scan.Leaves != 1 {
		t.Fatalf("unexpected BBT leaf scan: %+v", scan)
	}

	binary.LittleEndian.PutUint64(page[8:16], 1020)
	binary.LittleEndian.PutUint32(page[500:504], disk.ComputeCRC(page[:496]))
	copy(data[512:], page)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	scan = walkIndexTree(path, 512, 512, int64(len(data)), 0x80, 10, 2)
	if scan.BlockRefs != 1 || scan.BadBlocks != 1 || scan.BlockBytes != 0 {
		t.Fatalf("invalid BBT block reference was not reported: %+v", scan)
	}
}

func TestWalkIndexTreeExtractsNBTNodeBlockRefs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nbt.pff")
	page := make([]byte, 512)
	binary.LittleEndian.PutUint64(page[8:16], 0x200)  // bidData
	binary.LittleEndian.PutUint64(page[16:24], 0x300) // bidSub
	page[488], page[489], page[490], page[491] = 1, 1, 32, 0
	page[496], page[497] = 0x81, 0x81
	binary.LittleEndian.PutUint32(page[500:504], disk.ComputeCRC(page[:496]))
	data := make([]byte, 1024)
	copy(data[512:], page)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	scan := walkIndexTree(path, 512, 512, int64(len(data)), 0x81, 10, 2)
	if scan.NodeRefs != 1 || scan.DataBIDs != 1 || scan.SubnodeBIDs != 1 {
		t.Fatalf("unexpected NBT leaf scan: %+v", scan)
	}
}

func TestResolveNodeBlocksDistinguishesMissingBIDs(t *testing.T) {
	nbt := IndexTreeScan{nodeBIDs: []nodeRef{
		{dataBID: 0x10, subnodeBID: 0x20},
		{dataBID: 0x30},
	}}
	resolveNodeBlocks(&nbt, map[uint64]bool{0x10: true, 0x20: true})
	if nbt.ResolvedData != 1 || nbt.MissingData != 1 || nbt.ResolvedSub != 1 || nbt.MissingSub != 0 {
		t.Fatalf("unexpected BID resolution: %+v", nbt)
	}
}

func TestAssembleIndexedPayloadIsBounded(t *testing.T) {
	path := filepath.Join("..", "..", ".gomodcache", "github.com", "useti", "go-pst", "v6@v6.0.8", "data", "support.pst")
	if _, err := os.Stat(path); err != nil {
		t.Skip("go-pst corpus not available")
	}
	h := ReadHeader(path)
	nbt, _ := WalkIndexTrees(path, h, 0, 100000, 64)
	if len(nbt.nodeBIDs) == 0 || nbt.nodeBIDs[0].dataBID == 0 {
		t.Fatal("corpus has no data BID")
	}
	data, result, err := AssembleIndexedPayload(path, h, fileSizeForTest(t, path), nbt.nodeBIDs[0].dataBID, 16, 1<<20)
	if err != nil {
		t.Fatalf("assemble payload: %v", err)
	}
	if result.BlocksRead == 0 || uint64(len(data)) != result.BytesProduced || result.BytesProduced > 1<<20 {
		t.Fatalf("unexpected bounded assembly result: %+v, bytes=%d", result, len(data))
	}
}

func fileSizeForTest(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}
