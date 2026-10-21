// Package mbox writes Thunderbird-compatible mbox files.
package mbox

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Attachment struct {
	Name      string
	MimeType  string
	ContentID string
	Inline    bool
	Data      []byte
	// Open supplies the payload on demand. When present, the exporter copies
	// it directly through the MIME encoder instead of making an encoded copy.
	Open func() (io.ReadCloser, error)
	Size int64
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

	if from == "" {
		from = "unknown"
	}
	if date.IsZero() {
		date = time.Unix(0, 0).UTC()
	}
	if _, err := fmt.Fprintf(f, "From %s %s\r\n", from, date.Format("Mon Jan _2 15:04:05 2006")); err != nil {
		return err
	}
	if err := writeMessageTo(newMboxLineWriter(f), w, subject, from, to, cc, messageID, date, body, attachments, source); err != nil {
		return err
	}
	_, err = f.WriteString("\r\n")
	return err
}

// RenderWithSource creates a raw RFC 5322 message. It is shared by mbox and
// per-message EML export so both recovery outputs carry identical MIME data.
func (w *Writer) RenderWithSource(subject, from, to, cc, messageID string, date time.Time, body string, attachments []Attachment, source string) ([]byte, error) {
	var msg bytes.Buffer
	if err := writeMessageTo(&msg, w, subject, from, to, cc, messageID, date, body, attachments, source); err != nil {
		return nil, err
	}
	return msg.Bytes(), nil
}

// writeMessageTo writes one RFC 5322 message without first building the whole
// MIME document in memory. This matters for large attachments: the source
// reader is copied through the MIME encoder into the destination in bounded
// chunks.
func writeMessageTo(dst io.Writer, w *Writer, subject, from, to, cc, messageID string, date time.Time, body string, attachments []Attachment, source string) error {
	if from == "" {
		from = "unknown"
	}
	if date.IsZero() {
		date = time.Unix(0, 0).UTC()
	}
	fmt.Fprintf(dst, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject))
	fmt.Fprintf(dst, "From: %s\r\n", from)
	if to != "" {
		fmt.Fprintf(dst, "To: %s\r\n", to)
	}
	if cc != "" {
		fmt.Fprintf(dst, "Cc: %s\r\n", cc)
	}
	if messageID != "" {
		fmt.Fprintf(dst, "Message-ID: %s\r\n", messageID)
	}
	fmt.Fprintf(dst, "Date: %s\r\n", date.Format(time.RFC1123Z))
	if source == "" {
		source = "indexed"
	}
	fmt.Fprintf(dst, "X-Ostom-Source: %s\r\nMIME-Version: 1.0\r\n", source)

	usable := make([]Attachment, 0, len(attachments))
	for _, attachment := range attachments {
		size := attachment.Size
		if size == 0 {
			size = int64(len(attachment.Data))
		}
		if w.MaxAttachmentMemory > 0 && size > w.MaxAttachmentMemory {
			w.SkippedAttachments++
			continue
		}
		usable = append(usable, attachment)
	}
	if len(usable) == 0 {
		fmt.Fprint(dst, "Content-Type: text/plain; charset=utf-8\r\n\r\n")
		writeMboxBody(dst, body)
	} else {
		mw := multipart.NewWriter(dst)
		if _, err := fmt.Fprintf(dst, "Content-Type: multipart/mixed; boundary=%q\r\n\r\n", mw.Boundary()); err != nil {
			return err
		}
		textHeader := make(textproto.MIMEHeader)
		textHeader.Set("Content-Type", "text/plain; charset=utf-8")
		part, err := mw.CreatePart(textHeader)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(part, body); err != nil {
			return err
		}
		for _, attachment := range usable {
			h := make(textproto.MIMEHeader)
			contentType := attachment.MimeType
			if contentType == "" {
				contentType = "application/octet-stream"
			}
			h.Set("Content-Type", contentType)
			if attachment.Inline || attachment.ContentID != "" {
				h.Set("Content-ID", "<"+strings.Trim(attachment.ContentID, "<>")+">")
				h.Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", attachment.Name))
			} else {
				h.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", attachment.Name))
			}
			part, err := mw.CreatePart(h)
			if err != nil {
				return err
			}
			if err := writeBase64(part, attachment); err != nil {
				return fmt.Errorf("encode attachment %q: %w", attachment.Name, err)
			}
		}
		if err := mw.Close(); err != nil {
			return err
		}
	}
	return nil
}

// writeBase64 streams an attachment through the MIME encoder. Data remains a
// compatibility fallback for recovered candidates that are already bounded
// in memory; new backends can provide Open and avoid loading the payload.
func writeBase64(dst io.Writer, attachment Attachment) error {
	reader := io.Reader(bytes.NewReader(attachment.Data))
	var closer io.Closer
	if attachment.Open != nil {
		opened, err := attachment.Open()
		if err != nil {
			return err
		}
		reader = opened
		closer = opened
	}
	if closer != nil {
		defer closer.Close()
	}
	wrapped := &base64LineWriter{dst: dst}
	encoder := base64.NewEncoder(base64.StdEncoding, wrapped)
	if _, err := io.Copy(encoder, reader); err != nil {
		_ = encoder.Close()
		return err
	}
	if err := encoder.Close(); err != nil {
		return err
	}
	return wrapped.flush()
}

type base64LineWriter struct {
	dst  io.Writer
	line []byte
}

func (w *base64LineWriter) Write(data []byte) (int, error) {
	written := 0
	for len(data) > 0 {
		space := 76 - len(w.line)
		if space > len(data) {
			space = len(data)
		}
		w.line = append(w.line, data[:space]...)
		data = data[space:]
		written += space
		if len(w.line) == 76 {
			if err := w.flush(); err != nil {
				return written, err
			}
		}
	}
	return written, nil
}

func (w *base64LineWriter) flush() error {
	if len(w.line) == 0 {
		return nil
	}
	if _, err := w.dst.Write(w.line); err != nil {
		return err
	}
	if _, err := io.WriteString(w.dst, "\r\n"); err != nil {
		return err
	}
	w.line = w.line[:0]
	return nil
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

func writeMboxBody(msg io.Writer, body string) {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	for _, line := range strings.Split(body, "\n") {
		fmt.Fprintf(msg, "%s\r\n", line)
	}
}

type mboxLineWriter struct {
	w           io.Writer
	atLineStart bool
}

func newMboxLineWriter(w io.Writer) *mboxLineWriter {
	return &mboxLineWriter{w: w, atLineStart: true}
}

func (w *mboxLineWriter) Write(p []byte) (int, error) {
	for i, b := range p {
		if w.atLineStart && b == 'F' && len(p)-i >= 5 && string(p[i:i+5]) == "From " {
			if _, err := io.WriteString(w.w, ">"); err != nil {
				return i, err
			}
		}
		if _, err := w.w.Write([]byte{b}); err != nil {
			return i, err
		}
		w.atLineStart = b == '\n'
	}
	return len(p), nil
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
