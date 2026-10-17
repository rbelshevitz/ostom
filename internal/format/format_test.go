package format

import (
	"bytes"
	"encoding/binary"
	"fmt"
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

func TestParseExtendedBlockBoundedValidatesLevelAndLength(t *testing.T) {
	data := make([]byte, 16)
	data[0] = 0x01
	data[1] = 0x01
	binary.LittleEndian.PutUint16(data[2:4], 1)
	binary.LittleEndian.PutUint64(data[8:16], 0x1234)
	block, err := parseExtendedBlockBounded(data, disk.FormatUnicode, 1)
	if err != nil || block.Level != 1 || len(block.BIDs) != 1 || block.BIDs[0] != 0x1234 {
		t.Fatalf("unexpected extended block: block=%+v err=%v", block, err)
	}
	if _, err := parseExtendedBlockBounded(data, disk.FormatUnicode, 2); err == nil {
		t.Fatal("level mismatch was accepted")
	}
	if _, err := parseExtendedBlockBounded(data[:15], disk.FormatUnicode, 1); err == nil {
		t.Fatal("truncated BID array was accepted")
	}
}

type testPayloadSource map[uint64][]byte

func (s testPayloadSource) Payload(bid uint64) ([]byte, error) {
	payload, ok := s[bid]
	if !ok {
		return nil, fmt.Errorf("missing test BID 0x%X", bid)
	}
	return payload, nil
}

func makeTestExtendedBlock(level byte, bids ...uint64) []byte {
	payload := make([]byte, 8+len(bids)*8)
	payload[0] = 0x01
	payload[1] = level
	binary.LittleEndian.PutUint16(payload[2:4], uint16(len(bids)))
	for i, bid := range bids {
		binary.LittleEndian.PutUint64(payload[8+i*8:], bid)
	}
	return payload
}

func TestStreamPayloadFromSourceXBlockAndXXBlockSmoke(t *testing.T) {
	source := testPayloadSource{
		0x100: []byte("hello "),
		0x120: []byte("world"),
		0x102: makeTestExtendedBlock(1, 0x100, 0x120),
		0x106: makeTestExtendedBlock(2, 0x102),
	}
	var got bytes.Buffer
	result, err := streamPayloadFromSource(source, 0x106, 8, 64, func(chunk []byte) error {
		_, err := got.Write(chunk)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "hello world" || result.BlocksRead != 4 || result.BytesProduced != 11 || result.Truncated {
		t.Fatalf("unexpected XXBLOCK smoke result: %q %+v", got.String(), result)
	}
}

func TestStreamPayloadFromSourceRejectsTreeDamage(t *testing.T) {
	tests := []struct {
		name   string
		source testPayloadSource
		root   uint64
	}{
		{
			name: "cycle",
			source: testPayloadSource{
				0x102: makeTestExtendedBlock(1, 0x102),
			},
			root: 0x102,
		},
		{
			name: "level mismatch",
			source: testPayloadSource{
				0x100: []byte("data"),
				0x102: makeTestExtendedBlock(2, 0x100),
			},
			root: 0x102,
		},
		{
			name: "missing child",
			source: testPayloadSource{
				0x102: makeTestExtendedBlock(1, 0x100),
			},
			root: 0x102,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := streamPayloadFromSource(tc.source, tc.root, 8, 64, func([]byte) error { return nil })
			if err == nil {
				t.Fatal("damaged tree was accepted")
			}
		})
	}
}

func TestParseSubnodeBlockBoundedSmoke(t *testing.T) {
	leaf := make([]byte, 32)
	leaf[0], leaf[1] = 0x02, 0
	binary.LittleEndian.PutUint16(leaf[2:4], 1)
	binary.LittleEndian.PutUint64(leaf[8:16], 0x30)
	binary.LittleEndian.PutUint64(leaf[16:24], 0x300)
	block, err := parseSubnodeBlockBounded(leaf)
	if err != nil || !block.IsLeaf() || len(block.LeafEntries) != 1 || block.LeafEntries[0].NID != 0x30 || block.LeafEntries[0].DataBID != 0x300 {
		t.Fatalf("unexpected SLBLOCK: block=%+v err=%v", block, err)
	}

	branch := make([]byte, 24)
	branch[0], branch[1] = 0x02, 1
	binary.LittleEndian.PutUint16(branch[2:4], 1)
	binary.LittleEndian.PutUint64(branch[8:16], 0x40)
	binary.LittleEndian.PutUint64(branch[16:24], 0x302)
	block, err = parseSubnodeBlockBounded(branch)
	if err != nil || block.IsLeaf() || len(block.NonleafEntries) != 1 || block.NonleafEntries[0].SubBID != 0x302 {
		t.Fatalf("unexpected SIBLOCK: block=%+v err=%v", block, err)
	}

	truncated := leaf[:31]
	if _, err := parseSubnodeBlockBounded(truncated); err == nil {
		t.Fatal("truncated SLBLOCK was accepted")
	}
}

func makeTestSubnodeLeaf(nid, dataBID, subBID uint64) []byte {
	payload := make([]byte, 32)
	payload[0], payload[1] = 0x02, 0
	binary.LittleEndian.PutUint16(payload[2:4], 1)
	binary.LittleEndian.PutUint64(payload[8:16], nid)
	binary.LittleEndian.PutUint64(payload[16:24], dataBID)
	binary.LittleEndian.PutUint64(payload[24:32], subBID)
	return payload
}

func makeTestSubnodeBranch(childBID uint64) []byte {
	payload := make([]byte, 24)
	payload[0], payload[1] = 0x02, 1
	binary.LittleEndian.PutUint16(payload[2:4], 1)
	binary.LittleEndian.PutUint64(payload[8:16], 0x40)
	binary.LittleEndian.PutUint64(payload[16:24], childBID)
	return payload
}

func TestWalkSubnodeTreeFromSourceSmoke(t *testing.T) {
	source := testPayloadSource{
		0x202: makeTestSubnodeLeaf(0x30, 0x300, 0),
		0x206: makeTestSubnodeBranch(0x202),
	}
	entries, result, err := walkSubnodeTreeFromSource(source, 0x206, 8)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := entries[0x30]
	if !ok || entry.DataBID != 0x300 || result.BlocksRead != 2 || result.Branches != 1 || result.Leaves != 1 {
		t.Fatalf("unexpected subnode tree result: entries=%+v result=%+v", entries, result)
	}
}

func TestCollectSubnodePayloadsFromSourceSmoke(t *testing.T) {
	source := testPayloadSource{
		0x202: makeTestSubnodeLeaf(0x125, 0x300, 0),
		0x300: []byte("attachment-payload"),
	}
	entries, tree, err := walkSubnodeTreeFromSource(source, 0x202, 8)
	if err != nil {
		t.Fatal(err)
	}
	payloads := collectSubnodePayloadsFromSource(source, entries, 8, 1<<20)
	if tree.Leaves != 1 || len(payloads) != 1 || string(payloads[0x125]) != "attachment-payload" {
		t.Fatalf("unexpected subnode payload map: entries=%+v tree=%+v payloads=%q", entries, tree, payloads[0x125])
	}
	collector, err := NewRecoveredMessageCollector(4, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	added, err := collector.AddResolvedProperties(0x22, 0x200, []PropertyValue{{PropID: 0x0037, PropType: 0x001F, Data: []byte("subject")}})
	if err != nil || !added {
		t.Fatalf("message candidate was not added: %v %v", added, err)
	}
	if err := collector.AddAttachmentProperties(0x200, 0x125, []PropertyValue{{PropID: 0x3707, PropType: 0x001F, Data: []byte("file.bin")}, {PropID: 0x3701, PropType: 0x0102, Data: payloads[0x125]}}); err != nil {
		t.Fatal(err)
	}
	message := collector.Messages()[0]
	if len(message.Attachments) != 1 || string(message.Attachments[0].Data) != "attachment-payload" || message.Attachments[0].Incomplete {
		t.Fatalf("subnode attachment was not collected: %+v", message.Attachments)
	}
}

func TestParseHeapBlockSmoke(t *testing.T) {
	data := make([]byte, 64)
	binary.LittleEndian.PutUint16(data[0:2], 42)
	data[2], data[3] = 0xEC, 0xBC
	binary.LittleEndian.PutUint32(data[4:8], 0x20)
	binary.LittleEndian.PutUint16(data[42:44], 2)
	binary.LittleEndian.PutUint16(data[44:46], 0)
	for i, offset := range []uint16{12, 20, 30} {
		binary.LittleEndian.PutUint16(data[46+i*2:48+i*2], offset)
	}
	heap, err := ParseHeapBlock(0, data)
	if err != nil || heap.Client != 0xBC || heap.RootHID != 0x20 || len(heap.Allocations) != 3 || heap.Allocations[2] != 30 {
		t.Fatalf("unexpected HN block: %+v err=%v", heap, err)
	}
	copy(data[12:20], []byte("property"))
	allocation, err := ReadHeapAllocation([][]byte{data}, []HeapBlockScan{heap}, 0x20)
	if err != nil || string(allocation) != "property" {
		t.Fatalf("unexpected HID allocation: %q err=%v", allocation, err)
	}
	reader, err := NewHeapReader([][]byte{data})
	if err != nil || reader.RootHID() != 0x20 || reader.ClientSignature() != 0xBC {
		t.Fatalf("unexpected HN reader: reader=%+v err=%v", reader, err)
	}
	allocation, err = reader.Read(0x20)
	if err != nil || string(allocation) != "property" {
		t.Fatalf("unexpected HN reader allocation: %q err=%v", allocation, err)
	}
	value, err := ResolvePropertyValue(PropertyRecord{PropID: 1, PropType: 0x001F, ValueHNID: 0x20}, reader, nil)
	if err != nil || value.Source != "heap" || string(value.Data) != "property" {
		t.Fatalf("unexpected heap property value: %+v err=%v", value, err)
	}
	value, err = ResolvePropertyValue(PropertyRecord{PropID: 2, PropType: 0x0003, ValueHNID: 0x11223344}, nil, nil)
	if err != nil || value.Source != "inline" || !bytes.Equal(value.Data, []byte{0x44, 0x33, 0x22, 0x11}) {
		t.Fatalf("unexpected inline property value: %+v err=%v", value, err)
	}
	value, err = ResolvePropertyValue(PropertyRecord{PropID: 3, PropType: 0x0102, ValueHNID: 0x125}, nil, map[uint64][]byte{0x125: []byte("attachment")})
	if err != nil || value.Source != "subnode" || string(value.Data) != "attachment" {
		t.Fatalf("unexpected subnode property value: %+v err=%v", value, err)
	}
	if _, err := ReadHeapAllocation([][]byte{data}, []HeapBlockScan{heap}, 0x21); err == nil {
		t.Fatal("HID with reserved bits was accepted")
	}

	data[46], data[47] = 31, 0
	if _, err := ParseHeapBlock(0, data); err == nil {
		t.Fatal("allocation beyond page map was accepted")
	}
	data[46], data[47] = 12, 0
	binary.LittleEndian.PutUint16(data[42:44], 40)
	if _, err := ParseHeapBlock(0, data); err == nil {
		t.Fatal("truncated allocation table was accepted")
	}
}

func TestParseBTHHeaderAndLeafRecordsSmoke(t *testing.T) {
	headerBytes := make([]byte, 8)
	headerBytes[0], headerBytes[1], headerBytes[2], headerBytes[3] = 0xB5, 2, 6, 0
	binary.LittleEndian.PutUint32(headerBytes[4:8], 0x20)
	header, err := ParseBTHHeader(headerBytes)
	if err != nil || header.KeySize != 2 || header.EntrySize != 6 || header.RootHID != 0x20 {
		t.Fatalf("unexpected BTH header: %+v err=%v", header, err)
	}

	data := []byte{
		0x01, 0x00, 0xBC, 0x00, 0x20, 0x00, 0x00, 0x00,
		0x02, 0x00, 0x7C, 0x00, 0x40, 0x00, 0x00, 0x00,
	}
	records, err := ParseBTHLeafRecords(data, header, 4)
	if err != nil || len(records) != 2 || records[0].Key[0] != 1 || records[1].Value[0] != 0x7C {
		t.Fatalf("unexpected BTH records: %+v err=%v", records, err)
	}
	if _, err := ParseBTHHeader([]byte{0xB5, 3, 6, 0, 0, 0, 0, 0}); err == nil {
		t.Fatal("unsupported BTH key size was accepted")
	}
	if _, err := ParseBTHLeafRecords(data[:7], header, 4); err == nil {
		t.Fatal("partial BTH leaf record was accepted")
	}
	properties, err := ParsePropertyRecords(records)
	if err != nil || len(properties) != 2 || properties[0].PropID != 1 || properties[0].PropType != 0x00BC || properties[0].ValueHNID != 0x20 {
		t.Fatalf("unexpected property records: %+v err=%v", properties, err)
	}
	if _, err := ParsePropertyRecords([]BTHRecord{{Key: []byte{1}, Value: make([]byte, 6)}}); err == nil {
		t.Fatal("malformed property record was accepted")
	}
}

func TestParsePropertyContextSmoke(t *testing.T) {
	data := make([]byte, 64)
	binary.LittleEndian.PutUint16(data[0:2], 54)
	data[2], data[3] = 0xEC, 0xBC
	binary.LittleEndian.PutUint32(data[4:8], 0x20)
	// HN allocation 0: BTHHEADER, allocation 1: two PC records.
	data[12] = 0xB5
	data[13], data[14], data[15] = 2, 6, 0
	binary.LittleEndian.PutUint32(data[16:20], 0x40)
	binary.LittleEndian.PutUint16(data[20:22], 1)
	binary.LittleEndian.PutUint16(data[22:24], 0x001F)
	binary.LittleEndian.PutUint32(data[24:28], 0x20)
	binary.LittleEndian.PutUint16(data[28:30], 2)
	binary.LittleEndian.PutUint16(data[30:32], 0x0003)
	binary.LittleEndian.PutUint32(data[32:36], 0x11223344)
	binary.LittleEndian.PutUint16(data[54:56], 2)
	binary.LittleEndian.PutUint16(data[56:58], 0)
	for i, offset := range []uint16{12, 20, 36} {
		binary.LittleEndian.PutUint16(data[58+i*2:60+i*2], offset)
	}
	heap, err := NewHeapReader([][]byte{data})
	if err != nil {
		t.Fatal(err)
	}
	properties, err := ParsePropertyContext(heap, 8)
	if err != nil || len(properties) != 2 || properties[0].PropID != 1 || properties[1].ValueHNID != 0x11223344 {
		t.Fatalf("unexpected PC: %+v err=%v", properties, err)
	}
}

func TestParseTableContextHeaderSmoke(t *testing.T) {
	headerData := make([]byte, 30)
	headerData[0], headerData[1] = 0x7C, 1
	for i, value := range []uint16{8, 16, 20, 24} {
		binary.LittleEndian.PutUint16(headerData[2+i*2:4+i*2], value)
	}
	binary.LittleEndian.PutUint32(headerData[10:14], 0x60)
	binary.LittleEndian.PutUint32(headerData[14:18], 0x85)
	binary.LittleEndian.PutUint16(headerData[22:24], 0x0003)
	binary.LittleEndian.PutUint16(headerData[24:26], 0x0037)
	binary.LittleEndian.PutUint16(headerData[26:28], 4)
	headerData[28], headerData[29] = 4, 0
	header, err := ParseTableContextHeader(headerData, 8)
	if err != nil || header.ColumnCount != 1 || header.RowBTHID != 0x60 || header.Columns[0].PropID != 0x0037 {
		t.Fatalf("unexpected TC header: %+v err=%v", header, err)
	}
	if _, err := ParseTableContextHeader(headerData, 0); err == nil {
		t.Fatal("zero TC column limit was accepted")
	}
	headerData[0] = 0xBC
	if _, err := ParseTableContextHeader(headerData, 8); err == nil {
		t.Fatal("PC signature was accepted as TC")
	}
}

func TestParseTableContextRowsSmoke(t *testing.T) {
	data := make([]byte, 128)
	binary.LittleEndian.PutUint16(data[0:2], 90)
	data[2], data[3] = 0xEC, 0x7C
	binary.LittleEndian.PutUint32(data[4:8], 0x20)
	// Allocation 0: TCINFO, allocation 1: row BTH header,
	// allocation 2: row BTH leaf, allocation 3: heap-backed row matrix.
	data[12], data[13] = 0x7C, 1
	for i, value := range []uint16{4, 4, 6, 8} {
		binary.LittleEndian.PutUint16(data[14+i*2:16+i*2], value)
	}
	binary.LittleEndian.PutUint32(data[22:26], 0x40)
	binary.LittleEndian.PutUint32(data[26:30], 0x80)
	binary.LittleEndian.PutUint16(data[34:36], 0x0003)
	binary.LittleEndian.PutUint16(data[36:38], 0x0037)
	binary.LittleEndian.PutUint16(data[38:40], 0)
	data[40], data[41] = 4, 0
	data[42], data[43], data[44], data[45] = 0xB5, 4, 4, 0
	binary.LittleEndian.PutUint32(data[46:50], 0x60)
	binary.LittleEndian.PutUint32(data[50:54], 7)
	binary.LittleEndian.PutUint32(data[54:58], 0)
	binary.LittleEndian.PutUint32(data[58:62], 0x11223344)
	binary.LittleEndian.PutUint32(data[62:66], 0x55667788)
	binary.LittleEndian.PutUint16(data[90:92], 4)
	binary.LittleEndian.PutUint16(data[92:94], 0)
	for i, offset := range []uint16{12, 42, 50, 58, 66} {
		binary.LittleEndian.PutUint16(data[94+i*2:96+i*2], offset)
	}
	heap, err := NewHeapReader([][]byte{data})
	if err != nil {
		t.Fatal(err)
	}
	header, err := ParseTableContext(heap, 8)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := ParseTableContextRows(heap, header, 8)
	if err != nil || len(rows) != 1 || rows[0].RowID != 7 || !bytes.Equal(rows[0].Data, []byte{0x44, 0x33, 0x22, 0x11, 0x88, 0x77, 0x66, 0x55}) {
		t.Fatalf("unexpected TC rows: %+v err=%v", rows, err)
	}
	header.RowMatrixHNID = 0x85
	if _, err := ParseTableContextRows(heap, header, 8); err == nil {
		t.Fatal("subnode-backed row matrix was silently accepted")
	}
}

func TestDecodeTableRowAndIdentifySignals(t *testing.T) {
	header := TableContextHeader{
		SizeOffsets: [4]uint16{4, 6, 6, 8},
		Columns: []TableColumn{
			{PropID: 0x0037, PropType: 0x001F, Offset: 0, Size: 4, BitOffset: 0},
			{PropID: 0x3001, PropType: 0x001F, Offset: 4, Size: 2, BitOffset: 1},
		},
	}
	row := TableRowData{RowID: 7, Data: []byte{1, 2, 3, 4, 0, 0, 0x01, 0}}
	cells, err := DecodeTableRow(header, row)
	if err != nil || len(cells) != 2 || !cells[0].Present || cells[1].Present || !bytes.Equal(cells[0].Data, []byte{1, 2, 3, 4}) {
		t.Fatalf("unexpected decoded cells: %+v err=%v", cells, err)
	}
	signals := IdentifyTableRow(cells)
	if !signals.Subject || signals.DisplayName || signals.Candidate != "message-candidate" {
		t.Fatalf("unexpected row signals: %+v", signals)
	}
	header.Columns[0].Offset = 7
	if _, err := DecodeTableRow(header, row); err == nil {
		t.Fatal("out-of-bounds TC cell was accepted")
	}
}

func TestRecoveredMessageCollectorSmoke(t *testing.T) {
	collector, err := NewRecoveredMessageCollector(2, 4)
	if err != nil {
		t.Fatal(err)
	}
	added, err := collector.AddResolvedProperties(0x22, 0x200, []PropertyValue{
		{PropID: 0x001A, PropType: 0x001F, Data: []byte{'I', 0, 'P', 0, 'M', 0, '.', 0, 'N', 0, 'o', 0, 't', 0, 'e', 0, 0, 0}},
		{PropID: 0x0037, PropType: 0x001F, Data: []byte{'T', 0, 'e', 0, 's', 0, 't', 0, 0, 0}},
		{PropID: 0x0039, PropType: 0x0040, Data: func() []byte {
			data := make([]byte, 8)
			binary.LittleEndian.PutUint64(data, 116444736000000000+1704067200*10000000)
			return data
		}()},
		{PropID: 0x1000, PropType: 0x0102, Data: []byte("123456")},
	})
	if err != nil || !added {
		t.Fatalf("message was not collected: added=%v err=%v", added, err)
	}
	messages := collector.Messages()
	if len(messages) != 1 || messages[0].MessageClass != "IPM.Note" || messages[0].Subject != "Test" || messages[0].DateUnix != 1704067200 || string(messages[0].Body) != "1234" || !messages[0].Incomplete {
		t.Fatalf("unexpected recovered message: %+v", messages)
	}
	if err := collector.AddAttachmentProperties(0x200, 0x301, []PropertyValue{
		{PropID: 0x3707, PropType: 0x001F, Data: []byte{'a', 0, '.', 0, 't', 0, 'x', 0, 't', 0, 0, 0}},
		{PropID: 0x370E, PropType: 0x001F, Data: []byte{'t', 0, 'e', 0, 'x', 0, 't', 0, '/', 0, 'p', 0, 'l', 0, 'a', 0, 'i', 0, 'n', 0, 0, 0}},
		{PropID: 0x0E20, PropType: 0x0003, Data: []byte{3, 0, 0, 0}},
		{PropID: 0x3701, PropType: 0x0102, Data: []byte("abc")},
	}); err != nil {
		t.Fatal(err)
	}
	messages = collector.Messages()
	if len(messages[0].Attachments) != 1 || messages[0].Attachments[0].Name != "a.txt" || messages[0].Attachments[0].DeclaredSize != 3 || string(messages[0].Attachments[0].Data) != "abc" {
		t.Fatalf("unexpected recovered attachment: %+v", messages[0].Attachments)
	}
	if added, err := collector.AddResolvedProperties(0x22, 0x200, nil); err != nil || added {
		t.Fatalf("duplicate BID was not suppressed: added=%v err=%v", added, err)
	}
	if added, err := collector.AddResolvedProperties(0x23, 0x201, []PropertyValue{{PropID: 0x3001, Data: []byte("folder")}}); err != nil || added {
		t.Fatalf("non-message candidate was collected: added=%v err=%v", added, err)
	}
	addedCount, rejected, duplicates, limited := collector.Stats()
	if addedCount != 1 || rejected != 1 || duplicates != 1 || limited {
		t.Fatalf("unexpected collector stats: %d %d %d %v", addedCount, rejected, duplicates, limited)
	}
}

func TestWalkIndexedSubnodeTreeRejectsExternalRoot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subnode.pff")
	if err := os.WriteFile(path, make([]byte, 1), 0o600); err != nil {
		t.Fatal(err)
	}
	_, result, err := WalkIndexedSubnodeTree(path, Header{PageSize: 512}, 1, 0x10, 8)
	if err == nil || result.BlocksRead != 0 {
		t.Fatalf("unexpected subnode root result: %+v err=%v", result, err)
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

func TestStreamIndexedPayloadMatchesAssemblyOnCorpus(t *testing.T) {
	path := filepath.Join("..", "..", ".gomodcache", "github.com", "useti", "go-pst", "v6@v6.0.8", "data", "support.pst")
	if _, err := os.Stat(path); err != nil {
		t.Skip("go-pst corpus not available")
	}
	h := ReadHeader(path)
	fileSize := fileSizeForTest(t, path)
	nbt, _ := WalkIndexTrees(path, h, fileSize, 100000, 64)
	seen := make(map[uint64]bool)
	checked := 0
	for _, node := range nbt.nodeBIDs {
		if node.dataBID == 0 || seen[node.dataBID] {
			continue
		}
		seen[node.dataBID] = true
		want, wantResult, err := AssembleIndexedPayload(path, h, fileSize, node.dataBID, 128, 1<<20)
		if err != nil {
			continue
		}
		var got bytes.Buffer
		gotResult, err := StreamIndexedPayload(path, h, fileSize, node.dataBID, 128, 1<<20, func(chunk []byte) error {
			_, err := got.Write(chunk)
			return err
		})
		if err != nil {
			t.Fatalf("stream BID 0x%X: %v", node.dataBID, err)
		}
		if !bytes.Equal(got.Bytes(), want) || gotResult.BytesProduced != wantResult.BytesProduced || gotResult.Truncated != wantResult.Truncated {
			t.Fatalf("stream mismatch for BID 0x%X: got=%+v want=%+v bytes=%d/%d", node.dataBID, gotResult, wantResult, got.Len(), len(want))
		}
		checked++
		if checked == 8 {
			break
		}
	}
	if checked == 0 {
		t.Skip("corpus has no assemblable data nodes")
	}
}

func TestScanIndexedPropertyContextsOnCorpusIsBounded(t *testing.T) {
	path := filepath.Join("..", "..", ".gomodcache", "github.com", "useti", "go-pst", "v6@v6.0.8", "data", "support.pst")
	if _, err := os.Stat(path); err != nil {
		t.Skip("go-pst corpus not available")
	}
	h := ReadHeader(path)
	scan := ScanIndexedPropertyContexts(path, h, fileSizeForTest(t, path), 8, 32, 1<<20, 128, 4)
	if scan.Status != "completed" || scan.NodesAttempted == 0 || scan.NodesAttempted > 8 {
		t.Fatalf("unexpected bounded PC scan: %+v", scan)
	}
	if len(scan.ErrorSamples) > 4 || scan.Errors < len(scan.ErrorSamples) {
		t.Fatalf("invalid bounded error sample accounting: %+v", scan)
	}
}

func TestWalkIndexedSubnodeTreeOnCorpus(t *testing.T) {
	path := filepath.Join("..", "..", ".gomodcache", "github.com", "useti", "go-pst", "v6@v6.0.8", "data", "support.pst")
	if _, err := os.Stat(path); err != nil {
		t.Skip("go-pst corpus not available")
	}
	h := ReadHeader(path)
	fileSize := fileSizeForTest(t, path)
	nbt, _ := WalkIndexTrees(path, h, fileSize, 100000, 64)
	for _, node := range nbt.nodeBIDs {
		if node.subnodeBID == 0 {
			continue
		}
		entries, result, err := WalkIndexedSubnodeTree(path, h, fileSize, node.subnodeBID, 1024)
		if err != nil {
			t.Fatalf("walk subnode BID 0x%X: %v", node.subnodeBID, err)
		}
		if result.BlocksRead == 0 || result.Leaves == 0 || len(entries) == 0 {
			t.Fatalf("subnode BID 0x%X produced no leaf data: result=%+v entries=%d", node.subnodeBID, result, len(entries))
		}
		return
	}
	t.Skip("corpus has no subnode roots")
}

func TestCollectIndexedSubnodePayloadsOnCorpus(t *testing.T) {
	path := filepath.Join("..", "..", ".gomodcache", "github.com", "useti", "go-pst", "v6@v6.0.8", "data", "support.pst")
	if _, err := os.Stat(path); err != nil {
		t.Skip("go-pst corpus not available")
	}
	h := ReadHeader(path)
	fileSize := fileSizeForTest(t, path)
	nbt, _ := WalkIndexTrees(path, h, fileSize, 100000, 64)
	for _, node := range nbt.nodeBIDs {
		if node.subnodeBID == 0 {
			continue
		}
		payloads, tree, err := CollectIndexedSubnodePayloads(path, h, fileSize, node.subnodeBID, 128, 1<<20)
		if err != nil {
			t.Fatalf("collect subnode payloads BID 0x%X: %v", node.subnodeBID, err)
		}
		if tree.BlocksRead == 0 || tree.Leaves == 0 {
			t.Fatalf("subnode BID 0x%X was not traversed: tree=%+v", node.subnodeBID, tree)
		}
		if len(payloads) > tree.Entries {
			t.Fatalf("payload map exceeds leaf entries: %d > %d", len(payloads), tree.Entries)
		}
		return
	}
	t.Skip("corpus has no subnode roots")
}

func TestCollectIndexedRecoveredMessagesOnCorpus(t *testing.T) {
	path := filepath.Join("..", "..", ".gomodcache", "github.com", "useti", "go-pst", "v6@v6.0.8", "data", "support.pst")
	if _, err := os.Stat(path); err != nil {
		t.Skip("go-pst corpus not available")
	}
	h := ReadHeader(path)
	scan, err := CollectIndexedRecoveredMessages(path, h, fileSizeForTest(t, path), 8, 32, 1<<20, 128, 16, 1<<20, 8)
	if err != nil {
		t.Fatal(err)
	}
	if scan.Status != "completed" || scan.NodesAttempted == 0 || scan.NodesAttempted > 8 || len(scan.ErrorSamples) > 8 {
		t.Fatalf("unexpected bounded native corpus scan: %+v", scan)
	}
	for _, message := range scan.Messages {
		if message.BID == 0 || len(message.Attachments) > 16 {
			t.Fatalf("invalid native recovered message: %+v", message)
		}
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
