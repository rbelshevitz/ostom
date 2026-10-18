package pst

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestUnicode4kBTreePageMetadataAndEntries(t *testing.T) {
	page := make([]byte, 4096)
	binary.LittleEndian.PutUint16(page[unicode4kBTreeEntryCountOff:], 1)
	page[unicode4kBTreeEntrySizeOff] = 24
	page[unicode4kBTreeLevelOffset] = 0
	binary.LittleEndian.PutUint32(page[0:4], 290)
	binary.LittleEndian.PutUint32(page[8:12], 34)
	binary.LittleEndian.PutUint32(page[16:20], 35)

	file := &File{
		Reader:     NewDefaultReader(bytes.NewReader(page)),
		FormatType: FormatTypeUnicode4k,
	}
	var entries []BTreeNode
	var level uint8
	var gotErr error
	callbackCount := 0
	file.GetBTreeNodeEntries(0, BTreeTypeNode, func(got []BTreeNode, gotLevel uint8, err error) {
		callbackCount++
		entries, level, gotErr = got, gotLevel, err
	})

	if callbackCount != 1 {
		t.Fatalf("callback called %d times, want once", callbackCount)
	}
	if gotErr != nil {
		t.Fatalf("GetBTreeNodeEntries() error = %v", gotErr)
	}
	if level != 0 || len(entries) != 1 {
		t.Fatalf("got level %d and %d entries, want level 0 and one entry", level, len(entries))
	}
	if entries[0].Identifier != 290 || entries[0].DataIdentifier != 34 || entries[0].LocalDescriptorsIdentifier != 35 {
		t.Fatalf("unexpected leaf entry: %+v", entries[0])
	}
}

func TestUnicode4kBTreeRejectsEntryArrayOverrun(t *testing.T) {
	page := make([]byte, 4096)
	binary.LittleEndian.PutUint16(page[unicode4kBTreeEntryCountOff:], 200)
	page[unicode4kBTreeEntrySizeOff] = 24

	file := &File{
		Reader:     NewDefaultReader(bytes.NewReader(page)),
		FormatType: FormatTypeUnicode4k,
	}
	callbackCount := 0
	var gotErr error
	file.GetBTreeNodeEntries(0, BTreeTypeNode, func(_ []BTreeNode, _ uint8, err error) {
		callbackCount++
		gotErr = err
	})

	if callbackCount != 1 {
		t.Fatalf("callback called %d times, want once", callbackCount)
	}
	if gotErr == nil {
		t.Fatal("GetBTreeNodeEntries() error = nil, want entry-array bounds error")
	}
}
