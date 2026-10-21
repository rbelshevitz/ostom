package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// ItemEntry is one durable outcome from a single indexed or native item.
// Entries are append-only so an interrupted write never rewrites history.
type ItemEntry struct {
	TimeUTC    string `json:"time_utc"`
	Identity   string `json:"identity"`
	Kind       string `json:"kind"`
	Folder     string `json:"folder,omitempty"`
	Status     string `json:"status"`
	OutputPath string `json:"output_path,omitempty"`
	Error      string `json:"error,omitempty"`
}

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
	scanner := bufio.NewScanner(file)
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, 2*1024*1024)
	for scanner.Scan() {
		var entry ItemEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			return nil, fmt.Errorf("read item journal for resume: %w", err)
		}
		if entry.Identity != "" {
			completed[entry.Identity] = entry.Status == "exported"
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan item journal for resume: %w", err)
	}
	return completed, nil
}

func (j *Journal) Close() error { return j.file.Close() }
