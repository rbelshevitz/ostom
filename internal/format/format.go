// Package format contains conservative, read-only PFF/OFF header detection.
// It deliberately reports an unsupported or unknown variant instead of
// classifying it as corruption.
package format

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

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

// IndexedBlockLocation exposes only the physical location needed by fixture
// tooling and diagnostics. It does not expose mutable parser state.
type IndexedBlockLocation struct {
	BID    uint64
	Offset uint64
	Size   uint16
}

// IndexedBlockLocations returns BBT leaf locations for a bounded indexed
// walk. Callers must still validate each location against the file they open.
func IndexedBlockLocations(name string, h Header, fileSize int64, maxPages, maxDepth int) (map[uint64]IndexedBlockLocation, error) {
	if fileSize <= 0 || maxPages <= 0 || maxDepth < 0 {
		return nil, fmt.Errorf("invalid indexed block location limits")
	}
	_, bbt := WalkIndexTrees(name, h, fileSize, maxPages, maxDepth)
	if bbt.blockLocations == nil {
		return nil, fmt.Errorf("BBT locations unavailable")
	}
	locations := make(map[uint64]IndexedBlockLocation, len(bbt.blockLocations))
	for bid, location := range bbt.blockLocations {
		locations[bid] = IndexedBlockLocation{BID: bid, Offset: location.offset, Size: location.size}
	}
	return locations, nil
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

// StreamIndexedPayload writes one indexed data tree in bounded chunks. It is
// deliberately additive: the existing AssembleIndexedPayload path remains
// unchanged and can be used as a fallback while this reader is validated.
// The writer receives logical payload bytes only; block headers, padding, and
// trailers are never exposed.
func StreamIndexedPayload(name string, h Header, fileSize int64, bid uint64, maxBlocks int, maxBytes uint64, write func([]byte) error) (AssemblyResult, error) {
	if h.PageSize != 512 {
		return AssemblyResult{}, fmt.Errorf("payload streaming for page size %d is not implemented", h.PageSize)
	}
	if maxBlocks <= 0 || maxBytes == 0 {
		return AssemblyResult{}, fmt.Errorf("payload streaming limits must be positive")
	}
	_, bbt := WalkIndexTrees(name, h, fileSize, 100000, 64)
	if bbt.blockLocations == nil {
		return AssemblyResult{}, fmt.Errorf("BBT locations unavailable")
	}
	f, err := os.Open(name)
	if err != nil {
		return AssemblyResult{}, err
	}
	defer f.Close()
	source := indexedPayloadSource{file: f, locations: bbt.blockLocations, fileSize: fileSize, method: h.CryptMethod}
	return streamPayloadFromSource(source, bid, maxBlocks, maxBytes, write)
}

// CollectIndexedPayloadBlocks returns the logical data-tree leaves as
// separate chunks. HN parsing needs these page boundaries; concatenating them
// would lose the HNPAGEHDR/HNBITMAPHDR boundaries.
func CollectIndexedPayloadBlocks(name string, h Header, fileSize int64, bid uint64, maxBlocks int, maxBytes uint64) ([][]byte, AssemblyResult, error) {
	blocks := make([][]byte, 0)
	result, err := StreamIndexedPayload(name, h, fileSize, bid, maxBlocks, maxBytes, func(chunk []byte) error {
		blocks = append(blocks, append([]byte(nil), chunk...))
		return nil
	})
	return blocks, result, err
}

// IndexedPropertyContextResult is the bounded native PC result for one NBT
// data BID. It is intentionally raw and does not claim that the node is a
// message until higher-level NID/type and property rules confirm that.
type IndexedPropertyContextResult struct {
	Assembly           AssemblyResult   `json:"assembly"`
	Properties         []PropertyRecord `json:"properties,omitempty"`
	ResolvedProperties []PropertyValue  `json:"resolved_properties,omitempty"`
	ResolutionErrors   []string         `json:"resolution_errors,omitempty"`
}

// IndexedRecoveredMessageScan is the bounded native candidate collection
// result. Messages are ready for a later exporter, while errors retain only a
// capped sample for the final report.
type IndexedRecoveredMessageScan struct {
	NodesAttempted int                `json:"nodes_attempted"`
	Messages       []RecoveredMessage `json:"messages,omitempty"`
	Rejected       int                `json:"rejected"`
	Duplicates     int                `json:"duplicates"`
	Errors         int                `json:"errors"`
	ErrorSamples   []string           `json:"error_samples,omitempty"`
	LimitReached   bool               `json:"limit_reached"`
	Status         string             `json:"status"`
}

// PropertyContextScanError keeps a bounded sample of node-level failures so a
// recovery report can explain what was skipped without retaining an unbounded
// error list from a damaged mailbox.
type PropertyContextScanError struct {
	NID   uint64 `json:"nid"`
	BID   uint64 `json:"bid"`
	Error string `json:"error"`
}

// PropertyContextScan is the first native higher-level recovery summary. It
// deliberately reports attempts and failures instead of calling every NBT
// data block a message: NID/type resolution and richer LTP structures are
// still separate stages.
type PropertyContextScan struct {
	NodesReferenced int                        `json:"nodes_referenced"`
	NodesAttempted  int                        `json:"nodes_attempted"`
	NodesParsed     int                        `json:"nodes_parsed"`
	Properties      int                        `json:"properties"`
	Errors          int                        `json:"errors"`
	LimitReached    bool                       `json:"limit_reached"`
	Status          string                     `json:"status"`
	ErrorSamples    []PropertyContextScanError `json:"error_samples,omitempty"`
}

// ScanIndexedPropertyContexts attempts a bounded PC parse for each unique NBT
// data BID. It is intentionally read-only and conservative: ordinary HN
// blocks that are not PC roots become reportable errors, not false messages.
func ScanIndexedPropertyContexts(name string, h Header, fileSize int64, maxNodes, maxBlocks int, maxBytes uint64, maxRecords, maxErrorSamples int) PropertyContextScan {
	scan := PropertyContextScan{Status: "not applicable"}
	if maxNodes <= 0 || maxBlocks <= 0 || maxBytes == 0 || maxRecords <= 0 || maxErrorSamples < 0 {
		return scan
	}
	nbt, bbt := WalkIndexTrees(name, h, fileSize, 100000, 64)
	if bbt.blockLocations == nil {
		scan.Status = "BBT locations unavailable"
		return scan
	}
	seen := make(map[uint64]bool)
	for _, node := range nbt.nodeBIDs {
		if node.dataBID == 0 || seen[node.dataBID] {
			continue
		}
		seen[node.dataBID] = true
		scan.NodesReferenced++
		if scan.NodesAttempted >= maxNodes {
			scan.LimitReached = true
			break
		}
		scan.NodesAttempted++
		result, err := ParseIndexedPropertyContext(name, h, fileSize, node.dataBID, maxBlocks, maxBytes, maxRecords)
		if err != nil {
			scan.Errors++
			if len(scan.ErrorSamples) < maxErrorSamples {
				scan.ErrorSamples = append(scan.ErrorSamples, PropertyContextScanError{NID: node.nid, BID: node.dataBID, Error: err.Error()})
			}
			continue
		}
		scan.NodesParsed++
		scan.Properties += len(result.Properties)
	}
	scan.Status = "completed"
	return scan
}

// ParseIndexedPropertyContext collects one indexed node's HN blocks and
// decodes a leaf-root Property Context. Unsupported or damaged structures are
// returned as errors for the caller's bounded recovery log.
func ParseIndexedPropertyContext(name string, h Header, fileSize int64, bid uint64, maxBlocks int, maxBytes uint64, maxRecords int) (IndexedPropertyContextResult, error) {
	return ParseIndexedPropertyContextWithSubnodes(name, h, fileSize, bid, maxBlocks, maxBytes, maxRecords, nil)
}

// ParseIndexedPropertyContextWithSubnodes resolves PC values that point into
// the node's subnode tree, including attachment payload properties.
func ParseIndexedPropertyContextWithSubnodes(name string, h Header, fileSize int64, bid uint64, maxBlocks int, maxBytes uint64, maxRecords int, subnodes map[uint64][]byte) (IndexedPropertyContextResult, error) {
	blocks, assembly, err := CollectIndexedPayloadBlocks(name, h, fileSize, bid, maxBlocks, maxBytes)
	result := IndexedPropertyContextResult{Assembly: assembly}
	if err != nil {
		return result, err
	}
	heap, err := NewHeapReader(blocks)
	if err != nil {
		return result, err
	}
	result.Properties, err = ParsePropertyContext(heap, maxRecords)
	if err != nil {
		return result, err
	}
	for _, property := range result.Properties {
		value, resolveErr := ResolvePropertyValue(property, heap, subnodes)
		if resolveErr != nil {
			result.ResolutionErrors = append(result.ResolutionErrors, resolveErr.Error())
			continue
		}
		result.ResolvedProperties = append(result.ResolvedProperties, value)
	}
	return result, nil
}

// CollectIndexedSubnodePayloads resolves leaf subnode NIDs to bounded logical
// payloads. It keeps partial results so one damaged attachment does not erase
// other readable subnode payloads.
func CollectIndexedSubnodePayloads(name string, h Header, fileSize int64, rootBID uint64, maxBlocks int, maxBytes uint64) (map[uint64][]byte, SubnodeTreeResult, error) {
	entries, tree, err := WalkIndexedSubnodeTree(name, h, fileSize, rootBID, maxBlocks)
	if err != nil {
		return map[uint64][]byte{}, tree, err
	}
	_, bbt := WalkIndexTrees(name, h, fileSize, 100000, 64)
	if bbt.blockLocations == nil {
		return map[uint64][]byte{}, tree, fmt.Errorf("BBT locations unavailable")
	}
	f, err := os.Open(name)
	if err != nil {
		return map[uint64][]byte{}, tree, err
	}
	defer f.Close()
	source := indexedPayloadSource{file: f, locations: bbt.blockLocations, fileSize: fileSize, method: h.CryptMethod}
	return collectSubnodePayloadsFromSource(source, entries, maxBlocks, maxBytes), tree, nil
}

func collectSubnodePayloadsFromSource(source payloadSource, entries map[uint64]SubnodeTreeEntry, maxBlocks int, maxBytes uint64) map[uint64][]byte {
	payloads := make(map[uint64][]byte)
	if maxBlocks <= 0 || maxBytes == 0 {
		return payloads
	}
	for nid, entry := range entries {
		if entry.DataBID == 0 {
			continue
		}
		var data []byte
		_, readErr := streamPayloadFromSource(source, entry.DataBID, maxBlocks, maxBytes, func(chunk []byte) error {
			data = append(data, chunk...)
			return nil
		})
		if readErr != nil {
			continue
		}
		payloads[nid] = data
	}
	return payloads
}

// CollectIndexedRecoveredMessages connects NBT/BBT, PC resolution, and the
// bounded message collector. It intentionally continues after a bad node.
func CollectIndexedRecoveredMessages(name string, h Header, fileSize int64, maxNodes, maxBlocks int, maxBytes uint64, maxRecords, maxMessages, maxBodyBytes, maxErrorSamples int) (IndexedRecoveredMessageScan, error) {
	return CollectIndexedRecoveredMessagesWithProgress(name, h, fileSize, maxNodes, maxBlocks, maxBytes, maxRecords, maxMessages, maxBodyBytes, maxErrorSamples, nil)
}

// CollectIndexedRecoveredMessagesWithProgress is the progress-reporting form
// used by the CLI. total is the bounded number of unique NBT data candidates.
func CollectIndexedRecoveredMessagesWithProgress(name string, h Header, fileSize int64, maxNodes, maxBlocks int, maxBytes uint64, maxRecords, maxMessages, maxBodyBytes, maxErrorSamples int, progress func(done, total int)) (IndexedRecoveredMessageScan, error) {
	scan := IndexedRecoveredMessageScan{Status: "not applicable"}
	if maxNodes <= 0 || maxBlocks <= 0 || maxBytes == 0 || maxRecords <= 0 || maxMessages <= 0 || maxBodyBytes <= 0 || maxErrorSamples < 0 {
		return scan, fmt.Errorf("native recovery limits must be positive")
	}
	collector, err := NewRecoveredMessageCollector(maxMessages, maxBodyBytes)
	if err != nil {
		return scan, err
	}
	nbt, bbt := WalkIndexTrees(name, h, fileSize, 100000, 64)
	if bbt.blockLocations == nil {
		scan.Status = "BBT locations unavailable"
		return scan, nil
	}
	seen := make(map[uint64]bool)
	total := 0
	for _, node := range nbt.nodeBIDs {
		if node.dataBID != 0 && !seen[node.dataBID] {
			seen[node.dataBID] = true
			total++
			if total == maxNodes {
				break
			}
		}
	}
	seen = make(map[uint64]bool)
	if progress != nil {
		progress(0, total)
	}
	for _, node := range nbt.nodeBIDs {
		if node.dataBID == 0 || seen[node.dataBID] {
			continue
		}
		seen[node.dataBID] = true
		if scan.NodesAttempted >= maxNodes {
			scan.LimitReached = true
			break
		}
		scan.NodesAttempted++
		if progress != nil {
			progress(scan.NodesAttempted, total)
		}
		var subnodes map[uint64][]byte
		if node.subnodeBID != 0 {
			var subnodeErr error
			subnodes, _, subnodeErr = CollectIndexedSubnodePayloads(name, h, fileSize, node.subnodeBID, maxBlocks, maxBytes)
			if subnodeErr != nil {
				scan.addError(fmt.Errorf("NID 0x%X subnodes: %w", node.nid, subnodeErr), maxErrorSamples)
			}
		}
		result, parseErr := ParseIndexedPropertyContextWithSubnodes(name, h, fileSize, node.dataBID, maxBlocks, maxBytes, maxRecords, subnodes)
		if parseErr != nil {
			scan.addError(parseErr, maxErrorSamples)
			continue
		}
		for _, resolutionErr := range result.ResolutionErrors {
			scan.addError(fmt.Errorf("NID 0x%X BID 0x%X: %s", node.nid, node.dataBID, resolutionErr), maxErrorSamples)
		}
		_, addErr := collector.AddResolvedProperties(node.nid, node.dataBID, result.ResolvedProperties)
		if addErr != nil {
			scan.addError(addErr, maxErrorSamples)
		}
	}
	scan.Messages = collector.Messages()
	_, rejected, duplicates, collectorLimit := collector.Stats()
	scan.Rejected = rejected
	scan.Duplicates = duplicates
	if collectorLimit {
		scan.LimitReached = true
	}
	scan.Status = "completed"
	return scan, nil
}

func (s *IndexedRecoveredMessageScan) addError(err error, maxSamples int) {
	if err == nil {
		return
	}
	s.Errors++
	if len(s.ErrorSamples) < maxSamples {
		s.ErrorSamples = append(s.ErrorSamples, err.Error())
	}
}

type payloadSource interface {
	Payload(uint64) ([]byte, error)
}

type indexedPayloadSource struct {
	file      *os.File
	locations map[uint64]blockLocation
	fileSize  int64
	method    disk.CryptMethod
}

func (s indexedPayloadSource) Payload(bid uint64) ([]byte, error) {
	location, ok := s.locations[bid]
	if !ok {
		return nil, fmt.Errorf("BID 0x%X not found in BBT", bid)
	}
	aligned := (uint64(location.size) + 63) &^ 63
	total := aligned + 16
	if location.offset+total < location.offset || location.offset+total > uint64(s.fileSize) {
		return nil, fmt.Errorf("BID 0x%X block exceeds file", bid)
	}
	data := make([]byte, total)
	if _, err := s.file.ReadAt(data, int64(location.offset)); err != nil {
		return nil, err
	}
	return decodeBlockPayload(data[:location.size], bid, s.method), nil
}

func streamPayloadFromSource(source payloadSource, bid uint64, maxBlocks int, maxBytes uint64, write func([]byte) error) (AssemblyResult, error) {
	result := AssemblyResult{}
	if write == nil {
		return result, fmt.Errorf("payload writer is nil")
	}
	if maxBlocks <= 0 || maxBytes == 0 {
		return result, fmt.Errorf("payload streaming limits must be positive")
	}

	active := make(map[uint64]bool)
	var emit func([]byte) error
	emit = func(payload []byte) error {
		if result.BytesProduced >= maxBytes {
			result.Truncated = true
			return nil
		}
		remaining := maxBytes - result.BytesProduced
		if uint64(len(payload)) > remaining {
			payload = payload[:remaining]
			result.Truncated = true
		}
		if len(payload) > 0 {
			if err := write(payload); err != nil {
				return err
			}
			result.BytesProduced += uint64(len(payload))
		}
		return nil
	}

	var visit func(uint64, int, byte) error
	visit = func(current uint64, depth int, expectedLevel byte) error {
		if depth > 64 {
			return fmt.Errorf("payload tree depth exceeds 64 at BID 0x%X", current)
		}
		if active[current] {
			return fmt.Errorf("payload cycle at BID 0x%X", current)
		}
		if result.BlocksRead >= maxBlocks {
			result.Truncated = true
			return nil
		}
		result.BlocksRead++
		payload, err := source.Payload(current)
		if err != nil {
			return err
		}
		if len(payload) == 0 {
			return nil
		}

		if expectedLevel == 0 {
			if current&0x2 != 0 {
				return fmt.Errorf("BID 0x%X is internal where an external data block was expected", current)
			}
			return emit(payload)
		}
		if expectedLevel == 0xFF && current&0x2 == 0 {
			return emit(payload)
		}
		if expectedLevel == 1 && current&0x2 == 0 {
			return fmt.Errorf("BID 0x%X is external where an XBLOCK was expected", current)
		}
		if payload[0] != 0x01 {
			return fmt.Errorf("BID 0x%X is not an extended block", current)
		}
		level := expectedLevel
		if level == 0xFF {
			level = payload[1]
			if level != 1 && level != 2 {
				return fmt.Errorf("BID 0x%X has unsupported extended block level %d", current, level)
			}
		}
		extended, err := parseExtendedBlockBounded(payload, disk.FormatUnicode, level)
		if err != nil {
			return fmt.Errorf("BID 0x%X: %w", current, err)
		}
		active[current] = true
		defer delete(active, current)
		for _, child := range extended.BIDs {
			childLevel := byte(0)
			if extended.Level == 2 {
				childLevel = 1
			}
			if err := visit(child, depth+1, childLevel); err != nil {
				return err
			}
			if result.BytesProduced >= maxBytes {
				result.Truncated = true
				break
			}
		}
		return nil
	}

	if err := visit(bid, 0, 0xFF); err != nil {
		return result, err
	}
	return result, nil
}

func parseExtendedBlockBounded(data []byte, format disk.PSTFormat, expectedLevel byte) (*disk.ExtendedBlock, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("extended block header is truncated")
	}
	if data[0] != 0x01 {
		return nil, fmt.Errorf("invalid extended block type 0x%02X", data[0])
	}
	if expectedLevel != 0 && data[1] != expectedLevel {
		return nil, fmt.Errorf("unexpected extended block level %d, want %d", data[1], expectedLevel)
	}
	count := int(binary.LittleEndian.Uint16(data[2:4]))
	bidSize := 8
	if format == disk.FormatANSI {
		bidSize = 4
	}
	need := 8 + count*bidSize
	if count < 0 || need < 8 || need > len(data) {
		return nil, fmt.Errorf("extended block BID array is truncated")
	}
	block, err := disk.ParseExtendedBlock(data[:need], format)
	if err != nil {
		return nil, err
	}
	return block, nil
}

// SubnodeTreeEntry is the resolved leaf mapping for one subnode NID.
type SubnodeTreeEntry struct {
	NID     uint64
	DataBID uint64
	SubBID  uint64
}

// SubnodeTreeResult records a bounded subnode-BTree walk.
type SubnodeTreeResult struct {
	BlocksRead   int  `json:"blocks_read"`
	Leaves       int  `json:"leaves"`
	Branches     int  `json:"branches"`
	Entries      int  `json:"entries"`
	Cycles       int  `json:"cycles"`
	Invalid      int  `json:"invalid_blocks"`
	LimitReached bool `json:"limit_reached"`
}

func parseSubnodeBlockBounded(payload []byte) (*disk.SubnodeBlock, error) {
	if len(payload) < 8 || payload[0] != 0x02 {
		return nil, fmt.Errorf("invalid or truncated subnode block header")
	}
	level := payload[1]
	if level > 1 {
		return nil, fmt.Errorf("unsupported subnode level %d", level)
	}
	count := int(binary.LittleEndian.Uint16(payload[2:4]))
	entrySize := 24
	if level > 0 {
		entrySize = 16
	}
	need := 8 + count*entrySize
	if need < 8 || need > len(payload) {
		return nil, fmt.Errorf("subnode entries are truncated")
	}
	return disk.ParseSubnodeBlock(payload[:need], disk.FormatUnicode)
}

// WalkIndexedSubnodeTree resolves an NBT subnode BID into leaf NID mappings.
// It is intentionally independent from the existing payload scan: callers can
// opt into this deeper structure only after the conservative scan succeeds.
func WalkIndexedSubnodeTree(name string, h Header, fileSize int64, rootBID uint64, maxBlocks int) (map[uint64]SubnodeTreeEntry, SubnodeTreeResult, error) {
	entries := make(map[uint64]SubnodeTreeEntry)
	result := SubnodeTreeResult{}
	if h.PageSize != 512 {
		return entries, result, fmt.Errorf("subnode tree walk for page size %d is not implemented", h.PageSize)
	}
	if rootBID == 0 || maxBlocks <= 0 {
		return entries, result, fmt.Errorf("subnode tree root and limit must be positive")
	}
	if rootBID&0x2 == 0 {
		return entries, result, fmt.Errorf("subnode root BID 0x%X is not internal", rootBID)
	}
	_, bbt := WalkIndexTrees(name, h, fileSize, 100000, 64)
	if bbt.blockLocations == nil {
		return entries, result, fmt.Errorf("BBT locations unavailable")
	}
	f, err := os.Open(name)
	if err != nil {
		return entries, result, err
	}
	defer f.Close()
	source := indexedPayloadSource{file: f, locations: bbt.blockLocations, fileSize: fileSize, method: h.CryptMethod}
	return walkSubnodeTreeFromSource(source, rootBID, maxBlocks)
}

func walkSubnodeTreeFromSource(source payloadSource, rootBID uint64, maxBlocks int) (map[uint64]SubnodeTreeEntry, SubnodeTreeResult, error) {
	entries := make(map[uint64]SubnodeTreeEntry)
	result := SubnodeTreeResult{}
	if rootBID == 0 || maxBlocks <= 0 {
		return entries, result, fmt.Errorf("subnode tree root and limit must be positive")
	}
	if rootBID&0x2 == 0 {
		return entries, result, fmt.Errorf("subnode root BID 0x%X is not internal", rootBID)
	}

	active := make(map[uint64]bool)
	var visit func(uint64, int) error
	visit = func(bid uint64, depth int) error {
		if depth > 64 {
			return fmt.Errorf("subnode tree depth exceeds 64 at BID 0x%X", bid)
		}
		if active[bid] {
			result.Cycles++
			return fmt.Errorf("subnode tree cycle at BID 0x%X", bid)
		}
		if result.BlocksRead >= maxBlocks {
			result.LimitReached = true
			return nil
		}
		result.BlocksRead++
		payload, err := source.Payload(bid)
		if err != nil {
			result.Invalid++
			return err
		}
		block, err := parseSubnodeBlockBounded(payload)
		if err != nil {
			result.Invalid++
			return fmt.Errorf("BID 0x%X: %w", bid, err)
		}
		active[bid] = true
		defer delete(active, bid)
		if block.IsLeaf() {
			result.Leaves++
			for _, entry := range block.LeafEntries {
				entries[entry.NID] = SubnodeTreeEntry{NID: entry.NID, DataBID: entry.DataBID, SubBID: entry.SubBID}
				result.Entries++
			}
			return nil
		}
		result.Branches++
		for _, entry := range block.NonleafEntries {
			if entry.SubBID == 0 || entry.SubBID&0x2 == 0 {
				result.Invalid++
				return fmt.Errorf("BID 0x%X has invalid child subnode BID 0x%X", bid, entry.SubBID)
			}
			if err := visit(entry.SubBID, depth+1); err != nil {
				return err
			}
		}
		return nil
	}

	if err := visit(rootBID, 0); err != nil {
		return entries, result, err
	}
	return entries, result, nil
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

// HeapBlockScan is the bounded structural view of one HN data block.
type HeapBlockScan struct {
	Index         int      `json:"index"`
	PageMapOffset uint16   `json:"page_map_offset"`
	Signature     byte     `json:"signature"`
	Client        byte     `json:"client_signature"`
	RootHID       uint32   `json:"root_hid"`
	Allocations   []uint16 `json:"allocations"`
	FreeCount     uint16   `json:"free_count"`
}

// HeapReader provides bounded allocation reads over an already separated HN.
// It keeps the physical block reader outside the LTP layer.
type HeapReader struct {
	blocks [][]byte
	scans  []HeapBlockScan
}

// NewHeapReader parses all supplied HN blocks and returns a reader for their
// allocations. The input slices are copied so callers can safely reuse their
// read buffers after construction.
func NewHeapReader(blocks [][]byte) (*HeapReader, error) {
	if len(blocks) == 0 {
		return nil, fmt.Errorf("HN requires at least one block")
	}
	reader := &HeapReader{
		blocks: make([][]byte, len(blocks)),
		scans:  make([]HeapBlockScan, len(blocks)),
	}
	for index, block := range blocks {
		if len(block) == 0 {
			return nil, fmt.Errorf("HN block %d is empty", index)
		}
		scan, err := ParseHeapBlock(index, block)
		if err != nil {
			return nil, fmt.Errorf("parse HN block %d: %w", index, err)
		}
		reader.blocks[index] = append([]byte(nil), block...)
		reader.scans[index] = scan
	}
	return reader, nil
}

// RootHID returns the user-root allocation from the first HN block.
func (h *HeapReader) RootHID() uint32 {
	if h == nil || len(h.scans) == 0 {
		return 0
	}
	return h.scans[0].RootHID
}

// ClientSignature returns the higher-level HN client signature.
func (h *HeapReader) ClientSignature() byte {
	if h == nil || len(h.scans) == 0 {
		return 0
	}
	return h.scans[0].Client
}

// Read returns a copy of the allocation addressed by HID.
func (h *HeapReader) Read(hid uint32) ([]byte, error) {
	if h == nil {
		return nil, fmt.Errorf("HN reader is nil")
	}
	return ReadHeapAllocation(h.blocks, h.scans, hid)
}

// ParseHeapBlock parses one HN page without assuming that the surrounding
// node is healthy. The first page has HNHDR; later pages have HNPAGEHDR.
func ParseHeapBlock(index int, data []byte) (HeapBlockScan, error) {
	result := HeapBlockScan{Index: index}
	if index < 0 {
		return result, fmt.Errorf("heap block index must be non-negative")
	}
	if len(data) < 4 {
		return result, fmt.Errorf("heap block is truncated")
	}
	if index == 0 {
		if len(data) < 12 {
			return result, fmt.Errorf("HNHDR is truncated")
		}
		result.PageMapOffset = binary.LittleEndian.Uint16(data[0:2])
		result.Signature = data[2]
		result.Client = data[3]
		result.RootHID = binary.LittleEndian.Uint32(data[4:8])
		if result.Signature != 0xEC {
			return result, fmt.Errorf("invalid HN signature 0x%02X", result.Signature)
		}
	} else {
		result.PageMapOffset = binary.LittleEndian.Uint16(data[0:2])
	}
	mapOffset := int(result.PageMapOffset)
	if mapOffset < 0 || mapOffset+4 > len(data) {
		return result, fmt.Errorf("HNPAGEMAP offset %d is outside block", result.PageMapOffset)
	}
	result.FreeCount = binary.LittleEndian.Uint16(data[mapOffset+2 : mapOffset+4])
	allocCount := int(binary.LittleEndian.Uint16(data[mapOffset : mapOffset+2]))
	offsetEnd := mapOffset + 4 + (allocCount+1)*2
	if allocCount < 0 || offsetEnd < mapOffset+4 || offsetEnd > len(data) {
		return result, fmt.Errorf("HNPAGEMAP allocation table is truncated")
	}
	result.Allocations = make([]uint16, allocCount+1)
	for i := range result.Allocations {
		result.Allocations[i] = binary.LittleEndian.Uint16(data[mapOffset+4+i*2 : mapOffset+6+i*2])
		if int(result.Allocations[i]) > mapOffset || (i > 0 && result.Allocations[i] < result.Allocations[i-1]) {
			return result, fmt.Errorf("invalid HN allocation offset at index %d", i)
		}
	}
	return result, nil
}

// ReadHeapAllocation returns one allocation addressed by a HID. The caller
// supplies already separated HN block payloads and their parsed page maps;
// this keeps allocation resolution independent from the physical NDB reader.
func ReadHeapAllocation(blocks [][]byte, scans []HeapBlockScan, hid uint32) ([]byte, error) {
	if len(blocks) != len(scans) {
		return nil, fmt.Errorf("HN block data and scan counts differ")
	}
	if hid == 0 {
		return nil, nil
	}
	if hid&0x1F != 0 {
		return nil, fmt.Errorf("HID 0x%X has non-zero reserved bits", hid)
	}
	blockIndex := int(hid >> 16)
	hidAllocIndex := int((hid >> 5) & 0x7FF)
	if hidAllocIndex == 0 {
		return nil, fmt.Errorf("HID 0x%X has invalid zero allocation index", hid)
	}
	allocIndex := hidAllocIndex - 1
	if blockIndex < 0 || blockIndex >= len(blocks) {
		return nil, fmt.Errorf("HID 0x%X block index %d is out of bounds", hid, blockIndex)
	}
	scan := scans[blockIndex]
	if allocIndex < 0 || allocIndex+1 >= len(scan.Allocations) {
		return nil, fmt.Errorf("HID 0x%X allocation index %d is out of bounds", hid, allocIndex)
	}
	start := int(scan.Allocations[allocIndex])
	end := int(scan.Allocations[allocIndex+1])
	data := blocks[blockIndex]
	if start > end || end > len(data) {
		return nil, fmt.Errorf("HID 0x%X allocation bounds %d:%d exceed block size %d", hid, start, end, len(data))
	}
	return append([]byte(nil), data[start:end]...), nil
}

// BTHHeaderScan is the bounded structural view of a B-tree-on-Heap header.
type BTHHeaderScan struct {
	KeySize    byte   `json:"key_size"`
	EntrySize  byte   `json:"entry_size"`
	IndexLevel byte   `json:"index_level"`
	RootHID    uint32 `json:"root_hid"`
}

// BTHRecord is one tightly packed leaf record. Its interpretation belongs to
// the higher-level structure using the BTH (for example, PC or TC).
type BTHRecord struct {
	Key   []byte
	Value []byte
}

// PropertyRecord is the raw PC BTH record. ValueHNID is interpreted as an
// inline value, an HID, or a subnode NID according to PropType and size.
type PropertyRecord struct {
	PropID    uint16 `json:"prop_id"`
	PropType  uint16 `json:"prop_type"`
	ValueHNID uint32 `json:"value_hnid"`
}

// PropertyValue is a raw PC value with its storage provenance preserved.
type PropertyValue struct {
	PropID   uint16 `json:"prop_id"`
	PropType uint16 `json:"prop_type"`
	Source   string `json:"source"`
	Data     []byte `json:"-"`
}

// TableColumn describes one TCOLDESC entry in a Table Context.
type TableColumn struct {
	PropType  uint16 `json:"prop_type"`
	PropID    uint16 `json:"prop_id"`
	Offset    uint16 `json:"offset"`
	Size      byte   `json:"size"`
	BitOffset byte   `json:"bit_offset"`
}

// TableContextHeader is the bounded structural part of TCINFO. Row data and
// the embedded row-index BTH are intentionally parsed in a later step.
type TableContextHeader struct {
	Signature     byte          `json:"signature"`
	ColumnCount   byte          `json:"column_count"`
	SizeOffsets   [4]uint16     `json:"size_offsets"`
	RowBTHID      uint32        `json:"row_bth_hid"`
	RowMatrixHNID uint32        `json:"row_matrix_hnid"`
	Columns       []TableColumn `json:"columns,omitempty"`
}

// TableRowData is one raw row resolved through the TC row-index BTH. The
// bytes include the cell-existence bitmap and are intentionally not decoded
// into MAPI values yet.
type TableRowData struct {
	RowID    uint32 `json:"row_id"`
	RowIndex uint32 `json:"row_index"`
	Data     []byte `json:"-"`
}

// TableCell is one decoded raw TC cell. Data is present only when the CEB
// says the property exists; no MAPI type conversion is implied here.
type TableCell struct {
	PropID   uint16 `json:"prop_id"`
	PropType uint16 `json:"prop_type"`
	Present  bool   `json:"present"`
	Data     []byte `json:"-"`
}

// TableRowSignals records explainable hints used by the recovery planner.
// These are candidates, not proof of a message, folder, or attachment.
type TableRowSignals struct {
	MessageClass     bool   `json:"message_class"`
	Subject          bool   `json:"subject"`
	DisplayName      bool   `json:"display_name"`
	AttachmentNumber bool   `json:"attachment_number"`
	AttachmentData   bool   `json:"attachment_data"`
	Candidate        string `json:"candidate"`
}

// RecoveredMessage is the bounded native-recovery record that can be handed
// to an exporter. It retains NID/BID provenance for the final recovery log.
type RecoveredMessage struct {
	NID          uint64                `json:"nid"`
	BID          uint64                `json:"bid"`
	MessageClass string                `json:"message_class,omitempty"`
	Subject      string                `json:"subject,omitempty"`
	Sender       string                `json:"sender,omitempty"`
	DateUnix     int64                 `json:"date_unix,omitempty"`
	Body         []byte                `json:"-"`
	Attachments  []RecoveredAttachment `json:"attachments,omitempty"`
	Properties   int                   `json:"properties"`
	Incomplete   bool                  `json:"incomplete"`
	Errors       []string              `json:"errors,omitempty"`
}

// RecoveredAttachment is bounded attachment metadata plus an optional payload.
// Missing/subnode payloads remain visible through Incomplete and Errors.
type RecoveredAttachment struct {
	NID          uint64   `json:"nid"`
	Name         string   `json:"name,omitempty"`
	MimeType     string   `json:"mime_type,omitempty"`
	ContentID    string   `json:"content_id,omitempty"`
	DeclaredSize int64    `json:"declared_size,omitempty"`
	Data         []byte   `json:"-"`
	Inline       bool     `json:"inline"`
	Incomplete   bool     `json:"incomplete"`
	Errors       []string `json:"errors,omitempty"`
}

// RecoveredMessageCollector turns resolved native properties into bounded
// message records. It is independent of mbox/MIME writing.
type RecoveredMessageCollector struct {
	maxMessages int
	maxBody     int
	messages    []RecoveredMessage
	seen        map[uint64]bool
	duplicates  int
	rejected    int
	limited     bool
}

func NewRecoveredMessageCollector(maxMessages, maxBodyBytes int) (*RecoveredMessageCollector, error) {
	if maxMessages <= 0 || maxBodyBytes <= 0 {
		return nil, fmt.Errorf("recovered message limits must be positive")
	}
	return &RecoveredMessageCollector{
		maxMessages: maxMessages,
		maxBody:     maxBodyBytes,
		messages:    make([]RecoveredMessage, 0, maxMessages),
		seen:        make(map[uint64]bool),
	}, nil
}

// AddResolvedProperties adds one native node. The bool is false when there is
// not enough message evidence to present it as a recovered message.
func (c *RecoveredMessageCollector) AddResolvedProperties(nid, bid uint64, values []PropertyValue) (bool, error) {
	if c == nil {
		return false, fmt.Errorf("recovered message collector is nil")
	}
	if bid == 0 {
		return false, fmt.Errorf("recovered message BID is empty")
	}
	if c.seen[bid] {
		c.duplicates++
		return false, nil
	}
	c.seen[bid] = true
	if len(c.messages) >= c.maxMessages {
		c.limited = true
		return false, nil
	}
	message := RecoveredMessage{NID: nid, BID: bid, Properties: len(values)}
	senderEmail := ""
	for _, value := range values {
		switch value.PropID {
		case 0x001A: // PidTagMessageClass
			message.MessageClass = decodePropertyText(value)
		case 0x0037: // PidTagSubject
			message.Subject = decodePropertyText(value)
		case 0x0C1A: // PidTagSenderName
			message.Sender = decodePropertyText(value)
		case 0x0C1F: // PidTagSenderEmailAddress
			senderEmail = decodePropertyText(value)
		case 0x0039: // PidTagClientSubmitTime
			if date, ok := decodeFiletimeUnix(value.Data); ok {
				message.DateUnix = date
			}
		case 0x1000: // PidTagBody
			if len(value.Data) > c.maxBody {
				body := value.Data[:c.maxBody]
				if value.PropType&0x0FFF == 0x001F {
					body = []byte(decodePropertyText(PropertyValue{PropType: value.PropType, Data: body}))
				}
				message.Body = append([]byte(nil), body...)
				message.Incomplete = true
				message.Errors = append(message.Errors, "body truncated by collector limit")
			} else {
				body := value.Data
				if value.PropType&0x0FFF == 0x001F {
					body = []byte(decodePropertyText(value))
				}
				message.Body = append([]byte(nil), body...)
			}
		}
	}
	if message.Sender != "" && senderEmail != "" && message.Sender != senderEmail {
		message.Sender += " <" + senderEmail + ">"
	} else if message.Sender == "" {
		message.Sender = senderEmail
	}
	if message.MessageClass == "" && message.Subject == "" && len(message.Body) == 0 {
		c.rejected++
		return false, nil
	}
	c.messages = append(c.messages, message)
	return true, nil
}

// AddAttachmentProperties attaches one resolved attachment property set to a
// collected message. A missing payload is retained as incomplete metadata.
func (c *RecoveredMessageCollector) AddAttachmentProperties(messageBID, attachmentNID uint64, values []PropertyValue) error {
	if c == nil {
		return fmt.Errorf("recovered message collector is nil")
	}
	for index := range c.messages {
		if c.messages[index].BID != messageBID {
			continue
		}
		attachment := RecoveredAttachment{NID: attachmentNID}
		for _, value := range values {
			switch value.PropID {
			case 0x3707: // PidTagAttachLongFilename
				attachment.Name = decodePropertyText(value)
			case 0x3704: // PidTagAttachFilename
				if attachment.Name == "" {
					attachment.Name = decodePropertyText(value)
				}
			case 0x370E: // PidTagAttachMimeTag
				attachment.MimeType = decodePropertyText(value)
			case 0x3712: // PidTagAttachContentId
				attachment.ContentID = decodePropertyText(value)
				attachment.Inline = attachment.ContentID != ""
			case 0x0E20: // PidTagAttachSize
				if size, ok := decodeUint64(value.Data); ok {
					attachment.DeclaredSize = int64(size)
				}
			case 0x3701: // PidTagAttachDataBinary
				if len(value.Data) > c.maxBody {
					attachment.Data = append([]byte(nil), value.Data[:c.maxBody]...)
					attachment.Incomplete = true
					attachment.Errors = append(attachment.Errors, "attachment payload truncated by collector limit")
				} else {
					attachment.Data = append([]byte(nil), value.Data...)
				}
			}
		}
		if attachment.Name == "" {
			attachment.Name = fmt.Sprintf("attachment-%X", attachmentNID)
		}
		if attachment.DeclaredSize > int64(len(attachment.Data)) {
			attachment.Incomplete = true
			attachment.Errors = append(attachment.Errors, "declared attachment size exceeds readable bytes")
		}
		if attachment.DeclaredSize == 0 && len(attachment.Data) == 0 {
			attachment.Incomplete = true
			attachment.Errors = append(attachment.Errors, "attachment payload unavailable")
		}
		c.messages[index].Attachments = append(c.messages[index].Attachments, attachment)
		return nil
	}
	return fmt.Errorf("message BID 0x%X not found for attachment NID 0x%X", messageBID, attachmentNID)
}

func (c *RecoveredMessageCollector) Messages() []RecoveredMessage {
	if c == nil {
		return nil
	}
	result := make([]RecoveredMessage, len(c.messages))
	copy(result, c.messages)
	for i := range result {
		result[i].Body = append([]byte(nil), result[i].Body...)
		result[i].Errors = append([]string(nil), result[i].Errors...)
		for j := range result[i].Attachments {
			result[i].Attachments[j].Data = append([]byte(nil), result[i].Attachments[j].Data...)
			result[i].Attachments[j].Errors = append([]string(nil), result[i].Attachments[j].Errors...)
		}
	}
	return result
}

// Stats returns bounded accounting for the final recovery log.
func (c *RecoveredMessageCollector) Stats() (added, rejected, duplicates int, limitReached bool) {
	if c == nil {
		return 0, 0, 0, false
	}
	return len(c.messages), c.rejected, c.duplicates, c.limited
}

func decodePropertyText(value PropertyValue) string {
	data := value.Data
	if value.PropType&0x0FFF == 0x001F {
		for len(data) >= 2 && data[len(data)-1] == 0 && data[len(data)-2] == 0 {
			data = data[:len(data)-2]
		}
		if len(data)%2 != 0 {
			return strings.TrimSpace(string(bytesTrimNUL(data)))
		}
		runes := make([]rune, 0, len(data)/2)
		for index := 0; index < len(data); index += 2 {
			runes = append(runes, rune(binary.LittleEndian.Uint16(data[index:index+2])))
		}
		return strings.TrimSpace(string(runes))
	}
	data = bytesTrimNUL(data)
	return strings.TrimSpace(string(data))
}

func bytesTrimNUL(data []byte) []byte {
	end := len(data)
	for end > 0 && data[end-1] == 0 {
		end--
	}
	return data[:end]
}

func decodeFiletimeUnix(data []byte) (int64, bool) {
	if len(data) < 8 {
		return 0, false
	}
	filetime := binary.LittleEndian.Uint64(data[:8])
	const unixEpochFiletime = uint64(116444736000000000)
	if filetime < unixEpochFiletime {
		return 0, false
	}
	seconds := (filetime - unixEpochFiletime) / 10000000
	if seconds > uint64(^uint64(0)>>1) {
		return 0, false
	}
	unix := int64(seconds)
	if time.Unix(unix, 0).Year() < 1970 {
		return 0, false
	}
	return unix, true
}

func decodeUint64(data []byte) (uint64, bool) {
	switch len(data) {
	case 1:
		return uint64(data[0]), true
	case 2:
		return uint64(binary.LittleEndian.Uint16(data)), true
	case 4:
		return uint64(binary.LittleEndian.Uint32(data)), true
	case 8:
		return binary.LittleEndian.Uint64(data), true
	default:
		return 0, false
	}
}

// ParseBTHHeader parses a BTHHEADER allocation.
func ParseBTHHeader(data []byte) (BTHHeaderScan, error) {
	result := BTHHeaderScan{}
	if len(data) < 8 {
		return result, fmt.Errorf("BTHHEADER is truncated")
	}
	if data[0] != 0xB5 {
		return result, fmt.Errorf("invalid BTH signature 0x%02X", data[0])
	}
	switch data[1] {
	case 2, 4, 8, 16:
	default:
		return result, fmt.Errorf("unsupported BTH key size %d", data[1])
	}
	if data[2] == 0 || data[2] > 32 {
		return result, fmt.Errorf("invalid BTH entry size %d", data[2])
	}
	result.KeySize = data[1]
	result.EntrySize = data[2]
	result.IndexLevel = data[3]
	result.RootHID = binary.LittleEndian.Uint32(data[4:8])
	return result, nil
}

// ParseBTHLeafRecords parses tightly packed leaf records using a validated
// BTH header. It does not interpret keys or values.
func ParseBTHLeafRecords(data []byte, header BTHHeaderScan, maxRecords int) ([]BTHRecord, error) {
	if maxRecords <= 0 {
		return nil, fmt.Errorf("BTH record limit must be positive")
	}
	recordSize := int(header.KeySize) + int(header.EntrySize)
	if recordSize <= 0 {
		return nil, fmt.Errorf("invalid BTH record size")
	}
	if len(data)%recordSize != 0 {
		return nil, fmt.Errorf("BTH leaf allocation is not a whole number of records")
	}
	count := len(data) / recordSize
	if count > maxRecords {
		return nil, fmt.Errorf("BTH record limit reached: %d > %d", count, maxRecords)
	}
	records := make([]BTHRecord, 0, count)
	for offset := 0; offset < len(data); offset += recordSize {
		keyEnd := offset + int(header.KeySize)
		valueEnd := keyEnd + int(header.EntrySize)
		records = append(records, BTHRecord{
			Key:   append([]byte(nil), data[offset:keyEnd]...),
			Value: append([]byte(nil), data[keyEnd:valueEnd]...),
		})
	}
	return records, nil
}

// ParsePropertyRecords decodes the fixed-width records used by a Property
// Context. It intentionally leaves value resolution to the HN/subnode layer.
func ParsePropertyRecords(records []BTHRecord) ([]PropertyRecord, error) {
	properties := make([]PropertyRecord, 0, len(records))
	for index, record := range records {
		if len(record.Key) != 2 || len(record.Value) != 6 {
			return nil, fmt.Errorf("property record %d has key/value sizes %d/%d", index, len(record.Key), len(record.Value))
		}
		properties = append(properties, PropertyRecord{
			PropID:    binary.LittleEndian.Uint16(record.Key),
			PropType:  binary.LittleEndian.Uint16(record.Value[0:2]),
			ValueHNID: binary.LittleEndian.Uint32(record.Value[2:6]),
		})
	}
	return properties, nil
}

// ParseTableContextHeader parses TCINFO and its fixed-width TCOLDESC array.
// The caller still has to resolve RowBTHID/RowMatrixHNID through HN and
// subnodes; this function does not claim that a table has readable rows.
func ParseTableContextHeader(data []byte, maxColumns int) (TableContextHeader, error) {
	result := TableContextHeader{}
	if maxColumns <= 0 {
		return result, fmt.Errorf("table column limit must be positive")
	}
	if len(data) < 22 {
		return result, fmt.Errorf("TCINFO is truncated: %d bytes", len(data))
	}
	if data[0] != 0x7C {
		return result, fmt.Errorf("invalid TC signature 0x%02X", data[0])
	}
	result.Signature = data[0]
	result.ColumnCount = data[1]
	if int(result.ColumnCount) > maxColumns {
		return result, fmt.Errorf("TC column limit reached: %d > %d", result.ColumnCount, maxColumns)
	}
	for i := range result.SizeOffsets {
		pos := 2 + i*2
		result.SizeOffsets[i] = binary.LittleEndian.Uint16(data[pos : pos+2])
	}
	result.RowBTHID = binary.LittleEndian.Uint32(data[10:14])
	result.RowMatrixHNID = binary.LittleEndian.Uint32(data[14:18])
	columnsEnd := 22 + int(result.ColumnCount)*8
	if columnsEnd < 22 || columnsEnd > len(data) {
		return TableContextHeader{}, fmt.Errorf("TCINFO column descriptors are truncated")
	}
	result.Columns = make([]TableColumn, 0, result.ColumnCount)
	for pos := 22; pos < columnsEnd; pos += 8 {
		result.Columns = append(result.Columns, TableColumn{
			PropType:  binary.LittleEndian.Uint16(data[pos : pos+2]),
			PropID:    binary.LittleEndian.Uint16(data[pos+2 : pos+4]),
			Offset:    binary.LittleEndian.Uint16(data[pos+4 : pos+6]),
			Size:      data[pos+6],
			BitOffset: data[pos+7],
		})
	}
	if result.SizeOffsets[0] > result.SizeOffsets[1] || result.SizeOffsets[1] > result.SizeOffsets[2] || result.SizeOffsets[2] > result.SizeOffsets[3] {
		return TableContextHeader{}, fmt.Errorf("TCINFO size offsets are not monotonic")
	}
	return result, nil
}

// ParseTableContext reads the TCINFO allocation from a Table Context HN.
// It returns only the validated header; row decoding remains bounded and
// separate because the row matrix may live in a subnode.
func ParseTableContext(heap *HeapReader, maxColumns int) (TableContextHeader, error) {
	result := TableContextHeader{}
	if heap == nil {
		return result, fmt.Errorf("table context HN is nil")
	}
	if heap.ClientSignature() != 0x7C {
		return result, fmt.Errorf("invalid Table Context client signature 0x%02X", heap.ClientSignature())
	}
	root := heap.RootHID()
	if root == 0 {
		return result, fmt.Errorf("table context root HID is empty")
	}
	data, err := heap.Read(root)
	if err != nil {
		return result, fmt.Errorf("read TCINFO HID 0x%X: %w", root, err)
	}
	return ParseTableContextHeader(data, maxColumns)
}

// ParseTableContextRows resolves a heap-backed row matrix and its leaf row
// index BTH. Subnode-backed matrices and intermediate BTH levels are reported
// explicitly for a later bounded subnode/branch traversal step.
func ParseTableContextRows(heap *HeapReader, header TableContextHeader, maxRows int) ([]TableRowData, error) {
	if heap == nil {
		return nil, fmt.Errorf("table context HN is nil")
	}
	if heap.ClientSignature() != 0x7C {
		return nil, fmt.Errorf("invalid Table Context client signature 0x%02X", heap.ClientSignature())
	}
	if maxRows <= 0 {
		return nil, fmt.Errorf("table row limit must be positive")
	}
	rowSize := int(header.SizeOffsets[3])
	if rowSize <= 0 || header.SizeOffsets[2] > header.SizeOffsets[3] {
		return nil, fmt.Errorf("invalid TC row size %d", rowSize)
	}
	rowIndexData, err := heap.Read(header.RowBTHID)
	if err != nil {
		return nil, fmt.Errorf("read TC row BTH HID 0x%X: %w", header.RowBTHID, err)
	}
	rowBTH, err := ParseBTHHeader(rowIndexData)
	if err != nil {
		return nil, fmt.Errorf("parse TC row BTH: %w", err)
	}
	if rowBTH.KeySize != 4 || rowBTH.EntrySize != 4 {
		return nil, fmt.Errorf("invalid TC row BTH shape key=%d entry=%d", rowBTH.KeySize, rowBTH.EntrySize)
	}
	if rowBTH.IndexLevel != 0 {
		return nil, fmt.Errorf("TC row BTH intermediate levels are not implemented: %d", rowBTH.IndexLevel)
	}
	if rowBTH.RootHID == 0 {
		return []TableRowData{}, nil
	}
	rowRecordsData, err := heap.Read(rowBTH.RootHID)
	if err != nil {
		return nil, fmt.Errorf("read TC row BTH root HID 0x%X: %w", rowBTH.RootHID, err)
	}
	rowRecords, err := ParseBTHLeafRecords(rowRecordsData, rowBTH, maxRows)
	if err != nil {
		return nil, fmt.Errorf("parse TC row records: %w", err)
	}
	if header.RowMatrixHNID == 0 {
		return []TableRowData{}, nil
	}
	if header.RowMatrixHNID&0x1F != 0 {
		return nil, fmt.Errorf("TC row matrix subnode HNID 0x%X is not supported", header.RowMatrixHNID)
	}
	matrix, err := heap.Read(header.RowMatrixHNID)
	if err != nil {
		return nil, fmt.Errorf("read TC row matrix HID 0x%X: %w", header.RowMatrixHNID, err)
	}
	rows := make([]TableRowData, 0, len(rowRecords))
	for index, record := range rowRecords {
		if len(record.Key) != 4 || len(record.Value) != 4 {
			return nil, fmt.Errorf("TC row record %d has key/value sizes %d/%d", index, len(record.Key), len(record.Value))
		}
		rowID := binary.LittleEndian.Uint32(record.Key)
		rowIndex := binary.LittleEndian.Uint32(record.Value)
		start := uint64(rowIndex) * uint64(rowSize)
		end := start + uint64(rowSize)
		if end < start || end > uint64(len(matrix)) {
			return nil, fmt.Errorf("TC row %d index %d exceeds matrix size %d", rowID, rowIndex, len(matrix))
		}
		rows = append(rows, TableRowData{RowID: rowID, RowIndex: rowIndex, Data: append([]byte(nil), matrix[start:end]...)})
	}
	return rows, nil
}

// DecodeTableRow resolves column offsets and the cell-existence bitmap for a
// raw row. Every column must fit the row and its bitmap byte must be present;
// damaged rows are rejected instead of returning partially shifted values.
func DecodeTableRow(header TableContextHeader, row TableRowData) ([]TableCell, error) {
	if len(row.Data) < int(header.SizeOffsets[3]) {
		return nil, fmt.Errorf("TC row %d is shorter than declared row size", row.RowID)
	}
	bitmapOffset := int(header.SizeOffsets[2])
	if bitmapOffset > len(row.Data) {
		return nil, fmt.Errorf("TC row %d bitmap offset is out of bounds", row.RowID)
	}
	cells := make([]TableCell, 0, len(header.Columns))
	for index, column := range header.Columns {
		bitmapByte := int(column.BitOffset) / 8
		bitmapIndex := bitmapOffset + bitmapByte
		if bitmapIndex >= len(row.Data) {
			return nil, fmt.Errorf("TC row %d column %d bitmap is out of bounds", row.RowID, index)
		}
		cell := TableCell{PropID: column.PropID, PropType: column.PropType}
		cell.Present = row.Data[bitmapIndex]&(1<<uint(column.BitOffset%8)) != 0
		if cell.Present {
			start := int(column.Offset)
			end := start + int(column.Size)
			if start < 0 || end < start || end > len(row.Data) {
				return nil, fmt.Errorf("TC row %d column %d data is out of bounds", row.RowID, index)
			}
			cell.Data = append([]byte(nil), row.Data[start:end]...)
		}
		cells = append(cells, cell)
	}
	return cells, nil
}

// IdentifyTableRow emits only property-presence signals. A row can carry
// several signals at once; the candidate label is deliberately broad because
// full node/NID context is required for a definitive object type.
func IdentifyTableRow(cells []TableCell) TableRowSignals {
	var signals TableRowSignals
	for _, cell := range cells {
		if !cell.Present {
			continue
		}
		switch cell.PropID {
		case 0x001A: // PidTagMessageClass
			signals.MessageClass = true
		case 0x0037: // PidTagSubject
			signals.Subject = true
		case 0x3001: // PidTagDisplayName
			signals.DisplayName = true
		case 0x0E21: // PidTagAttachNumber
			signals.AttachmentNumber = true
		case 0x3701: // PidTagAttachDataBinary
			signals.AttachmentData = true
		}
	}
	switch {
	case signals.AttachmentNumber || signals.AttachmentData:
		signals.Candidate = "attachment-candidate"
	case signals.MessageClass || signals.Subject:
		signals.Candidate = "message-candidate"
	case signals.DisplayName:
		signals.Candidate = "folder-candidate"
	default:
		signals.Candidate = "unknown"
	}
	return signals
}

const multiValuePropertyFlag uint16 = 0x1000

func propertyFixedSize(propType uint16) int {
	if propType&multiValuePropertyFlag != 0 {
		return 0
	}
	switch propType & 0x0FFF {
	case 0x0002: // PtypInteger16
		return 2
	case 0x0003, 0x0004, 0x000A: // PtypInteger32, PtypFloat, PtypError
		return 4
	case 0x0005, 0x0006, 0x0007, 0x0014, 0x0040: // 8-byte scalar types
		return 8
	case 0x000B: // PtypBoolean
		return 2
	case 0x0048: // PtypGuid
		return 16
	default:
		return 0
	}
}

// ResolvePropertyValue resolves the storage location of one PC value. It
// deliberately returns raw bytes; decoding strings, times, and MAPI-specific
// structures belongs to a higher layer.
func ResolvePropertyValue(record PropertyRecord, heap *HeapReader, subnodes map[uint64][]byte) (PropertyValue, error) {
	value := PropertyValue{PropID: record.PropID, PropType: record.PropType}
	fixedSize := propertyFixedSize(record.PropType)
	if fixedSize > 0 && fixedSize <= 4 {
		value.Source = "inline"
		inline := make([]byte, 4)
		binary.LittleEndian.PutUint32(inline, record.ValueHNID)
		value.Data = append([]byte(nil), inline[:fixedSize]...)
		return value, nil
	}
	if record.ValueHNID == 0 {
		value.Source = "empty"
		return value, nil
	}
	if record.ValueHNID&0x1F == 0 {
		if heap == nil {
			return value, fmt.Errorf("property 0x%04X references HID 0x%X but HN is unavailable", record.PropID, record.ValueHNID)
		}
		data, err := heap.Read(record.ValueHNID)
		if err != nil {
			return value, fmt.Errorf("property 0x%04X HID 0x%X: %w", record.PropID, record.ValueHNID, err)
		}
		value.Source = "heap"
		value.Data = data
		return value, nil
	}
	if subnodes == nil {
		return value, fmt.Errorf("property 0x%04X references subnode NID 0x%X but subnodes are unavailable", record.PropID, record.ValueHNID)
	}
	data, ok := subnodes[uint64(record.ValueHNID)]
	if !ok {
		return value, fmt.Errorf("property 0x%04X subnode NID 0x%X not found", record.PropID, record.ValueHNID)
	}
	value.Source = "subnode"
	value.Data = append([]byte(nil), data...)
	return value, nil
}

// ParsePropertyContext reads a PC HN whose BTH root is a leaf allocation.
// Intermediate BTH levels are reported explicitly and remain a separate
// bounded traversal step.
func ParsePropertyContext(heap *HeapReader, maxRecords int) ([]PropertyRecord, error) {
	if heap == nil {
		return nil, fmt.Errorf("property context HN is nil")
	}
	if heap.ClientSignature() != 0xBC {
		return nil, fmt.Errorf("invalid Property Context client signature 0x%02X", heap.ClientSignature())
	}
	if maxRecords <= 0 {
		return nil, fmt.Errorf("property record limit must be positive")
	}
	rootHID := heap.RootHID()
	if rootHID == 0 {
		return []PropertyRecord{}, nil
	}
	headerData, err := heap.Read(rootHID)
	if err != nil {
		return nil, fmt.Errorf("read PC BTH header HID 0x%X: %w", rootHID, err)
	}
	header, err := ParseBTHHeader(headerData)
	if err != nil {
		return nil, err
	}
	if header.KeySize != 2 || header.EntrySize != 6 {
		return nil, fmt.Errorf("invalid PC BTH shape key=%d entry=%d", header.KeySize, header.EntrySize)
	}
	if header.IndexLevel != 0 {
		return nil, fmt.Errorf("PC BTH intermediate levels are not implemented: %d", header.IndexLevel)
	}
	if header.RootHID == 0 {
		return []PropertyRecord{}, nil
	}
	leafData, err := heap.Read(header.RootHID)
	if err != nil {
		return nil, fmt.Errorf("read PC BTH root HID 0x%X: %w", header.RootHID, err)
	}
	records, err := ParseBTHLeafRecords(leafData, header, maxRecords)
	if err != nil {
		return nil, err
	}
	return ParsePropertyRecords(records)
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
	nid        uint64
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
				nid:        binary.LittleEndian.Uint64(page[pos : pos+8]),
				dataBID:    binary.LittleEndian.Uint64(page[pos+8 : pos+16]),
				subnodeBID: binary.LittleEndian.Uint64(page[pos+16 : pos+24]),
			})
		}
	}
	return result, true
}
