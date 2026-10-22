package mbox

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteBase64UsesStreamingAttachment(t *testing.T) {
	var opened bool
	attachment := Attachment{
		Name: "stream.bin",
		Open: func() (io.ReadCloser, error) {
			opened = true
			return io.NopCloser(strings.NewReader(strings.Repeat("payload-", 20))), nil
		},
		Size: int64(len(strings.Repeat("payload-", 20))),
	}
	var out strings.Builder
	if err := writeBase64(&out, attachment); err != nil {
		t.Fatal(err)
	}
	if !opened {
		t.Fatal("streaming attachment opener was not called")
	}
	for _, line := range strings.Split(strings.TrimSuffix(out.String(), "\r\n"), "\r\n") {
		if len(line) > 76 {
			t.Fatalf("base64 line length = %d, want <= 76", len(line))
		}
	}
	if !strings.Contains(out.String(), "cGF5bG9hZC1") {
		t.Fatalf("streaming payload was not base64 encoded: %q", out.String())
	}
}

func TestWriteBase64FallsBackToData(t *testing.T) {
	var out strings.Builder
	if err := writeBase64(&out, Attachment{Data: []byte("abc")}); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "YWJj\r\n"; got != want {
		t.Fatalf("encoded data = %q, want %q", got, want)
	}
}

func TestRenderKeepsAttachmentsBoundToOneMessage(t *testing.T) {
	writer, err := New(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := writer.RenderWithSource(
		"Письмо с вложениями", "sender@example.test", "", "", "<message@example.test>",
		zeroTime(), "body", []Attachment{
			{Name: "unicode-имя.txt", MimeType: "text/plain; charset=utf-8", Data: []byte("Привет")},
			{Name: "inline.bin", MimeType: "application/octet-stream", ContentID: "inline-1", Inline: true, Data: []byte{1, 2, 3}},
		}, "native-recovered",
	)
	if err != nil {
		t.Fatal(err)
	}
	message, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	mediaType, params, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/mixed" {
		t.Fatalf("content type = %q params=%v err=%v", mediaType, params, err)
	}
	reader := multipart.NewReader(message.Body, params["boundary"])
	var names []string
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.ReadAll(part); err != nil {
			t.Fatal(err)
		}
		if name := part.FileName(); name != "" {
			names = append(names, name)
		}
	}
	if len(names) != 2 || names[0] != "unicode-имя.txt" || names[1] != "inline.bin" {
		t.Fatalf("MIME attachment names = %v", names)
	}
}

func TestWriteWithSourceStreamsAttachmentToMbox(t *testing.T) {
	root := t.TempDir()
	writer, err := New(root, 0)
	if err != nil {
		t.Fatal(err)
	}
	opened := false
	attachment := Attachment{
		Name: "large.bin",
		Open: func() (io.ReadCloser, error) {
			opened = true
			return io.NopCloser(strings.NewReader(strings.Repeat("payload-", 4096))), nil
		},
		Size: 8 * 4096,
	}
	if err := writer.WriteWithSource([]string{"Inbox"}, "subject", "sender@example.test", "", "", "", zeroTime(), "body", []Attachment{attachment}, "native-recovered"); err != nil {
		t.Fatal(err)
	}
	if !opened {
		t.Fatal("streaming attachment opener was not called")
	}
	data, err := os.ReadFile(filepath.Join(root, "Inbox"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("Content-Type: multipart/mixed")) || !bytes.Contains(data, []byte("cGF5bG9hZC0")) {
		t.Fatalf("streamed mbox message is incomplete: %q", data[:min(len(data), 256)])
	}
}

func TestLongAttachmentHeavyMboxPipeline(t *testing.T) {
	root := t.TempDir()
	writer, err := New(root, 0)
	if err != nil {
		t.Fatal(err)
	}
	const count = 256
	payload := strings.Repeat("long-pipeline-payload-", 4096)
	opened := 0
	for index := 0; index < count; index++ {
		messageIndex := index
		attachment := Attachment{
			Name: "long-pipeline.bin", MimeType: "application/octet-stream", Size: int64(len(payload)),
			Open: func() (io.ReadCloser, error) {
				opened++
				return io.NopCloser(strings.NewReader(payload)), nil
			},
		}
		if err := writer.WriteWithSource([]string{"Inbox"}, "long pipeline", "sender@example.test", "", "", fmt.Sprintf("<long-%d@example.test>", messageIndex), zeroTime(), "body", []Attachment{attachment}, "native-recovered"); err != nil {
			t.Fatalf("write message %d: %v", index, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(root, "Inbox"))
	if err != nil {
		t.Fatal(err)
	}
	if opened != count {
		t.Fatalf("lazy attachment opens = %d, want %d", opened, count)
	}
	if got := bytes.Count(data, []byte("Message-ID: <long-")); got != count {
		t.Fatalf("mbox messages = %d, want %d", got, count)
	}
	if got := bytes.Count(data, []byte("filename=\"long-pipeline.bin\"")); got != count {
		t.Fatalf("MIME attachments = %d, want %d", got, count)
	}
}

func zeroTime() time.Time { return time.Time{} }
