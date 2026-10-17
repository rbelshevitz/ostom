// Package format contains conservative, read-only PFF/OFF header detection.
// It deliberately reports an unsupported or unknown variant instead of
// classifying it as corruption.
package format

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"github.com/grokify/outlook-pst-go/pkg/disk"
)

type Header struct {
	OK           bool
	ContentType  string
	NDBVersion   uint16
	PageSize     int
	Variant      string
	Compression  string
	CryptMethod  disk.CryptMethod `json:"-"`
	Encryption   string
	HeaderCRC    string
	DeclaredSize uint64
	NBTOffset    uint64
	BBTOffset    uint64
	Roots        string
	Allocation   AllocationScan
}

type AllocationScan struct {
	PagesChecked int
	PagesValid   int
	PagesInvalid int
	Status       string
}

type IndexRootScan struct {
	NBT        string
	BBT        string
	Status     string
	NBTEntries int
	NBTMax     int
	NBTSize    int
	NBTLevel   int
	BBTEntries int
	BBTMax     int
	BBTSize    int
	BBTLevel   int
	NBTRefs    int
	BBTRefs    int
	BadRefs    int
}

type IndexTreeScan struct {
	PagesVisited   int    `json:"pages_visited"`
	Leaves         int    `json:"leaves"`
	Branches       int    `json:"branches"`
	BlockRefs      int    `json:"block_refs"`
	BlockBytes     uint64 `json:"block_bytes"`
	BadBlocks      int    `json:"bad_block_refs"`
	NodeRefs       int    `json:"node_refs"`
	DataBIDs       int    `json:"data_bids"`
	SubnodeBIDs    int    `json:"subnode_bids"`
	ResolvedData   int    `json:"resolved_data_bids"`
	MissingData    int    `json:"missing_data_bids"`
	ResolvedSub    int    `json:"resolved_subnode_bids"`
	MissingSub     int    `json:"missing_subnode_bids"`
	CRCInvalid     int    `json:"crc_invalid_advisory"`
	InvalidPages   int    `json:"invalid_pages"`
	BadRefs        int    `json:"bad_refs"`
	Cycles         int    `json:"cycles"`
	MaxDepth       int    `json:"max_depth"`
	Status         string `json:"status"`
	blockBIDs      map[uint64]bool
	nodeBIDs       []nodeRef
	blockLocations map[uint64]blockLocation
}

type blockLocation struct {
	offset uint64
	size   uint16
}

// PayloadScan describes the NBT-referenced block payloads that could be read.
// Classification is deliberately conservative: a byte value is reported as a
// block-family hint, not as proof that the complete node/message is valid.
type PayloadScan struct {
	BlocksReferenced    int    `json:"blocks_referenced"`
	BlocksDiscovered    int    `json:"blocks_discovered"`
	BlocksRead          int    `json:"blocks_read"`
	ExternalBlocks      int    `json:"external_blocks"`
	ExtendedBlocks      int    `json:"extended_blocks"`
	SubnodeBlocks       int    `json:"subnode_blocks"`
	UnknownBlocks       int    `json:"unknown_blocks"`
	ExtendedParsed      int    `json:"extended_parsed"`
	SubnodeParsed       int    `json:"subnode_parsed"`
	ExtendedReferences  int    `json:"extended_references"`
	SubnodeEntries      int    `json:"subnode_entries"`
	LogicalBytes        uint64 `json:"logical_bytes"`
	DecodeErrors        int    `json:"decode_errors"`
	ReadErrors          int    `json:"read_errors"`
	TrailersChecked     int    `json:"trailers_checked"`
	TrailersValid       int    `json:"trailers_valid"`
	TrailersInvalid     int    `json:"trailers_invalid"`
	TrailerSizeMismatch int    `json:"trailer_size_mismatch"`
	TrailerCRCInvalid   int    `json:"trailer_crc_invalid"`
	TrailerSigInvalid   int    `json:"trailer_signature_invalid"`
	TrailerBIDInvalid   int    `json:"trailer_bid_invalid"`
	LimitReached        bool   `json:"limit_reached"`
	Status              string `json:"status"`
}

// AssemblyResult records bounded logical-payload assembly for one BID.
type AssemblyResult struct {
	BlocksRead    int    `json:"blocks_read"`
	BytesProduced uint64 `json:"bytes_produced"`
	Truncated     bool   `json:"truncated"`
}

type NodePayloadScan struct {
	NodesChecked     int    `json:"nodes_checked"`
	HeapNodes        int    `json:"heap_nodes"`
	PropertyContexts int    `json:"property_contexts"`
	TableContexts    int    `json:"table_contexts"`
	UnknownPayloads  int    `json:"unknown_payloads"`
	ReadErrors       int    `json:"read_errors"`
	LimitReached     bool   `json:"limit_reached"`
	Status           string `json:"status"`
}

// ScanIndexedNodeHeaders identifies the first-level Heap-on-Node signatures
// for unique NBT data BIDs. It is a bounded classification pass, not yet a
// property/table decoder.
func ScanIndexedNodeHeaders(name string, h Header, fileSize int64, maxNodes int) NodePayloadScan {
	scan := NodePayloadScan{Status: "not applicable"}
	if maxNodes <= 0 || fileSize <= 0 || h.PageSize != 512 {
		return scan
	}
	nbt, bbt := WalkIndexTrees(name, h, fileSize, 100000, 64)
	if bbt.blockLocations == nil {
		scan.Status = "BBT locations unavailable"
		return scan
	}
	f, err := os.Open(name)
	if err != nil {
		scan.Status = "unavailable: " + err.Error()
		return scan
	}
	defer f.Close()
	seen := make(map[uint64]bool)
	for _, node := range nbt.nodeBIDs {
		if node.dataBID == 0 || seen[node.dataBID] {
			continue
		}
		seen[node.dataBID] = true
		if scan.NodesChecked >= maxNodes {
			scan.LimitReached = true
			break
		}
		location, ok := bbt.blockLocations[node.dataBID]
		if !ok || location.size < 4 || location.offset+4 > uint64(fileSize) {
			scan.ReadErrors++
			continue
		}
		var header [4]byte
		if _, err := f.ReadAt(header[:], int64(location.offset)); err != nil {
			scan.ReadErrors++
			continue
		}
		scan.NodesChecked++
		headerPayload := decodeBlockPayload(header[:], node.dataBID, h.CryptMethod)
		if len(headerPayload) >= 4 {
			copy(header[:], headerPayload[:4])
		}
		if header[0] == 0x01 {
			logical, _, assembleErr := assemblePayloadFromLocations(f, bbt.blockLocations, fileSize, node.dataBID, h.CryptMethod, 128, 4<<20)
			if assembleErr == nil && len(logical) >= 4 {
				copy(header[:], logical[:4])
			} else if assembleErr != nil {
				scan.ReadErrors++
				continue
			}
		}
		if header[2] != 0xEC {
			scan.UnknownPayloads++
			continue
		}
		scan.HeapNodes++
		switch header[3] {
		case 0xBC:
			scan.PropertyContexts++
		case 0x7C:
			scan.TableContexts++
		}
	}
	scan.Status = "completed"
	return scan
}

func assemblePayloadFromLocations(f *os.File, locations map[uint64]blockLocation, fileSize int64, bid uint64, method disk.CryptMethod, maxBlocks int, maxBytes uint64) ([]byte, AssemblyResult, error) {
	result := AssemblyResult{}
	active := make(map[uint64]bool)
	var assemble func(uint64) ([]byte, error)
	assemble = func(current uint64) ([]byte, error) {
		if active[current] {
			return nil, fmt.Errorf("payload cycle at BID 0x%X", current)
		}
		if result.BlocksRead >= maxBlocks {
			result.Truncated = true
			return nil, nil
		}
		location, ok := locations[current]
		if !ok {
			return nil, fmt.Errorf("BID 0x%X not found in BBT", current)
		}
		aligned := (uint64(location.size) + 63) &^ 63
		total := aligned + 16
		if location.offset+total < location.offset || location.offset+total > uint64(fileSize) {
			return nil, fmt.Errorf("BID 0x%X block exceeds file", current)
		}
		data := make([]byte, total)
		if _, err := f.ReadAt(data, int64(location.offset)); err != nil {
			return nil, err
		}
		result.BlocksRead++
		payload := decodeBlockPayload(data[:location.size], current, method)
		if len(payload) == 0 || payload[0] != 0x01 {
			return append([]byte(nil), payload...), nil
		}
		if len(payload) < 8 {
			return nil, fmt.Errorf("BID 0x%X has truncated extended header", current)
		}
		extended, err := disk.ParseExtendedBlock(payload, disk.FormatUnicode)
		if err != nil {
			return nil, err
		}
		active[current] = true
		defer delete(active, current)
		out := make([]byte, 0, extended.TotalSize)
		for _, child := range extended.BIDs {
			part, err := assemble(child)
			if err != nil {
				return nil, err
			}
			remaining := maxBytes - uint64(len(out))
			if uint64(len(part)) > remaining {
				part = part[:remaining]
				result.Truncated = true
			}
			out = append(out, part...)
			if uint64(len(out)) >= maxBytes {
				result.Truncated = true
				break
			}
		}
		return out, nil
	}
	data, err := assemble(bid)
	result.BytesProduced = uint64(len(data))
	return data, result, err
}

// decodeBlockPayload applies the PFF data-block encoding after trailer checks.
// Internal blocks (BID bit 1) are never encoded; only external data blocks use
// bCryptMethod. Keeping this operation separate prevents encoded bytes from
// being mistaken for an XBLOCK or an HN heap header.
func decodeBlockPayload(payload []byte, bid uint64, method disk.CryptMethod) []byte {
	if bid&0x2 != 0 {
		return payload
	}
	return disk.DecryptBlock(payload, method, bid)
}

// AssembleIndexedPayload follows one NBT/BBT data tree and returns its logical
// bytes. It is intentionally bounded and supports the ordinary Unicode
// layout; callers must treat errors and truncation as salvage evidence.
func AssembleIndexedPayload(name string, h Header, fileSize int64, bid uint64, maxBlocks int, maxBytes uint64) ([]byte, AssemblyResult, error) {
	result := AssemblyResult{}
	if h.PageSize != 512 {
		return nil, result, fmt.Errorf("payload assembly for page size %d is not implemented", h.PageSize)
	}
	if maxBlocks <= 0 || maxBytes == 0 {
		return nil, result, fmt.Errorf("payload assembly limits must be positive")
	}
	_, bbt := WalkIndexTrees(name, h, fileSize, 100000, 64)
	if bbt.blockLocations == nil {
		return nil, result, fmt.Errorf("BBT locations unavailable")
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, result, err
	}
	defer f.Close()
	active := make(map[uint64]bool)
	var assemble func(uint64) ([]byte, error)
	assemble = func(current uint64) ([]byte, error) {
		if active[current] {
			return nil, fmt.Errorf("payload cycle at BID 0x%X", current)
		}
		if result.BlocksRead >= maxBlocks {
			result.Truncated = true
			return nil, nil
		}
		location, ok := bbt.blockLocations[current]
		if !ok {
			return nil, fmt.Errorf("BID 0x%X not found in BBT", current)
		}
		aligned := (uint64(location.size) + 63) &^ 63
		total := aligned + 16
		if location.offset+total < location.offset || location.offset+total > uint64(fileSize) {
			return nil, fmt.Errorf("BID 0x%X block exceeds file", current)
		}
		data := make([]byte, total)
		if _, err := f.ReadAt(data, int64(location.offset)); err != nil {
			return nil, err
		}
		result.BlocksRead++
		payload := decodeBlockPayload(data[:location.size], current, h.CryptMethod)
		if len(payload) == 0 {
			return nil, nil
		}
		if payload[0] != 0x01 {
			return append([]byte(nil), payload...), nil
		}
		if len(payload) < 8 {
			return nil, fmt.Errorf("BID 0x%X has truncated extended header", current)
		}
		extended, err := disk.ParseExtendedBlock(payload, disk.FormatUnicode)
		if err != nil {
			return nil, err
		}
		active[current] = true
		defer delete(active, current)
		out := make([]byte, 0, extended.TotalSize)
		for _, child := range extended.BIDs {
			part, err := assemble(child)
			if err != nil {
				return nil, err
			}
			remaining := maxBytes - uint64(len(out))
			if uint64(len(part)) > remaining {
				part = part[:remaining]
				result.Truncated = true
			}
			out = append(out, part...)
			if uint64(len(out)) >= maxBytes {
				result.Truncated = true
				break
			}
		}
		return out, nil
	}
	data, err := assemble(bid)
	result.BytesProduced = uint64(len(data))
	return data, result, err
}

func ReadHeader(name string) Header {
	f, err := os.Open(name)
	if err != nil {
		return Header{}
	}
	defer f.Close()
	buf := make([]byte, 528)
	if _, err := io.ReadFull(f, buf); err != nil || string(buf[:4]) != "!BDN" {
		// A truncated file can still have enough bytes for the compact header.
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return Header{}
		}
		short := make([]byte, 16)
		if _, err := io.ReadFull(f, short); err != nil || string(short[:4]) != "!BDN" {
			return Header{}
		}
		copy(buf, short)
	}

	h := Header{
		OK:          true,
		ContentType: string(buf[8:10]),
		NDBVersion:  binary.LittleEndian.Uint16(buf[10:12]),
		PageSize:    512,
		Variant:     "unknown PFF variant",
		Compression: "not determined",
	}
	if len(buf) > 513 {
		h.CryptMethod = disk.CryptMethod(buf[513])
		h.Encryption = h.CryptMethod.String()
	}
	switch h.NDBVersion {
	case 14, 15:
		h.Variant = "32-bit ANSI"
	case 21, 23:
		h.Variant = "64-bit Unicode, 512-byte pages"
	case 36:
		h.PageSize = 4096
		h.Variant = "64-bit Unicode, 4 KiB pages"
		h.Compression = "DEFLATE blocks possible; block scan pending"
	default:
		h.PageSize = 0
	}
	if h.NDBVersion == 14 || h.NDBVersion == 15 {
		if len(buf) >= 472 {
			expected := binary.LittleEndian.Uint32(buf[4:8])
			actual := disk.ComputeCRC(buf[8:472])
			h.HeaderCRC = crcStatus(expected, actual)
			h.DeclaredSize = uint64(binary.LittleEndian.Uint32(buf[168:172]))
		}
	} else if h.NDBVersion >= 23 {
		if len(buf) >= 248 {
			if h.NDBVersion != 36 && len(buf) >= 524 {
				expected := binary.LittleEndian.Uint32(buf[4:8])
				// The partial CRC covers this range while the full-CRC field
				// (520:524) is still zero. It is written afterwards.
				crcInput := append([]byte(nil), buf[8:524]...)
				for i := 512; i < len(crcInput); i++ {
					crcInput[i] = 0
				}
				actual := disk.ComputeCRC(crcInput)
				h.HeaderCRC = crcStatus(expected, actual)
			}
			h.DeclaredSize = binary.LittleEndian.Uint64(buf[184:192])
			if len(buf) >= 248 {
				h.NBTOffset = binary.LittleEndian.Uint64(buf[224:232])
				h.BBTOffset = binary.LittleEndian.Uint64(buf[240:248])
				h.Roots = validateRoots(h.NBTOffset, h.BBTOffset, h.DeclaredSize, h.PageSize)
			}
		}
		if h.NDBVersion == 36 {
			h.HeaderCRC = "not determined for 4 KiB variant"
		}
	}
	return h
}

func validateRoots(nbt, bbt, fileSize uint64, pageSize int) string {
	if nbt == 0 || bbt == 0 {
		return "missing NBT/BBT root"
	}
	if fileSize > 0 && (nbt >= fileSize || bbt >= fileSize) {
		return "root outside declared file size"
	}
	if pageSize > 0 && (nbt%uint64(pageSize) != 0 || bbt%uint64(pageSize) != 0) {
		return "root offset is not page-aligned"
	}
	return "plausible"
}

func crcStatus(expected, actual uint32) string {
	if expected == actual {
		return "valid"
	}
	return "invalid"
}

// ScanAllocationPages checks known allocation-map locations only. It does not
// treat every 512-byte chunk as a page because PFF data blocks are interleaved.
func ScanAllocationPages(name string, h Header, fileSize int64) AllocationScan {
	if h.NDBVersion == 36 {
		return scan4KAllocationPages(name, fileSize)
	}
	if (h.NDBVersion != 21 && h.NDBVersion != 23) || fileSize <= 0 {
		return AllocationScan{Status: "not applicable"}
	}
	f, err := os.Open(name)
	if err != nil {
		return AllocationScan{Status: "unavailable: " + err.Error()}
	}
	defer f.Close()
	const (
		amapStart  int64 = 0x4400
		pmapStart  int64 = 0x4600
		amapPeriod int64 = 496 * 8 * 64
		pmapPeriod int64 = 496 * 8 * 512
	)
	scan := AllocationScan{Status: "completed"}
	for offset := amapStart; offset < fileSize; offset += amapPeriod {
		scanAllocationPage(f, offset, 0x84, &scan)
	}
	for offset := pmapStart; offset < fileSize; offset += pmapPeriod {
		scanAllocationPage(f, offset, 0x83, &scan)
	}
	return scan
}

func scan4KAllocationPages(name string, fileSize int64) AllocationScan {
	if fileSize <= 0 {
		return AllocationScan{Status: "not applicable"}
	}
	f, err := os.Open(name)
	if err != nil {
		return AllocationScan{Status: "unavailable: " + err.Error()}
	}
	defer f.Close()
	const (
		amapStart  int64 = 0x22000
		amapPeriod int64 = 4072 * 8 * 512
	)
	scan := AllocationScan{Status: "completed"}
	for offset := amapStart; offset < fileSize; offset += amapPeriod {
		var page [4096]byte
		if _, err := f.ReadAt(page[:], offset); err != nil {
			continue
		}
		scan.PagesChecked++
		if page[4072] != 0x84 || page[4073] != 0x84 || binary.LittleEndian.Uint32(page[4076:4080]) != disk.ComputeCRC(page[:4072]) {
			scan.PagesInvalid++
			continue
		}
		scan.PagesValid++
	}
	return scan
}

func scanAllocationPage(f *os.File, offset int64, pageType byte, scan *AllocationScan) {
	var page [512]byte
	if _, err := f.ReadAt(page[:], offset); err != nil {
		return
	}
	scan.PagesChecked++
	if page[496] != pageType || page[497] != pageType || binary.LittleEndian.Uint32(page[500:504]) != disk.ComputeCRC(page[:496]) {
		scan.PagesInvalid++
		return
	}
	scan.PagesValid++
}

// ScanIndexRoots validates the two root index pages referenced by the header.
// It deliberately checks only the roots; walking every B-tree node is a later
// recovery stage.
func ScanIndexRoots(name string, h Header, fileSize int64) IndexRootScan {
	scan := IndexRootScan{Status: "not applicable"}
	if h.NBTOffset == 0 || h.BBTOffset == 0 || h.PageSize == 0 {
		return scan
	}
	f, err := os.Open(name)
	if err != nil {
		scan.Status = "unavailable: " + err.Error()
		return scan
	}
	defer f.Close()
	scan.Status = "completed"
	scan.NBT, scan.NBTEntries, scan.NBTMax, scan.NBTSize, scan.NBTLevel, scan.NBTRefs, scan.BadRefs = validateIndexRoot(f, h.NBTOffset, h.PageSize, fileSize, 0x81)
	var bbtBad int
	scan.BBT, scan.BBTEntries, scan.BBTMax, scan.BBTSize, scan.BBTLevel, scan.BBTRefs, bbtBad = validateIndexRoot(f, h.BBTOffset, h.PageSize, fileSize, 0x80)
	scan.BadRefs += bbtBad
	return scan
}

func validateIndexRoot(f *os.File, offset uint64, pageSize int, fileSize int64, expectedType byte) (string, int, int, int, int, int, int) {
	if fileSize > 0 && (offset+uint64(pageSize) > uint64(fileSize)) {
		return "outside file", 0, 0, 0, 0, 0, 0
	}
	page := make([]byte, pageSize)
	if _, err := f.ReadAt(page, int64(offset)); err != nil {
		return "unreadable", 0, 0, 0, 0, 0, 0
	}
	dataEnd := pageSize - 24
	header := pageSize - 40
	if pageSize == 512 {
		dataEnd = 496
		header = 488
	}
	// NBT uses 0x81; BBT uses 0x80.
	if page[dataEnd] != expectedType || page[dataEnd] != page[dataEnd+1] {
		return "wrong page type", 0, 0, 0, 0, 0, 0
	}
	entries, maxEntries, entrySize, level := int(page[header]), int(page[header+1]), int(page[header+2]), int(page[header+3])
	if entries > maxEntries || maxEntries == 0 || entrySize == 0 {
		return "invalid B-tree header", entries, maxEntries, entrySize, level, 0, 0
	}
	refs, badRefs := 0, 0
	if level > 0 {
		for i := 0; i < entries; i++ {
			entryOffset := i * entrySize
			if entryOffset+24 > header {
				return "entry area exceeds page", entries, maxEntries, entrySize, level, refs, badRefs
			}
			child := binary.LittleEndian.Uint64(page[entryOffset+16 : entryOffset+24])
			refs++
			if child == 0 || (fileSize > 0 && child+uint64(pageSize) > uint64(fileSize)) || child%uint64(pageSize) != 0 {
				badRefs++
			}
		}
	}
	if binary.LittleEndian.Uint32(page[dataEnd+4:dataEnd+8]) != disk.ComputeCRC(page[:dataEnd]) {
		return "CRC invalid", entries, maxEntries, entrySize, level, refs, badRefs
	}
	return "valid", entries, maxEntries, entrySize, level, refs, badRefs
}

// WalkIndexTrees follows NBT and BBT branch references without reading message
// payloads. The limits are deliberate: a damaged tree must not cause an
// unbounded scan or recursion.
func WalkIndexTrees(name string, h Header, fileSize int64, maxPages, maxDepth int) (IndexTreeScan, IndexTreeScan) {
	nbt := walkIndexTree(name, h.NBTOffset, h.PageSize, fileSize, 0x81, maxPages, maxDepth)
	bbt := walkIndexTree(name, h.BBTOffset, h.PageSize, fileSize, 0x80, maxPages, maxDepth)
	resolveNodeBlocks(&nbt, bbt.blockBIDs)
	return nbt, bbt
}

// ScanIndexedPayloads reads only the unique block IDs referenced by NBT leaf
// entries. It never scans arbitrary file bytes and is bounded by maxBlocks.
func ScanIndexedPayloads(name string, h Header, fileSize int64, maxBlocks int) PayloadScan {
	scan := PayloadScan{Status: "not applicable"}
	if maxBlocks <= 0 || fileSize <= 0 {
		return scan
	}
	nbt, bbt := WalkIndexTrees(name, h, fileSize, 100000, 64)
	if bbt.blockLocations == nil {
		scan.Status = "BBT locations unavailable"
		return scan
	}
	ids := make(map[uint64]bool)
	for _, node := range nbt.nodeBIDs {
		if node.dataBID != 0 {
			ids[node.dataBID] = true
		}
		if node.subnodeBID != 0 {
			ids[node.subnodeBID] = true
		}
	}
	scan.BlocksReferenced = len(ids)
	queue := make([]uint64, 0, len(ids))
	for bid := range ids {
		queue = append(queue, bid)
	}
	f, err := os.Open(name)
	if err != nil {
		scan.Status = "unavailable: " + err.Error()
		return scan
	}
	defer f.Close()
	for queueIndex := 0; queueIndex < len(queue); queueIndex++ {
		bid := queue[queueIndex]
		if scan.BlocksRead >= maxBlocks {
			scan.LimitReached = true
			break
		}
		location, ok := bbt.blockLocations[bid]
		if !ok || location.size == 0 || location.offset+uint64(location.size) < location.offset || location.offset+uint64(location.size) > uint64(fileSize) {
			scan.ReadErrors++
			continue
		}
		alignedSize := (uint64(location.size) + 63) &^ 63
		totalSize := alignedSize + 16
		if location.offset+totalSize < location.offset || location.offset+totalSize > uint64(fileSize) {
			scan.ReadErrors++
			continue
		}
		data := make([]byte, totalSize)
		if _, err := f.ReadAt(data, int64(location.offset)); err != nil {
			scan.ReadErrors++
			continue
		}
		scan.BlocksRead++
		scan.TrailersChecked++
		trailer := disk.ParseBlockTrailerUnicode(data[alignedSize:])
		if trailer.Size != location.size {
			scan.TrailerSizeMismatch++
		}
		if trailer.CRC != disk.ComputeCRC(data[:alignedSize]) {
			scan.TrailerCRCInvalid++
		}
		if trailer.Signature != disk.ComputeSignature(bid, location.offset) {
			scan.TrailerSigInvalid++
		}
		if trailer.BID != bid {
			scan.TrailerBIDInvalid++
		}
		if trailer.Size != location.size || trailer.BID == 0 || disk.ValidateBlockTrailer(data, &trailer, bid, location.offset) != nil {
			scan.TrailersInvalid++
		} else {
			scan.TrailersValid++
		}
		if len(data) == 0 {
			scan.UnknownBlocks++
			continue
		}
		payload := data[:location.size]
		switch payload[0] {
		case 0x01:
			scan.ExtendedBlocks++
			count := 0
			if len(payload) >= 4 {
				count = int(binary.LittleEndian.Uint16(payload[2:4]))
			}
			if len(payload) >= 8 && 8+count*8 <= len(payload) {
				if block, err := disk.ParseExtendedBlock(payload, disk.FormatUnicode); err == nil {
					scan.ExtendedParsed++
					scan.ExtendedReferences += len(block.BIDs)
					scan.LogicalBytes += uint64(block.TotalSize)
					for _, childBID := range block.BIDs {
						if !ids[childBID] {
							ids[childBID] = true
							queue = append(queue, childBID)
						}
					}
				} else {
					scan.DecodeErrors++
				}
			} else {
				scan.DecodeErrors++
			}
		case 0x02:
			scan.SubnodeBlocks++
			if len(payload) < 8 {
				scan.DecodeErrors++
				continue
			}
			count := int(binary.LittleEndian.Uint16(payload[2:4]))
			entrySize := 24
			if payload[1] > 0 {
				entrySize = 16
			}
			if 8+count*entrySize <= len(payload) {
				if block, err := disk.ParseSubnodeBlock(payload, disk.FormatUnicode); err == nil {
					scan.SubnodeParsed++
					scan.SubnodeEntries += int(block.Count)
					if block.IsLeaf() {
						for _, entry := range block.LeafEntries {
							if entry.DataBID != 0 && !ids[entry.DataBID] {
								ids[entry.DataBID] = true
								queue = append(queue, entry.DataBID)
							}
							if entry.SubBID != 0 && !ids[entry.SubBID] {
								ids[entry.SubBID] = true
								queue = append(queue, entry.SubBID)
							}
						}
					} else {
						for _, entry := range block.NonleafEntries {
							if entry.SubBID != 0 && !ids[entry.SubBID] {
								ids[entry.SubBID] = true
								queue = append(queue, entry.SubBID)
							}
						}
					}
				} else {
					scan.DecodeErrors++
				}
			} else {
				scan.DecodeErrors++
			}
		default:
			scan.ExternalBlocks++
		}
	}
	scan.BlocksDiscovered = len(ids)
	scan.Status = "completed"
	return scan
}

func walkIndexTree(name string, root uint64, pageSize int, fileSize int64, expectedType byte, maxPages, maxDepth int) IndexTreeScan {
	scan := IndexTreeScan{Status: "not applicable"}
	if root == 0 || pageSize == 0 || maxPages <= 0 || maxDepth < 0 {
		return scan
	}
	f, err := os.Open(name)
	if err != nil {
		scan.Status = "unavailable: " + err.Error()
		return scan
	}
	defer f.Close()
	scan.Status = "completed"
	if expectedType == 0x80 {
		scan.blockBIDs = make(map[uint64]bool)
		scan.blockLocations = make(map[uint64]blockLocation)
	}
	visited := make(map[uint64]bool)
	var visit func(uint64, int)
	visit = func(offset uint64, depth int) {
		if scan.PagesVisited >= maxPages {
			scan.Status = "page limit reached"
			return
		}
		if depth > maxDepth {
			scan.Status = "depth limit reached"
			return
		}
		if visited[offset] {
			scan.Cycles++
			return
		}
		visited[offset] = true
		if depth > scan.MaxDepth {
			scan.MaxDepth = depth
		}
		page, ok := readIndexPage(f, offset, pageSize, fileSize, expectedType)
		if !ok {
			scan.InvalidPages++
			return
		}
		scan.PagesVisited++
		if !page.CRCValid {
			scan.CRCInvalid++
		}
		if page.Level == 0 {
			scan.Leaves++
			if expectedType == 0x80 {
				scanBlockRefs(&scan, page.Blocks, fileSize)
			} else if expectedType == 0x81 {
				scan.NodeRefs += len(page.Nodes)
				scan.nodeBIDs = append(scan.nodeBIDs, page.Nodes...)
				for _, node := range page.Nodes {
					if node.dataBID != 0 {
						scan.DataBIDs++
					}
					if node.subnodeBID != 0 {
						scan.SubnodeBIDs++
					}
				}
			}
			return
		}
		scan.Branches++
		for _, child := range page.Children {
			if child == 0 || child%uint64(pageSize) != 0 || (fileSize > 0 && child+uint64(pageSize) > uint64(fileSize)) {
				scan.BadRefs++
				continue
			}
			visit(child, depth+1)
		}
	}
	visit(root, 0)
	return scan
}

type indexPage struct {
	Level    int
	CRCValid bool
	Children []uint64
	Blocks   []blockRef
	Nodes    []nodeRef
}

// blockRef is the BBT leaf's physical block reference. The BBT stores the
// block ID, file offset (IB), unaligned byte count (CB), and reference count.
// Keeping this small record separate from message parsing lets inspection stay
// read-only and useful even when higher-level node data is damaged.
type blockRef struct {
	bid    uint64
	offset uint64
	size   uint16
}

type nodeRef struct {
	dataBID    uint64
	subnodeBID uint64
}

func resolveNodeBlocks(nbt *IndexTreeScan, blockBIDs map[uint64]bool) {
	for _, node := range nbt.nodeBIDs {
		if node.dataBID != 0 {
			if blockBIDs[node.dataBID] {
				nbt.ResolvedData++
			} else {
				nbt.MissingData++
			}
		}
		if node.subnodeBID != 0 {
			if blockBIDs[node.subnodeBID] {
				nbt.ResolvedSub++
			} else {
				nbt.MissingSub++
			}
		}
	}
}

func scanBlockRefs(scan *IndexTreeScan, refs []blockRef, fileSize int64) {
	for _, ref := range refs {
		scan.BlockRefs++
		scan.blockBIDs[ref.bid] = true
		scan.blockLocations[ref.bid] = blockLocation{offset: ref.offset, size: ref.size}
		end := ref.offset + uint64(ref.size)
		if end < ref.offset || ref.offset >= uint64(fileSize) || end > uint64(fileSize) || ref.size == 0 {
			scan.BadBlocks++
			continue
		}
		scan.BlockBytes += uint64(ref.size)
	}
}

func readIndexPage(f *os.File, offset uint64, pageSize int, fileSize int64, expectedType byte) (indexPage, bool) {
	if fileSize > 0 && offset+uint64(pageSize) > uint64(fileSize) {
		return indexPage{}, false
	}
	page := make([]byte, pageSize)
	if _, err := f.ReadAt(page, int64(offset)); err != nil {
		return indexPage{}, false
	}
	dataEnd, headerOffset := pageSize-24, pageSize-40
	if pageSize == 512 {
		dataEnd, headerOffset = 496, 488
	}
	if page[dataEnd] != expectedType || page[dataEnd] != page[dataEnd+1] {
		return indexPage{}, false
	}
	entries, maxEntries := int(page[headerOffset]), int(page[headerOffset+1])
	entrySize, level := int(page[headerOffset+2]), int(page[headerOffset+3])
	if entries > maxEntries || maxEntries == 0 || entrySize == 0 || entries*entrySize > headerOffset {
		return indexPage{}, false
	}
	result := indexPage{Level: level, CRCValid: binary.LittleEndian.Uint32(page[dataEnd+4:dataEnd+8]) == disk.ComputeCRC(page[:dataEnd])}
	if level > 0 {
		if entrySize < 24 {
			return indexPage{}, false
		}
		result.Children = make([]uint64, 0, entries)
		for i := 0; i < entries; i++ {
			pos := i * entrySize
			result.Children = append(result.Children, binary.LittleEndian.Uint64(page[pos+16:pos+24]))
		}
	} else if expectedType == 0x80 {
		// Unicode BBT leaf entries are 24 bytes: BID (8), IB (8), CB (2),
		// cRef (2), followed by reserved bytes. CB is the unaligned data
		// length; do not impose a guessed alignment rule here.
		if entrySize < 20 {
			return indexPage{}, false
		}
		result.Blocks = make([]blockRef, 0, entries)
		for i := 0; i < entries; i++ {
			pos := i * entrySize
			result.Blocks = append(result.Blocks, blockRef{
				bid:    binary.LittleEndian.Uint64(page[pos : pos+8]),
				offset: binary.LittleEndian.Uint64(page[pos+8 : pos+16]),
				size:   binary.LittleEndian.Uint16(page[pos+16 : pos+18]),
			})
		}
	} else if expectedType == 0x81 {
		// Unicode NBT leaf entries are 32 bytes: NID, bidData, bidSub,
		// nidParent, and four bytes of padding.
		if entrySize < 28 {
			return indexPage{}, false
		}
		result.Nodes = make([]nodeRef, 0, entries)
		for i := 0; i < entries; i++ {
			pos := i * entrySize
			result.Nodes = append(result.Nodes, nodeRef{
				dataBID:    binary.LittleEndian.Uint64(page[pos+8 : pos+16]),
				subnodeBID: binary.LittleEndian.Uint64(page[pos+16 : pos+24]),
			})
		}
	}
	return result, true
}
