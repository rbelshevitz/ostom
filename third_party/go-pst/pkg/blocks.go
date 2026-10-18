// go-pst is a library for reading Personal Storage Table (.pst) files (written in Go/Golang).
//
// Copyright 2023 Marten Mooij
// Copyright 2025 Yury Tikhoglaz
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pst

import (
	"encoding/binary"
	"fmt"

	"github.com/rotisserie/eris"
)

// getBlocksFromNode resolves an XBLOCK/XXBLOCK whose contents may themselves
// be compressed in a Unicode4k OST.
func (file *File) getBlocksFromNode(node BTreeNode) ([]BTreeNode, uint32, error) {
	data, err := file.readBTreeBlockData(node)
	if err != nil {
		return nil, 0, err
	}
	data, err = file.decodeBlockForTraversal(node, data)
	if err != nil {
		return nil, 0, err
	}
	return file.getBlocksFromData(data, map[Identifier]bool{node.Identifier: true}, 0)
}

func (file *File) getBlocksFromData(data []byte, path map[Identifier]bool, depth int) ([]BTreeNode, uint32, error) {
	if depth > 16 {
		return nil, 0, fmt.Errorf("XBLOCK nesting exceeds 16 levels")
	}
	if len(data) < 8 {
		return nil, 0, fmt.Errorf("XBLOCK header is truncated: %d bytes", len(data))
	}
	if data[0] != 1 {
		return nil, 0, ErrBlockSignatureInvalid
	}
	level := BlockType(data[1])
	if level != BlockTypeXBlock && level != BlockTypeXXBlock {
		return nil, 0, ErrBlockTypeInvalid
	}
	count := int(binary.LittleEndian.Uint16(data[2:4]))
	totalSize := binary.LittleEndian.Uint32(data[4:8])
	identifierSize := int(GetIdentifierSize(file.FormatType))
	if count > (len(data)-8)/identifierSize {
		return nil, 0, fmt.Errorf("XBLOCK declares %d identifiers, exceeding %d-byte payload", count, len(data)-8)
	}

	var blocks []BTreeNode
	for i := 0; i < count; i++ {
		start := 8 + i*identifierSize
		identifier := GetIdentifierFromBytes(data[start:start+identifierSize], file.FormatType)
		block, err := file.GetBlockBTreeNode(identifier)
		if err != nil {
			return nil, 0, fmt.Errorf("resolve XBLOCK child 0x%X: %w", identifier, err)
		}
		if block.Identifier&0x02 == 0 {
			if level != BlockTypeXBlock {
				return nil, 0, fmt.Errorf("XXBLOCK child 0x%X is not an internal block", identifier)
			}
			blocks = append(blocks, block)
			continue
		}
		if level != BlockTypeXXBlock {
			return nil, 0, fmt.Errorf("XBLOCK child 0x%X is unexpectedly internal", identifier)
		}
		if path[block.Identifier] {
			return nil, 0, fmt.Errorf("XBLOCK cycle at block 0x%X", block.Identifier)
		}
		path[block.Identifier] = true
		childData, err := file.readBTreeBlockData(block)
		if err != nil {
			delete(path, block.Identifier)
			return nil, 0, err
		}
		childData, err = file.decodeBlockForTraversal(block, childData)
		if err != nil {
			delete(path, block.Identifier)
			return nil, 0, err
		}
		childBlocks, _, err := file.getBlocksFromData(childData, path, depth+1)
		delete(path, block.Identifier)
		if err != nil {
			return nil, 0, err
		}
		blocks = append(blocks, childBlocks...)
	}
	return blocks, totalSize, nil
}

// GetBlockSize returns the size of a block.
// References "Blocks".
func (file *File) GetBlockSize() (int, error) {
	switch file.FormatType {
	case FormatTypeUnicode:
		return 8192, nil
	case FormatTypeUnicode4k:
		return 65536, nil
	case FormatTypeANSI:
		return 8192, nil
	default:
		return 0, ErrFormatTypeUnsupported
	}
}

// GetBlockTrailerSize returns the size of a block trailer.
// References "Blocks".
func (file *File) GetBlockTrailerSize() (int, error) {
	switch file.FormatType {
	case FormatTypeUnicode:
		return 16, nil
	case FormatTypeUnicode4k:
		return 16, nil
	case FormatTypeANSI:
		return 12, nil
	default:
		return 0, ErrFormatTypeUnsupported
	}
}

// BlockType represents a XBlock or XXBlock.
type BlockType uint8

// Constants defining the block types.
const (
	BlockTypeXBlock  BlockType = 1
	BlockTypeXXBlock BlockType = 2
)

// GetBlocks returns all blocks (XBlock/XXBlock) from a Heap-on-Node along with the total blocks size.
// Internal identifiers have blocks.
//
// References:
// - https://github.com/useti/go-pst/tree/master/docs#xblock
// - https://github.com/useti/go-pst/tree/master/docs#xxblock
func (file *File) GetBlocks(btreeNodeHeapOnNodeOffset int64) ([]BTreeNode, error) {
	data := make([]byte, 4)

	if _, err := file.Reader.ReadAt(data, btreeNodeHeapOnNodeOffset); err != nil {
		return nil, eris.Wrap(err, "failed to read block data")
	}

	blockSignature := data[0]                                // Must indicate 1.
	blockLevel := data[1]                                    // 1 indicates XBlock, 2 indicates XXBlock.
	entryCount := int(binary.LittleEndian.Uint16(data[2:4])) // The number of block b-tree identifiers in this XBlock or XXBlock.

	if blockSignature != 1 {
		return nil, ErrBlockSignatureInvalid
	}

	identifierSize := int(GetIdentifierSize(file.FormatType))

	var blocks []BTreeNode

	switch BlockType(blockLevel) {
	case BlockTypeXBlock:
		// XBlock
		blockIdentifiers := make([]byte, entryCount*identifierSize)

		if _, err := file.Reader.ReadAt(blockIdentifiers, btreeNodeHeapOnNodeOffset+8); err != nil {
			return nil, eris.Wrap(err, "failed to read block identifiers")
		}

		for i := 0; i < entryCount; i++ {
			blockIdentifier := GetIdentifierFromBytes(blockIdentifiers[i*identifierSize:(i*identifierSize)+identifierSize], file.FormatType)
			blockBTreeNode, err := file.GetBlockBTreeNode(blockIdentifier) // TODO - Async then wait for the block b-tree node lookups.

			if err != nil {
				return nil, eris.Wrap(err, "failed to find block b-tree node")
			}

			blocks = append(blocks, blockBTreeNode)
		}
	case BlockTypeXXBlock:
		// XXBlock
		blockIdentifiers := make([]byte, entryCount*int(GetIdentifierSize(file.FormatType)))

		if _, err := file.Reader.ReadAt(blockIdentifiers, btreeNodeHeapOnNodeOffset+8); err != nil {
			return nil, eris.Wrap(err, "failed to read block identifiers")
		}

		for i := 0; i < entryCount; i++ {
			blockIdentifier := GetIdentifierFromBytes(blockIdentifiers[i*identifierSize:(i*identifierSize)+identifierSize], file.FormatType)
			blockBTreeNode, err := file.GetBlockBTreeNode(blockIdentifier) // TODO - Async then wait for the block b-tree node lookups.

			if err != nil {
				return nil, eris.Wrap(err, "failed to find block b-tree node")
			}

			// Recursive.
			blockBTreeNodeBlocks, err := file.GetBlocks(blockBTreeNode.FileOffset)

			if err != nil {
				return nil, eris.Wrap(err, "failed to get blocks")
			}

			blocks = append(blocks, blockBTreeNodeBlocks...)
		}
	default:
		return nil, ErrBlockTypeInvalid
	}

	return blocks, nil
}

// GetBlocksTotalSize returns the size of the external data referenced by the XBlock or XXBlock.
func (file *File) GetBlocksTotalSize(nodeEntryHeapOnNodeOffset int64) (uint32, error) {
	blocksTotalSize := make([]byte, 4)

	if _, err := file.Reader.ReadAt(blocksTotalSize, nodeEntryHeapOnNodeOffset+4); err != nil {
		return 0, eris.Wrap(err, "failed to read total blocks size")
	}

	return binary.LittleEndian.Uint32(blocksTotalSize), nil
}
