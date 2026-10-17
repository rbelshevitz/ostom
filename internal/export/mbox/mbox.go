// Package mbox writes Thunderbird-compatible mbox files.
package mbox

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"mime"
	"mime/multipart"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Attachment struct {
	Name     string
	MimeType string
	Data     []byte
}

type Writer struct {
	Root                string
	MaxAttachmentMemory int64
	SkippedAttachments  int
	Errors              []string
}

func New(root string, maxAttachmentMemory int64) (*Writer, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &Writer{Root: root, MaxAttachmentMemory: maxAttachmentMemory}, nil
}

// Write appends one RFC 5322 message to the mbox represented by folderPath.
// folderPath is a logical folder path such as []string{"Sent", "Old"}.
func (w *Writer) Write(folderPath []string, subject, from, to, cc, messageID string, date time.Time, body string, attachments []Attachment) error {
	return w.WriteWithSource(folderPath, subject, from, to, cc, messageID, date, body, attachments, "indexed")
}

// WriteWithSource appends a message and records which recovery path produced
// it. Write keeps the historical indexed source marker for compatibility.
func (w *Writer) WriteWithSource(folderPath []string, subject, from, to, cc, messageID string, date time.Time, body string, attachments []Attachment, source string) error {
	if len(folderPath) == 0 {
		return fmt.Errorf("empty folder path")
	}
	path, err := w.folderFile(folderPath)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	var msg bytes.Buffer
	if from == "" {
		from = "unknown"
	}
	if date.IsZero() {
		date = time.Unix(0, 0).UTC()
	}
	if _, err := fmt.Fprintf(f, "From %s %s\r\n", from, date.Format("Mon Jan _2 15:04:05 2006")); err != nil {
		return err
	}
	fmt.Fprintf(&msg, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject))
	fmt.Fprintf(&msg, "From: %s\r\n", from)
	if to != "" {
		fmt.Fprintf(&msg, "To: %s\r\n", to)
	}
	if cc != "" {
		fmt.Fprintf(&msg, "Cc: %s\r\n", cc)
	}
	if messageID != "" {
		fmt.Fprintf(&msg, "Message-ID: %s\r\n", messageID)
	}
	fmt.Fprintf(&msg, "Date: %s\r\n", date.Format(time.RFC1123Z))
	if source == "" {
		source = "indexed"
	}
	fmt.Fprintf(&msg, "X-Ostom-Source: %s\r\nMIME-Version: 1.0\r\n", source)

	usable := make([]Attachment, 0, len(attachments))
	for _, attachment := range attachments {
		if w.MaxAttachmentMemory > 0 && int64(len(attachment.Data)) > w.MaxAttachmentMemory {
			w.SkippedAttachments++
			continue
		}
		usable = append(usable, attachment)
	}
	if len(usable) == 0 {
		fmt.Fprint(&msg, "Content-Type: text/plain; charset=utf-8\r\n\r\n")
		writeMboxBody(&msg, body)
	} else {
		var multipartBody bytes.Buffer
		mw := multipart.NewWriter(&multipartBody)
		textHeader := make(textproto.MIMEHeader)
		textHeader.Set("Content-Type", "text/plain; charset=utf-8")
		part, err := mw.CreatePart(textHeader)
		if err != nil {
			return err
		}
		if _, err := part.Write([]byte(body)); err != nil {
			return err
		}
		for _, attachment := range usable {
			h := make(textproto.MIMEHeader)
			contentType := attachment.MimeType
			if contentType == "" {
				contentType = "application/octet-stream"
			}
			h.Set("Content-Type", contentType)
			h.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", attachment.Name))
			part, err := mw.CreatePart(h)
			if err != nil {
				return err
			}
			encoded := make([]byte, base64.StdEncoding.EncodedLen(len(attachment.Data)))
			base64.StdEncoding.Encode(encoded, attachment.Data)
			for i := 0; i < len(encoded); i += 76 {
				end := i + 76
				if end > len(encoded) {
					end = len(encoded)
				}
				part.Write(encoded[i:end])
				part.Write([]byte("\r\n"))
			}
		}
		if err := mw.Close(); err != nil {
			return err
		}
		fmt.Fprintf(&msg, "Content-Type: multipart/mixed; boundary=%q\r\n\r\n", mw.Boundary())
		msg.Write(multipartBody.Bytes())
	}

	if err := writeEscapedMbox(f, msg.Bytes()); err != nil {
		return err
	}
	_, err = f.WriteString("\r\n")
	return err
}

func (w *Writer) folderFile(parts []string) (string, error) {
	file := filepath.Join(w.Root, safeName(parts[0]))
	for _, part := range parts[1:] {
		dir := file + ".sbd"
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		file = filepath.Join(dir, safeName(part))
	}
	return file, nil
}

func writeEscapedMbox(f *os.File, data []byte) error {
	for len(data) > 0 {
		line := data
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			line, data = data[:i+1], data[i+1:]
		} else {
			data = nil
		}
		line = bytes.TrimSuffix(line, []byte("\r\n"))
		line = bytes.TrimSuffix(line, []byte("\n"))
		if bytes.HasPrefix(line, []byte("From ")) {
			if _, err := f.WriteString(">From "); err != nil {
				return err
			}
			line = line[5:]
		}
		if _, err := f.Write(line); err != nil {
			return err
		}
		if _, err := f.WriteString("\r\n"); err != nil {
			return err
		}
	}
	return nil
}

func writeMboxBody(msg *bytes.Buffer, body string) {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	for _, line := range strings.Split(body, "\n") {
		fmt.Fprintf(msg, "%s\r\n", line)
	}
}

func safeName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Unnamed"
	}
	value = strings.Map(func(r rune) rune {
		switch r {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			return '_'
		default:
			return r
		}
	}, value)
	return value
}
