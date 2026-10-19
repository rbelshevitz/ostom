package mbox

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
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

func zeroTime() time.Time { return time.Time{} }
