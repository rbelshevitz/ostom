// Package manifest writes the durable recovery inventory for one OSTOM run.
package manifest

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Record describes one recovered or skipped message or attachment. Fields are
// deliberately evidence-oriented: an empty value means the backend could not
// establish it, rather than an inferred value.
type Record struct {
	Kind          string   `json:"kind"`
	Source        string   `json:"source"`
	Identity      string   `json:"identity"`
	Route         string   `json:"route"`
	StorePath     string   `json:"store_path,omitempty"`
	FolderPath    string   `json:"folder_path,omitempty"`
	MessageID     string   `json:"message_id,omitempty"`
	NID           string   `json:"nid,omitempty"`
	BID           string   `json:"bid,omitempty"`
	Status        string   `json:"status"`
	DeclaredBytes int64    `json:"declared_bytes,omitempty"`
	ReadableBytes int64    `json:"readable_bytes,omitempty"`
	ExportedBytes int64    `json:"exported_bytes,omitempty"`
	SHA256        string   `json:"sha256,omitempty"`
	OutputPath    string   `json:"output_path,omitempty"`
	DuplicateOf   string   `json:"duplicate_of,omitempty"`
	Errors        []string `json:"errors,omitempty"`
}

type Document struct {
	Version int      `json:"version"`
	Source  string   `json:"source"`
	Summary Summary  `json:"summary"`
	Records []Record `json:"records"`
}

// Summary is a small, stable projection of the recovery inventory. It keeps
// frontends from loading and classifying the complete record list just to
// render the outcome of a large recovery run.
type Summary struct {
	Total               int                       `json:"total"`
	Messages            int                       `json:"messages"`
	UniqueMessages      int                       `json:"unique_messages"`
	Attachments         int                       `json:"attachments"`
	ByStatus            map[string]int            `json:"by_status,omitempty"`
	ByKind              map[string]map[string]int `json:"by_kind,omitempty"`
	Orphans             int                       `json:"orphans,omitempty"`
	DuplicateCandidates int                       `json:"duplicate_candidates,omitempty"`
}

// Write creates matching deterministic JSON and CSV recovery manifests.
// Paths stored in records are intentionally relative to outputDir when the
// caller can provide one, so moving a completed recovery directory keeps the
// inventory useful.
func Write(outputDir, source string, records []Record) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	records = append([]Record(nil), records...)
	for i := range records {
		records[i].Errors = append([]string(nil), records[i].Errors...)
	}
	return writeOwned(outputDir, source, records)
}

// WriteOwned is for callers that will not use records again. It avoids a
// second full manifest-sized allocation during final recovery reporting.
func WriteOwned(outputDir, source string, records []Record) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	return writeOwned(outputDir, source, records)
}

func writeOwned(outputDir, source string, records []Record) error {
	for i := range records {
		sort.Strings(records[i].Errors)
	}
	sort.SliceStable(records, func(i, j int) bool {
		return recordKey(records[i]) < recordKey(records[j])
	})
	markDuplicateCandidates(records)
	if err := writeJSON(filepath.Join(outputDir, "recovery-manifest.json"), Document{Version: 1, Source: source, Summary: summarize(records), Records: records}); err != nil {
		return err
	}
	return writeCSV(filepath.Join(outputDir, "recovery-manifest.csv"), records)
}

func summarize(records []Record) Summary {
	summary := Summary{Total: len(records), ByStatus: make(map[string]int), ByKind: make(map[string]map[string]int)}
	for _, record := range records {
		switch record.Kind {
		case "message":
			summary.Messages++
			if record.DuplicateOf == "" {
				summary.UniqueMessages++
			}
		case "attachment":
			summary.Attachments++
		}
		status := record.Status
		if status == "" {
			status = "unknown"
		}
		summary.ByStatus[status]++
		if summary.ByKind[record.Kind] == nil {
			summary.ByKind[record.Kind] = make(map[string]int)
		}
		summary.ByKind[record.Kind][status]++
		if record.Kind == "message" && record.FolderPath == "" {
			summary.Orphans++
		}
		if record.DuplicateOf != "" {
			summary.DuplicateCandidates++
		}
	}
	return summary
}

// markDuplicateCandidates records deterministic, non-destructive matches. A
// Message-ID is the only cross-route identity strong enough to surface here;
// subject, size, and folder are deliberately excluded because they collide in
// real mailboxes. The first sorted record remains the reference record.
func markDuplicateCandidates(records []Record) {
	first := make(map[string]string)
	for i := range records {
		record := &records[i]
		if record.Kind != "message" || strings.TrimSpace(record.MessageID) == "" {
			continue
		}
		key := strings.TrimSpace(record.MessageID)
		identity := strings.Join([]string{record.Source, record.Route, record.Identity}, ":")
		if reference, ok := first[key]; ok {
			record.DuplicateOf = reference
			continue
		}
		first[key] = identity
	}
}

func recordKey(record Record) string {
	return strings.Join([]string{record.Source, record.Kind, record.Route, record.Identity, record.NID, record.BID, record.OutputPath}, "\x00")
}

func writeJSON(path string, document Document) error {
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, append(data, '\n'))
}

func writeCSV(path string, records []Record) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".ostom-manifest-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	w := csv.NewWriter(file)
	if err := w.Write([]string{"kind", "source", "identity", "route", "store_path", "folder_path", "message_id", "nid", "bid", "status", "declared_bytes", "readable_bytes", "exported_bytes", "sha256", "output_path", "duplicate_of", "errors"}); err != nil {
		file.Close()
		return err
	}
	for _, record := range records {
		if err := w.Write([]string{record.Kind, record.Source, record.Identity, record.Route, record.StorePath, record.FolderPath, record.MessageID, record.NID, record.BID, record.Status, strconv.FormatInt(record.DeclaredBytes, 10), strconv.FormatInt(record.ReadableBytes, 10), strconv.FormatInt(record.ExportedBytes, 10), record.SHA256, record.OutputPath, record.DuplicateOf, strings.Join(record.Errors, "; ")}); err != nil {
			file.Close()
			return err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("finalize CSV manifest: %w", err)
	}
	return nil
}

func writeAtomic(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".ostom-manifest-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("finalize JSON manifest: %w", err)
	}
	return nil
}
