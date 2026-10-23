package session

import (
	"bufio"
	"container/heap"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

const journalSortChunkSize = 1024

type journalSortEntry struct {
	Sequence uint64    `json:"sequence"`
	Entry    ItemEntry `json:"entry"`
}

// StreamLatest visits only the last durable entry for each identity without
// retaining the complete journal in memory. It is the bounded counterpart to
// Latest and is used when building the final recovery inventory.
func StreamLatest(path string, visit func(ItemEntry) error) error {
	if visit == nil {
		return fmt.Errorf("journal visitor is nil")
	}
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open item journal for streaming inventory: %w", err)
	}
	defer file.Close()

	workDir, err := os.MkdirTemp(filepath.Dir(path), ".journal-stream-")
	if err != nil {
		return fmt.Errorf("create journal stream workspace: %w", err)
	}
	defer os.RemoveAll(workDir)

	var chunks []string
	chunk := make([]journalSortEntry, 0, journalSortChunkSize)
	flush := func() error {
		if len(chunk) == 0 {
			return nil
		}
		sort.Slice(chunk, func(i, j int) bool {
			if chunk[i].Entry.Identity != chunk[j].Entry.Identity {
				return chunk[i].Entry.Identity < chunk[j].Entry.Identity
			}
			return chunk[i].Sequence < chunk[j].Sequence
		})
		path := filepath.Join(workDir, fmt.Sprintf("chunk-%06d.jsonl", len(chunks)))
		output, err := os.Create(path)
		if err != nil {
			return fmt.Errorf("create journal sort chunk: %w", err)
		}
		encoder := json.NewEncoder(output)
		for _, item := range chunk {
			if err := encoder.Encode(item); err != nil {
				_ = output.Close()
				return fmt.Errorf("write journal sort chunk: %w", err)
			}
		}
		if err := output.Close(); err != nil {
			return fmt.Errorf("close journal sort chunk: %w", err)
		}
		chunks = append(chunks, path)
		chunk = chunk[:0]
		return nil
	}
	if err := scanJournalSequenced(file, func(entry ItemEntry, ordinal uint64) error {
		chunk = append(chunk, journalSortEntry{Sequence: ordinal, Entry: entry})
		if len(chunk) == journalSortChunkSize {
			return flush()
		}
		return nil
	}); err != nil {
		return err
	}
	if err := flush(); err != nil {
		return err
	}
	if len(chunks) == 0 {
		return nil
	}

	readers := make([]*journalChunkReader, len(chunks))
	queue := &journalEntryHeap{}
	heap.Init(queue)
	for index, chunkPath := range chunks {
		reader, err := openJournalChunk(chunkPath)
		if err != nil {
			return err
		}
		readers[index] = reader
		if item, ok, err := reader.next(); err != nil {
			return err
		} else if ok {
			heap.Push(queue, journalHeapItem{entry: item, reader: index})
		}
	}
	defer func() {
		for _, reader := range readers {
			if reader != nil {
				_ = reader.file.Close()
			}
		}
	}()

	var current *journalSortEntry
	flushCurrent := func() error {
		if current == nil {
			return nil
		}
		return visit(current.Entry)
	}
	for queue.Len() > 0 {
		item := heap.Pop(queue).(journalHeapItem)
		if current == nil || current.Entry.Identity != item.entry.Entry.Identity {
			if err := flushCurrent(); err != nil {
				return err
			}
			copy := item.entry
			current = &copy
		} else if item.entry.Sequence > current.Sequence {
			copy := item.entry
			current = &copy
		}
		reader := readers[item.reader]
		if next, ok, err := reader.next(); err != nil {
			return err
		} else if ok {
			heap.Push(queue, journalHeapItem{entry: next, reader: item.reader})
		}
	}
	return flushCurrent()
}

type journalChunkReader struct {
	file    *os.File
	decoder *json.Decoder
}

func openJournalChunk(path string) (*journalChunkReader, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open journal sort chunk: %w", err)
	}
	return &journalChunkReader{file: file, decoder: json.NewDecoder(bufio.NewReaderSize(file, 64*1024))}, nil
}

func (r *journalChunkReader) next() (journalSortEntry, bool, error) {
	var item journalSortEntry
	err := r.decoder.Decode(&item)
	if err == io.EOF {
		return journalSortEntry{}, false, nil
	}
	if err != nil {
		return journalSortEntry{}, false, fmt.Errorf("read journal sort chunk: %w", err)
	}
	return item, true, nil
}

type journalHeapItem struct {
	entry  journalSortEntry
	reader int
}

type journalEntryHeap []journalHeapItem

func (h journalEntryHeap) Len() int { return len(h) }
func (h journalEntryHeap) Less(i, j int) bool {
	if h[i].entry.Entry.Identity != h[j].entry.Entry.Identity {
		return h[i].entry.Entry.Identity < h[j].entry.Entry.Identity
	}
	return h[i].entry.Sequence < h[j].entry.Sequence
}
func (h journalEntryHeap) Swap(i, j int)   { h[i], h[j] = h[j], h[i] }
func (h *journalEntryHeap) Push(value any) { *h = append(*h, value.(journalHeapItem)) }
func (h *journalEntryHeap) Pop() any {
	old := *h
	last := len(old) - 1
	value := old[last]
	*h = old[:last]
	return value
}
