package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// WriteBatchRecord writes the completion marker last. A missing or malformed
// marker means that the batch is not reusable after an interruption.
func WriteBatchRecord(directory string, batch Batch) error {
	if batch.Number < 1 || batch.Status == "" {
		return fmt.Errorf("invalid batch record")
	}
	batch.UpdatedUTC = time.Now().UTC().Format(time.RFC3339Nano)
	data, err := json.MarshalIndent(batch, "", "  ")
	if err != nil {
		return fmt.Errorf("encode batch record: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".batch-*")
	if err != nil {
		return fmt.Errorf("create batch record: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write batch record: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync batch record: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close batch record: %w", err)
	}
	if err := os.Rename(temporaryPath, filepath.Join(directory, "batch.json")); err != nil {
		return fmt.Errorf("finalize batch record: %w", err)
	}
	return nil
}

func ReadBatchRecord(directory string) (Batch, error) {
	data, err := os.ReadFile(filepath.Join(directory, "batch.json"))
	if err != nil {
		return Batch{}, err
	}
	var batch Batch
	if err := json.Unmarshal(data, &batch); err != nil {
		return Batch{}, fmt.Errorf("decode batch record: %w", err)
	}
	return batch, nil
}

func VerifyArtifact(batch Batch, directory string) error {
	if batch.Status != "committed" || batch.Artifact == "" {
		return fmt.Errorf("batch is not committed")
	}
	bytes, checksum, err := DigestArtifact(directory, batch.Artifact)
	if err != nil {
		return err
	}
	if batch.Bytes != bytes {
		return fmt.Errorf("batch artifact size mismatch: got %d, want %d", bytes, batch.Bytes)
	}
	if batch.SHA256 != "" && batch.SHA256 != checksum {
		return fmt.Errorf("batch artifact checksum mismatch")
	}
	if batch.Manifest != "" {
		manifestBytes, manifestChecksum, err := DigestArtifact(directory, batch.Manifest)
		if err != nil {
			return fmt.Errorf("verify batch manifest: %w", err)
		}
		if batch.ManifestBytes != manifestBytes {
			return fmt.Errorf("batch manifest size mismatch: got %d, want %d", manifestBytes, batch.ManifestBytes)
		}
		if batch.ManifestSHA256 != "" && batch.ManifestSHA256 != manifestChecksum {
			return fmt.Errorf("batch manifest checksum mismatch")
		}
	}
	return nil
}

// DigestArtifact returns a deterministic digest for a file or directory.
// Directory entries are sorted and include their relative names, so renaming
// or removing a result file invalidates the batch as well.
func DigestArtifact(directory, artifact string) (int64, string, error) {
	hash := sha256.New()
	artifactPath := filepath.Join(directory, artifact)
	info, err := os.Stat(artifactPath)
	if err != nil {
		return 0, "", err
	}
	var bytes int64
	if info.IsDir() {
		files := make([]string, 0)
		err = filepath.WalkDir(artifactPath, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			relative, err := filepath.Rel(artifactPath, path)
			if err != nil {
				return err
			}
			files = append(files, relative)
			return nil
		})
		if err != nil {
			return 0, "", fmt.Errorf("walk batch artifact: %w", err)
		}
		sort.Strings(files)
		for _, relative := range files {
			if _, err := io.WriteString(hash, relative+"\x00"); err != nil {
				return 0, "", err
			}
			file, err := os.Open(filepath.Join(artifactPath, relative))
			if err != nil {
				return 0, "", err
			}
			count, copyErr := io.Copy(hash, file)
			closeErr := file.Close()
			if copyErr != nil {
				return 0, "", fmt.Errorf("read batch artifact: %w", copyErr)
			}
			if closeErr != nil {
				return 0, "", closeErr
			}
			bytes += count
		}
	} else {
		file, err := os.Open(artifactPath)
		if err != nil {
			return 0, "", err
		}
		bytes, err = io.Copy(hash, file)
		closeErr := file.Close()
		if err != nil {
			return 0, "", fmt.Errorf("read batch artifact: %w", err)
		}
		if closeErr != nil {
			return 0, "", closeErr
		}
	}
	return bytes, hex.EncodeToString(hash.Sum(nil)), nil
}
