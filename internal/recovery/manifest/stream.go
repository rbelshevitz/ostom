package manifest

import (
	"bufio"
	"container/heap"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const streamChunkRecords = 1024

// WriteStream writes the recovery manifests from a record producer without
// retaining the complete inventory in memory. The producer may emit records
// in any order; bounded sorted chunks preserve the deterministic output order
// used by WriteOwned.
func WriteStream(outputDir, source string, produce func(func(Record) error) error) error {
	if produce == nil {
		return fmt.Errorf("manifest record producer is nil")
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	workDir, err := os.MkdirTemp(outputDir, ".manifest-stream-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)

	var chunks []string
	chunk := make([]Record, 0, streamChunkRecords)
	flush := func() error {
		if len(chunk) == 0 {
			return nil
		}
		for index := range chunk {
			sort.Strings(chunk[index].Errors)
		}
		sort.SliceStable(chunk, func(i, j int) bool { return recordKey(chunk[i]) < recordKey(chunk[j]) })
		file, err := os.CreateTemp(workDir, "chunk-*.jsonl")
		if err != nil {
			return err
		}
		path := file.Name()
		encoder := json.NewEncoder(file)
		for _, record := range chunk {
			if err := encoder.Encode(record); err != nil {
				_ = file.Close()
				_ = os.Remove(path)
				return err
			}
		}
		if err := file.Close(); err != nil {
			_ = os.Remove(path)
			return err
		}
		chunks = append(chunks, path)
		chunk = chunk[:0]
		return nil
	}
	if err := produce(func(record Record) error {
		chunk = append(chunk, record)
		if len(chunk) >= streamChunkRecords {
			return flush()
		}
		return nil
	}); err != nil {
		return err
	}
	if err := flush(); err != nil {
		return err
	}

	merged, err := os.CreateTemp(workDir, "merged-*.jsonl")
	if err != nil {
		return err
	}
	mergedPath := merged.Name()
	defer os.Remove(mergedPath)
	summary, err := mergeManifestChunks(merged, chunks)
	if closeErr := merged.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return writeStreamOutputs(outputDir, source, mergedPath, summary)
}

type streamReader struct {
	file    *os.File
	scanner *bufio.Scanner
	record  Record
	valid   bool
}

type streamItem struct {
	index  int
	record Record
}

type streamHeap []streamItem

func (h streamHeap) Len() int           { return len(h) }
func (h streamHeap) Less(i, j int) bool { return recordKey(h[i].record) < recordKey(h[j].record) }
func (h streamHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *streamHeap) Push(value any)    { *h = append(*h, value.(streamItem)) }
func (h *streamHeap) Pop() any {
	old := *h
	last := old[len(old)-1]
	*h = old[:len(old)-1]
	return last
}

func mergeManifestChunks(output *os.File, paths []string) (Summary, error) {
	readers := make([]streamReader, 0, len(paths))
	queue := &streamHeap{}
	heap.Init(queue)
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			return Summary{}, err
		}
		reader := streamReader{file: file, scanner: bufio.NewScanner(file)}
		reader.scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
		if err := readStreamRecord(&reader); err != nil {
			_ = file.Close()
			return Summary{}, err
		}
		index := len(readers)
		readers = append(readers, reader)
		if reader.valid {
			heap.Push(queue, streamItem{index: index, record: reader.record})
		}
	}
	defer func() {
		for index := range readers {
			_ = readers[index].file.Close()
		}
	}()

	summary := Summary{ByStatus: make(map[string]int), ByKind: make(map[string]map[string]int)}
	firstMessage := make(map[string]string)
	encoder := json.NewEncoder(output)
	for queue.Len() > 0 {
		item := heap.Pop(queue).(streamItem)
		record := item.record
		if record.Kind == "message" && strings.TrimSpace(record.MessageID) != "" {
			key := strings.TrimSpace(record.MessageID)
			identity := strings.Join([]string{record.Source, record.Route, record.Identity}, ":")
			if reference, ok := firstMessage[key]; ok {
				record.DuplicateOf = reference
			} else {
				firstMessage[key] = identity
			}
		}
		if err := encoder.Encode(record); err != nil {
			return Summary{}, err
		}
		updateSummary(&summary, record)

		reader := &readers[item.index]
		if err := readStreamRecord(reader); err != nil {
			return Summary{}, err
		}
		if reader.valid {
			heap.Push(queue, streamItem{index: item.index, record: reader.record})
		}
	}
	return summary, nil
}

func readStreamRecord(reader *streamReader) error {
	if !reader.scanner.Scan() {
		reader.valid = false
		if err := reader.scanner.Err(); err != nil {
			return err
		}
		return nil
	}
	if err := json.Unmarshal(reader.scanner.Bytes(), &reader.record); err != nil {
		return err
	}
	reader.valid = true
	return nil
}

func updateSummary(summary *Summary, record Record) {
	summary.Total++
	status := record.Status
	if status == "" {
		status = "unknown"
	}
	summary.ByStatus[status]++
	if summary.ByKind[record.Kind] == nil {
		summary.ByKind[record.Kind] = make(map[string]int)
	}
	summary.ByKind[record.Kind][status]++
	switch record.Kind {
	case "message":
		summary.Messages++
		if record.DuplicateOf == "" {
			summary.UniqueMessages++
		}
		if record.FolderPath == "" {
			summary.Orphans++
		}
	case "attachment":
		summary.Attachments++
	}
	if record.DuplicateOf != "" {
		summary.DuplicateCandidates++
	}
}

func writeStreamOutputs(outputDir, source, mergedPath string, summary Summary) error {
	jsonFile, err := os.CreateTemp(outputDir, ".recovery-manifest-*.json")
	if err != nil {
		return err
	}
	jsonTemp := jsonFile.Name()
	defer os.Remove(jsonTemp)
	csvFile, err := os.CreateTemp(outputDir, ".recovery-manifest-*.csv")
	if err != nil {
		_ = jsonFile.Close()
		return err
	}
	csvTemp := csvFile.Name()
	defer os.Remove(csvTemp)

	encodedSource, err := json.Marshal(source)
	if err != nil {
		_ = jsonFile.Close()
		_ = csvFile.Close()
		return err
	}
	encodedSummary, err := json.Marshal(summary)
	if err != nil {
		_ = jsonFile.Close()
		_ = csvFile.Close()
		return err
	}
	if _, err := fmt.Fprintf(jsonFile, `{"version":1,"source":%s,"summary":%s,"records":[`, encodedSource, encodedSummary); err != nil {
		_ = jsonFile.Close()
		_ = csvFile.Close()
		return err
	}
	csvWriter := csv.NewWriter(csvFile)
	if err := csvWriter.Write([]string{"kind", "source", "identity", "route", "store_path", "folder_path", "message_id", "nid", "bid", "status", "declared_bytes", "readable_bytes", "exported_bytes", "sha256", "output_path", "duplicate_of", "errors"}); err != nil {
		_ = jsonFile.Close()
		_ = csvFile.Close()
		return err
	}

	input, err := os.Open(mergedPath)
	if err != nil {
		_ = jsonFile.Close()
		_ = csvFile.Close()
		return err
	}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	first := true
	for scanner.Scan() {
		line := scanner.Bytes()
		if !first {
			if _, err := io.WriteString(jsonFile, ","); err != nil {
				_ = input.Close()
				_ = jsonFile.Close()
				_ = csvFile.Close()
				return err
			}
		}
		first = false
		if _, err := jsonFile.Write(line); err != nil {
			_ = input.Close()
			_ = jsonFile.Close()
			_ = csvFile.Close()
			return err
		}
		var record Record
		if err := json.Unmarshal(line, &record); err != nil {
			_ = input.Close()
			_ = jsonFile.Close()
			_ = csvFile.Close()
			return err
		}
		if err := csvWriter.Write(recordCSVRow(record)); err != nil {
			_ = input.Close()
			_ = jsonFile.Close()
			_ = csvFile.Close()
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		_ = input.Close()
		_ = jsonFile.Close()
		_ = csvFile.Close()
		return err
	}
	if err := input.Close(); err != nil {
		_ = jsonFile.Close()
		_ = csvFile.Close()
		return err
	}
	if _, err := io.WriteString(jsonFile, "]}\n"); err != nil {
		_ = jsonFile.Close()
		_ = csvFile.Close()
		return err
	}
	csvWriter.Flush()
	if err := csvWriter.Error(); err != nil {
		_ = jsonFile.Close()
		_ = csvFile.Close()
		return err
	}
	if err := jsonFile.Close(); err != nil {
		_ = csvFile.Close()
		return err
	}
	if err := csvFile.Close(); err != nil {
		return err
	}
	if err := os.Rename(jsonTemp, filepath.Join(outputDir, "recovery-manifest.json")); err != nil {
		return err
	}
	if err := os.Rename(csvTemp, filepath.Join(outputDir, "recovery-manifest.csv")); err != nil {
		return err
	}
	return nil
}

func recordCSVRow(record Record) []string {
	return []string{record.Kind, record.Source, record.Identity, record.Route, record.StorePath, record.FolderPath, record.MessageID, record.NID, record.BID, record.Status, strconv.FormatInt(record.DeclaredBytes, 10), strconv.FormatInt(record.ReadableBytes, 10), strconv.FormatInt(record.ExportedBytes, 10), record.SHA256, record.OutputPath, record.DuplicateOf, strings.Join(record.Errors, "; ")}
}
