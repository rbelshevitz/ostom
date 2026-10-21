// Package session stores resumable recovery metadata outside the user-facing
// export trees.
package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const schemaVersion = 1

type Options struct {
	RecoveryScan bool `json:"recovery_scan"`
	FullEML      bool `json:"full_eml"`
}

type Fingerprint struct {
	Size         int64  `json:"size"`
	ModifiedUTC  string `json:"modified_utc"`
	HeaderSHA256 string `json:"header_sha256"`
}

type State struct {
	Version     int         `json:"version"`
	SessionID   string      `json:"session_id"`
	Source      string      `json:"source"`
	ExportDir   string      `json:"export_dir"`
	Fingerprint Fingerprint `json:"fingerprint"`
	Options     Options     `json:"options"`
	Phase       string      `json:"phase"`
	Cursor      string      `json:"cursor,omitempty"`
	UpdatedUTC  string      `json:"updated_utc"`
}

// Batch is the durable record for one internal portion of work. A portion is
// reusable only after it is marked committed and its artifact passes checks.
type Batch struct {
	Number        int      `json:"number"`
	Status        string   `json:"status"` // writing, committed, failed
	Artifact      string   `json:"artifact"`
	FirstIdentity string   `json:"first_identity,omitempty"`
	LastIdentity  string   `json:"last_identity,omitempty"`
	Identities    []string `json:"identities,omitempty"`
	Items         int      `json:"items"`
	Bytes         int64    `json:"bytes"`
	SHA256        string   `json:"sha256,omitempty"`
	UpdatedUTC    string   `json:"updated_utc"`
}

type Store struct {
	path  string
	dir   string
	state State
}

func Open(exportDir, source string, options Options) (*Store, error) {
	absoluteExport, err := filepath.Abs(exportDir)
	if err != nil {
		return nil, fmt.Errorf("resolve export directory: %w", err)
	}
	absoluteSource, err := filepath.Abs(source)
	if err != nil {
		return nil, fmt.Errorf("resolve source: %w", err)
	}
	fingerprint, err := fingerprintFile(absoluteSource)
	if err != nil {
		return nil, err
	}
	identity := sha256.Sum256([]byte(absoluteSource + "\x00" + absoluteExport + "\x00" + fingerprint.SizeString() + fmt.Sprintf(":%t:%t", options.RecoveryScan, options.FullEML)))
	sessionID := hex.EncodeToString(identity[:])[:24]
	directory := filepath.Join(absoluteExport, ".ostom", "sessions", sessionID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create session directory: %w", err)
	}
	store := &Store{path: filepath.Join(directory, "session.json"), dir: directory}
	if data, readErr := os.ReadFile(store.path); readErr == nil {
		if err := json.Unmarshal(data, &store.state); err != nil {
			return nil, fmt.Errorf("read session state: %w", err)
		}
		if store.state.Fingerprint != fingerprint || store.state.Source != absoluteSource || store.state.Options != options {
			return nil, fmt.Errorf("session fingerprint does not match source")
		}
		return store, nil
	} else if !os.IsNotExist(readErr) {
		return nil, fmt.Errorf("open session state: %w", readErr)
	}
	store.state = State{
		Version:     schemaVersion,
		SessionID:   sessionID,
		Source:      absoluteSource,
		ExportDir:   absoluteExport,
		Fingerprint: fingerprint,
		Options:     options,
		Phase:       "started",
		UpdatedUTC:  time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := store.save(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) State() State { return s.state }

func (s *Store) JournalPath() string { return filepath.Join(s.dir, "items.jsonl") }

// BatchesDir is deliberately hidden from the user-facing Thunderbird tree.
func (s *Store) BatchesDir() string { return filepath.Join(s.dir, "batches") }

func (s *Store) WorkDir(name string) string { return filepath.Join(s.dir, name) }

func (s *Store) BeginBatch(number int) (string, error) {
	if number < 1 {
		return "", fmt.Errorf("invalid batch number %d", number)
	}
	directory := filepath.Join(s.BatchesDir(), fmt.Sprintf("%06d", number))
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("create batch directory: %w", err)
	}
	return directory, nil
}

func (s *Store) Update(phase, cursor string) error {
	s.state.Phase = phase
	s.state.Cursor = cursor
	s.state.UpdatedUTC = time.Now().UTC().Format(time.RFC3339Nano)
	return s.save()
}

func (f Fingerprint) SizeString() string {
	return fmt.Sprintf("%d:%s:%s", f.Size, f.ModifiedUTC, f.HeaderSHA256)
}

func fingerprintFile(path string) (Fingerprint, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Fingerprint{}, fmt.Errorf("stat source for session: %w", err)
	}
	file, err := os.Open(path)
	if err != nil {
		return Fingerprint{}, fmt.Errorf("open source for session: %w", err)
	}
	defer file.Close()
	const sampleSize int64 = 64 << 10
	data := make([]byte, sampleSize)
	read, readErr := file.Read(data)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return Fingerprint{}, fmt.Errorf("read source fingerprint: %w", readErr)
	}
	hash := sha256.Sum256(data[:read])
	return Fingerprint{Size: info.Size(), ModifiedUTC: info.ModTime().UTC().Format(time.RFC3339Nano), HeaderSHA256: hex.EncodeToString(hash[:])}, nil
}

func (s *Store) save() error {
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode session state: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(s.path), ".session-*")
	if err != nil {
		return fmt.Errorf("create session temporary: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write session state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close session state: %w", err)
	}
	if err := os.Rename(temporaryPath, s.path); err != nil {
		return fmt.Errorf("finalize session state: %w", err)
	}
	return nil
}
