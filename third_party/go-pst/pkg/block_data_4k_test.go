package pst

import (
	"bytes"
	"compress/zlib"
	"io"
	"testing"
)

func TestReadBTreeBlockDataInflatesUnicode4kBlock(t *testing.T) {
	payload := []byte("known uncompressed Outlook block payload")
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	file := &File{
		Reader:     NewDefaultReader(bytes.NewReader(compressed.Bytes())),
		FormatType: FormatTypeUnicode4k,
	}
	block := BTreeNode{
		Identifier:   2,
		FileOffset:   0,
		Size:         uint16(compressed.Len()),
		InflatedSize: uint16(len(payload)),
	}
	got, err := file.readBTreeBlockData(block)
	if err != nil {
		t.Fatalf("readBTreeBlockData() error = %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("readBTreeBlockData() = %q, want %q", got, payload)
	}
}

func TestReadBTreeBlockDataHonorsInflatedSize(t *testing.T) {
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	_, _ = io.WriteString(writer, "payload longer than declared size")
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	file := &File{
		Reader:     NewDefaultReader(bytes.NewReader(compressed.Bytes())),
		FormatType: FormatTypeUnicode4k,
	}
	block := BTreeNode{Identifier: 2, Size: uint16(compressed.Len()), InflatedSize: 4}
	if _, err := file.readBTreeBlockData(block); err == nil {
		t.Fatal("readBTreeBlockData() error = nil, want declared-size mismatch")
	}
}
