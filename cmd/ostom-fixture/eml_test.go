package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateEMLCorpusIsDeterministic(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	if err := generateEMLCorpus(first, 6); err != nil {
		t.Fatal(err)
	}
	if err := generateEMLCorpus(second, 6); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(first, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report emlManifest
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report.Version != 1 || len(report.Files) != 6 || len(report.Files[0].Attachments) != 1 || len(report.Files[1].Attachments) != 2 {
		t.Fatalf("unexpected manifest: %+v", report)
	}
	for _, name := range []string{"message-01.eml", "message-02.eml", "message-06.eml"} {
		left, err := os.ReadFile(filepath.Join(first, name))
		if err != nil {
			t.Fatal(err)
		}
		right, err := os.ReadFile(filepath.Join(second, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(left) != string(right) {
			t.Fatalf("%s is not deterministic", name)
		}
	}
}

func TestGenerateEMLCorpusAttachmentsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := generateEMLCorpus(dir, 6); err != nil {
		t.Fatal(err)
	}
	manifestData, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest emlManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, entry := range manifest.Files {
		file, err := os.Open(filepath.Join(dir, entry.Name))
		if err != nil {
			t.Fatal(err)
		}
		message, err := mail.ReadMessage(file)
		if err != nil {
			_ = file.Close()
			t.Fatalf("read %s: %v", entry.Name, err)
		}
		mediaType, params, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
		if err != nil {
			t.Fatalf("parse %s content type: %v", entry.Name, err)
		}
		if len(entry.Attachments) == 0 {
			_ = file.Close()
			continue
		}
		if mediaType != "multipart/mixed" {
			_ = file.Close()
			t.Fatalf("%s content type = %q, want multipart/mixed", entry.Name, mediaType)
		}
		reader := multipart.NewReader(message.Body, params["boundary"])
		var attachments []emlManifestPart
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("read %s part: %v", entry.Name, err)
			}
			if part.FileName() == "" {
				continue
			}
			payload, err := io.ReadAll(part)
			if err != nil {
				t.Fatalf("read %s attachment: %v", entry.Name, err)
			}
			if strings.EqualFold(part.Header.Get("Content-Transfer-Encoding"), "base64") {
				decoded, decodeErr := io.ReadAll(base64.NewDecoder(base64.StdEncoding, strings.NewReader(string(payload))))
				if decodeErr != nil {
					t.Fatalf("decode %s attachment: %v", entry.Name, decodeErr)
				}
				payload = decoded
			}
			sum := sha256.Sum256(payload)
			attachments = append(attachments, emlManifestPart{
				Name: part.FileName(), Bytes: len(payload), SHA256: hex.EncodeToString(sum[:]),
				Inline: strings.HasPrefix(strings.ToLower(part.Header.Get("Content-Disposition")), "inline"),
			})
		}
		_ = file.Close()
		if len(attachments) != len(entry.Attachments) {
			t.Fatalf("%s attachment count = %d, want %d", entry.Name, len(attachments), len(entry.Attachments))
		}
		for index, got := range attachments {
			want := entry.Attachments[index]
			if got.Name != want.Name || got.Bytes != want.Bytes || got.SHA256 != want.SHA256 || got.Inline != want.Inline {
				t.Fatalf("%s attachment %d = %+v, want %+v", entry.Name, index, got, want)
			}
		}
	}
	if len(manifest.Files[3].Attachments) != 1 || manifest.Files[3].Attachments[0].Bytes <= 64<<10 {
		t.Fatalf("large attachment fixture was not generated: %+v", manifest.Files[3].Attachments)
	}
}

func TestGenerateEMLCorpusTruncatedAttachmentIsDetected(t *testing.T) {
	dir := t.TempDir()
	if err := generateEMLCorpus(dir, 6); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "message-04.eml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 32 {
		t.Fatal("fixture unexpectedly short")
	}
	message, err := mail.ReadMessage(bytes.NewReader(raw[:len(raw)-24]))
	if err != nil {
		t.Fatal(err)
	}
	_, params, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	reader := multipart.NewReader(message.Body, params["boundary"])
	detected := false
	for {
		part, partErr := reader.NextPart()
		if partErr == io.EOF {
			break
		}
		if partErr != nil {
			detected = true
			break
		}
		if part.FileName() == "" {
			continue
		}
		payload, readErr := io.ReadAll(part)
		if readErr != nil {
			detected = true
			break
		}
		if strings.EqualFold(part.Header.Get("Content-Transfer-Encoding"), "base64") {
			if _, decodeErr := io.ReadAll(base64.NewDecoder(base64.StdEncoding, bytes.NewReader(payload))); decodeErr != nil {
				detected = true
				break
			}
		}
	}
	if !detected {
		t.Fatal("truncated attachment was accepted without an observable MIME error")
	}
}

func TestGenerateEMLCorpusRemainsAttachmentHeavyAtLongRunCount(t *testing.T) {
	dir := t.TempDir()
	const count = 256
	if err := generateEMLCorpus(dir, count); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report emlManifest
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	attachments, large := 0, 0
	for _, file := range report.Files {
		if len(file.Attachments) == 0 {
			t.Fatalf("message %s lost its attachment profile", file.Name)
		}
		attachments += len(file.Attachments)
		for _, part := range file.Attachments {
			if part.Bytes > 64<<10 {
				large++
			}
		}
	}
	if len(report.Files) != count || attachments != 358 || large != 51 {
		t.Fatalf("unexpected long-run attachment profile: files=%d attachments=%d large=%d", len(report.Files), attachments, large)
	}
}
