package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// ItemEntry is one durable outcome from a single indexed or native item.
// Entries are append-only so an interrupted write never rewrites history.
type ItemEntry struct {
	TimeUTC       string `json:"time_utc"`
	Source        string `json:"source,omitempty"`
	Identity      string `json:"identity"`
	Kind          string `json:"kind"`
	Route         string `json:"route,omitempty"`
	Folder        string `json:"folder,omitempty"`
	MessageID     string `json:"message_id,omitempty"`
	Status        string `json:"status"`
	DeclaredBytes int64  `json:"declared_bytes,omitempty"`
	ReadableBytes int64  `json:"readable_bytes,omitempty"`
	ExportedBytes int64  `json:"exported_bytes,omitempty"`
	SHA256        string `json:"sha256,omitempty"`
	OutputPath    string `json:"output_path,omitempty"`
	OutputOffset  int64  `json:"output_offset,omitempty"`
	Error         string `json:"error,omitempty"`
}

var itemStates = map[string]struct{}{
	"discovered":  {},
	"in_progress": {},
	"exported":    {},
	"partial":     {},
	"skipped":     {},
	"failed":      {},
}

const maxJournalLine = 2 * 1024 * 1024

type Journal struct {
	file *os.File
	mu   sync.Mutex
}

func OpenJournal(path string) (*Journal, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open item journal: %w", err)
	}
	return &Journal{file: file}, nil
}

func (j *Journal) Append(entry ItemEntry) error {
	if err := validateItemEntry(entry); err != nil {
		return err
	}
	entry.TimeUTC = time.Now().UTC().Format(time.RFC3339Nano)
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode item journal: %w", err)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if _, err := j.file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write item journal: %w", err)
	}
	return j.file.Sync()
}

func validateItemEntry(entry ItemEntry) error {
	if strings.TrimSpace(entry.Identity) == "" {
		return fmt.Errorf("item journal identity is required")
	}
	if strings.TrimSpace(entry.Kind) == "" {
		return fmt.Errorf("item journal kind is required")
	}
	if _, ok := itemStates[entry.Status]; !ok {
		return fmt.Errorf("item journal status %q is invalid", entry.Status)
	}
	if entry.DeclaredBytes < 0 || entry.ReadableBytes < 0 || entry.ExportedBytes < 0 {
		return fmt.Errorf("item journal byte counts cannot be negative")
	}
	return nil
}

func Completed(path string) (map[string]bool, error) {
	completed := make(map[string]bool)
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return completed, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open item journal for resume: %w", err)
	}
	defer file.Close()
	err = scanJournal(file, func(entry ItemEntry) {
		if entry.Identity != "" {
			completed[entry.Identity] = entry.Status == "exported"
		}
	})
	if err != nil {
		return nil, fmt.Errorf("scan item journal for resume: %w", err)
	}
	return completed, nil
}

// Latest returns the last durable state for each item. It is intended for
// final inventory generation; resume/repair callers should use the bounded
// helpers above instead of retaining the complete history.
func Latest(path string) (map[string]ItemEntry, error) {
	latest := make(map[string]ItemEntry)
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return latest, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open item journal for inventory: %w", err)
	}
	defer file.Close()
	err = scanJournal(file, func(entry ItemEntry) {
		if entry.Identity != "" {
			latest[entry.Identity] = entry
		}
	})
	if err != nil {
		return nil, fmt.Errorf("scan item journal for inventory: %w", err)
	}
	return latest, nil
}

// RepairIndexedOutputs removes output written by an item whose final outcome
// was not durably journaled. The next run can then retry that item without
// duplicating a partial mbox record.
func RepairIndexedOutputs(path string) error {
	pending := make(map[string]ItemEntry)
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open item journal for repair: %w", err)
	}
	err = scanJournal(file, func(entry ItemEntry) {
		if entry.Identity != "" {
			switch entry.Status {
			case "exported", "skipped":
				delete(pending, entry.Identity)
			default:
				pending[entry.Identity] = entry
			}
		}
	})
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("scan item journal for repair: %w", err)
	}
	if err := file.Close(); err != nil {
		return err
	}
	for _, entry := range pending {
		if entry.Status == "exported" || entry.Status == "skipped" || entry.OutputPath == "" || entry.OutputOffset < 0 {
			continue
		}
		if err := os.Truncate(entry.OutputPath, entry.OutputOffset); err != nil {
			return fmt.Errorf("truncate incomplete indexed output %q: %w", entry.OutputPath, err)
		}
	}
	return nil
}

// scanJournal treats an incomplete final line as a crash tail. A complete
// malformed line, or any malformed line before EOF, remains an integrity
// failure and must not be silently discarded.
func scanJournal(file *os.File, visit func(ItemEntry)) error {
	reader := bufio.NewReaderSize(file, 64*1024)
	var line []byte
	for {
		part, err := reader.ReadSlice('\n')
		line = append(line, part...)
		if len(line) > maxJournalLine {
			return fmt.Errorf("item journal line exceeds %d bytes", maxJournalLine)
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if len(line) != 0 {
			if err == io.EOF && line[len(line)-1] != '\n' {
				return nil
			}
			var entry ItemEntry
			if unmarshalErr := json.Unmarshal(line, &entry); unmarshalErr != nil {
				return fmt.Errorf("read item journal: %w", unmarshalErr)
			}
			visit(entry)
			line = line[:0]
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (j *Journal) Close() error { return j.file.Close() }
