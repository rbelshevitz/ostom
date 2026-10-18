package pst

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
)

// readBTreeBlockData reads one block and inflates it when a Unicode4k BBT
// entry says that the stored payload is compressed. The returned bytes remain
// encrypted; HeapOnNodeReader applies the file's block encryption exactly once.
func (file *File) readBTreeBlockData(block BTreeNode) ([]byte, error) {
	if block.Size == 0 {
		return nil, fmt.Errorf("block 0x%X has zero stored size", block.Identifier)
	}
	stored := make([]byte, int(block.Size))
	if _, err := file.Reader.ReadAt(stored, block.FileOffset); err != nil {
		return nil, fmt.Errorf("read block 0x%X: %w", block.Identifier, err)
	}
	if file.FormatType != FormatTypeUnicode4k || block.InflatedSize == 0 || block.InflatedSize == block.Size {
		return stored, nil
	}

	maxBlockSize, err := file.GetBlockSize()
	if err != nil {
		return nil, err
	}
	if int(block.InflatedSize) > maxBlockSize {
		return nil, fmt.Errorf("block 0x%X inflated size %d exceeds maximum %d", block.Identifier, block.InflatedSize, maxBlockSize)
	}

	reader, err := zlib.NewReader(bytes.NewReader(stored))
	if err != nil {
		return nil, fmt.Errorf("open compressed block 0x%X: %w", block.Identifier, err)
	}
	defer reader.Close()

	// Read one byte beyond the declared size so a corrupt stream cannot expand
	// without bound or silently exceed the BBT's declared length.
	inflated, err := io.ReadAll(io.LimitReader(reader, int64(block.InflatedSize)+1))
	if err != nil {
		return nil, fmt.Errorf("inflate block 0x%X: %w", block.Identifier, err)
	}
	if len(inflated) != int(block.InflatedSize) {
		return nil, fmt.Errorf("block 0x%X inflated to %d bytes; BBT declares %d", block.Identifier, len(inflated), block.InflatedSize)
	}
	return inflated, nil
}

func (file *File) decodeBlockForTraversal(block BTreeNode, data []byte) ([]byte, error) {
	decoded := make([]byte, len(data))
	reader := NewHeapOnNodeReaderWithIdentifiers(
		file.EncryptionType,
		[]Identifier{block.Identifier},
		*io.NewSectionReader(bytes.NewReader(data), 0, int64(len(data))),
	)
	if _, err := reader.ReadAt(decoded, 0); err != nil {
		return nil, fmt.Errorf("decode internal block 0x%X: %w", block.Identifier, err)
	}
	return decoded, nil
}
