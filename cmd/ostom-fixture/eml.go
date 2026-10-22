package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime"
	"mime/multipart"
	"net/textproto"
	"os"
	"path/filepath"
	"time"
)

type emlManifest struct {
	Version int               `json:"version"`
	Files   []emlManifestFile `json:"files"`
}

type emlManifestFile struct {
	Name        string            `json:"name"`
	Subject     string            `json:"subject"`
	Attachments []emlManifestPart `json:"attachments,omitempty"`
	Bytes       int               `json:"bytes"`
	SHA256      string            `json:"sha256"`
}

type emlManifestPart struct {
	Name   string `json:"name"`
	Mime   string `json:"mime"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
	Inline bool   `json:"inline,omitempty"`
}

type fixtureAttachment struct {
	Name      string
	MimeType  string
	Data      []byte
	ContentID string
	Inline    bool
}

func generateEMLCorpus(out string, count int) error {
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	report := emlManifest{Version: 1, Files: make([]emlManifestFile, 0, count)}
	for index := 1; index <= count; index++ {
		subject := fmt.Sprintf("OSTOM fixture %02d — Unicode проверка", index)
		attachments := fixtureAttachments(index)
		data, err := renderFixtureEML(index, subject, attachments)
		if err != nil {
			return fmt.Errorf("message %d: %w", index, err)
		}
		name := fmt.Sprintf("message-%02d.eml", index)
		if err := os.WriteFile(filepath.Join(out, name), data, 0o644); err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		entry := emlManifestFile{Name: name, Subject: subject, Bytes: len(data), SHA256: hex.EncodeToString(sum[:])}
		for _, attachment := range attachments {
			partSum := sha256.Sum256(attachment.Data)
			entry.Attachments = append(entry.Attachments, emlManifestPart{
				Name: attachment.Name, Mime: attachment.MimeType, Bytes: len(attachment.Data),
				SHA256: hex.EncodeToString(partSum[:]), Inline: attachment.Inline,
			})
		}
		report.Files = append(report.Files, entry)
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, "manifest.json"), append(encoded, '\n'), 0o644)
}

func fixtureAttachments(index int) []fixtureAttachment {
	// Cycle the deterministic attachment profiles so -eml-count can produce a
	// genuinely attachment-heavy corpus instead of testing only the first five
	// messages. This keeps the first five fixture identities unchanged.
	switch ((index - 1) % 5) + 1 {
	case 1:
		return []fixtureAttachment{{Name: "mock_document.pdf", MimeType: "application/pdf", Data: []byte("%PDF-1.5\n% OSTOM deterministic fixture\n")}}
	case 2:
		return []fixtureAttachment{
			{Name: "inline.txt", MimeType: "text/plain; charset=utf-8", Data: []byte("inline fixture\n"), ContentID: "fixture-inline-02", Inline: true},
			{Name: "data.bin", MimeType: "application/octet-stream", Data: bytes.Repeat([]byte("0123456789abcdef"), 32)},
		}
	case 3:
		return []fixtureAttachment{{Name: "unicode-имя.txt", MimeType: "text/plain; charset=utf-8", Data: []byte("Привет из вложения\n")}}
	case 4:
		return []fixtureAttachment{{Name: "large.bin", MimeType: "application/octet-stream", Data: bytes.Repeat([]byte("large-fixture-payload-"), 8192)}}
	case 5:
		return []fixtureAttachment{
			{Name: "первое-данные.txt", MimeType: "text/plain; charset=utf-8", Data: []byte("Первое вложение\n")},
			{Name: "второе-данные.bin", MimeType: "application/octet-stream", Data: bytes.Repeat([]byte{0, 1, 2, 3, 0xFE, 0xFF}, 257)},
		}
	default:
		return nil
	}
}

func renderFixtureEML(index int, subject string, attachments []fixtureAttachment) ([]byte, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	if err := writer.SetBoundary(fmt.Sprintf("ostom-fixture-boundary-%02d", index)); err != nil {
		return nil, err
	}
	date := time.Date(2024, time.January, index, 12, 0, 0, 0, time.UTC)
	fmt.Fprintf(&buf, "From: Sender %02d <sender%02d@example.test>\r\n", index, index)
	fmt.Fprint(&buf, "To: Test Recipient <recipient@example.test>\r\n")
	fmt.Fprintf(&buf, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject))
	fmt.Fprintf(&buf, "Date: %s\r\n", date.Format(time.RFC1123Z))
	fmt.Fprintf(&buf, "Message-ID: <ostom-fixture-%02d@example.test>\r\n", index)
	fmt.Fprint(&buf, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&buf, "Content-Type: multipart/mixed; boundary=%q\r\n\r\n", writer.Boundary())

	textHeader := make(textproto.MIMEHeader)
	textHeader.Set("Content-Type", "text/plain; charset=utf-8")
	textHeader.Set("Content-Transfer-Encoding", "8bit")
	part, err := writer.CreatePart(textHeader)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(part, "Привет!\r\n\r\nДетерминированное письмо %02d.\r\n", index)
	for _, attachment := range attachments {
		header := make(textproto.MIMEHeader)
		header.Set("Content-Type", attachment.MimeType)
		header.Set("Content-Transfer-Encoding", "base64")
		disposition := fmt.Sprintf("attachment; filename=%q", attachment.Name)
		if attachment.Inline {
			disposition = fmt.Sprintf("inline; filename=%q", attachment.Name)
			header.Set("Content-ID", "<"+attachment.ContentID+">")
		}
		header.Set("Content-Disposition", disposition)
		part, err := writer.CreatePart(header)
		if err != nil {
			return nil, err
		}
		encoded := base64.StdEncoding.EncodeToString(attachment.Data)
		for start := 0; start < len(encoded); start += 76 {
			end := start + 76
			if end > len(encoded) {
				end = len(encoded)
			}
			fmt.Fprintf(part, "%s\r\n", encoded[start:end])
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
