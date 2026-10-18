// Package gopst exports indexed go-pst messages to Thunderbird mbox files.
// Attachment payloads are copied through Attachment.WriteTo and are never
// accumulated as one []byte.
package gopst

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"time"

	pst "github.com/useti/go-pst/v6/pkg"
	"github.com/useti/go-pst/v6/pkg/properties"
)

type Report struct {
	Directory         string          `json:"directory"`
	RecoveryMode      bool            `json:"recovery_mode,omitempty"`
	Folders           int             `json:"folders"`
	FoldersWithMail   int             `json:"folders_with_mail"`
	EmptyFolders      int             `json:"empty_folders"`
	Messages          int             `json:"messages"`
	MessagesAttempted int             `json:"messages_attempted"`
	MessagesExpected  int             `json:"messages_expected,omitempty"`
	MessagesPartial   int             `json:"messages_partial"`
	Attachments       int             `json:"attachments"`
	AttachmentsFailed int             `json:"attachments_failed"`
	EMLWritten        int             `json:"eml_written"`
	EMLFailed         int             `json:"eml_failed"`
	SkippedMessages   int             `json:"skipped_messages"`
	MessageStatus     []MessageStatus `json:"message_status,omitempty"`
	StatusOmitted     int             `json:"message_status_omitted,omitempty"`
	Errors            []string        `json:"errors,omitempty"`
}

// MessageStatus is a bounded per-message export result. Aggregate counters
// remain complete while the JSON report keeps only a finite status sample.
type MessageStatus struct {
	Index       int    `json:"index"`
	Folder      string `json:"folder"`
	Subject     string `json:"subject,omitempty"`
	Outcome     string `json:"outcome"` // exported, partial, failed
	Attachments int    `json:"attachments,omitempty"`
	Error       string `json:"error,omitempty"`
}

const messageStatusLimit = 4096

func Export(filename, outputDir string) (Report, error) {
	return ExportWithHeartbeat(filename, outputDir, nil)
}

// ExportWithHeartbeat exports readable indexed messages and periodically calls
// heartbeat while traversing folders and messages. The callback must be cheap;
// it is used by the supervisor to distinguish a slow export from a stuck one.
func ExportWithHeartbeat(filename, outputDir string, heartbeat func()) (Report, error) {
	return ExportWithProgress(filename, outputDir, heartbeat, 0, false, nil)
}

// ExportWithRecoveryMode enables go-pst's opt-in lenient parser. It is kept
// explicit because recovery mode may accept structurally incomplete nodes and
// must be visible in the resulting report.
func ExportWithRecoveryMode(filename, outputDir string, heartbeat func()) (Report, error) {
	return ExportWithProgress(filename, outputDir, heartbeat, 0, true, nil)
}

// ExportWithProgress is the bounded export entry point. expectedMessages is a
// declared total from the inspection pass; zero means unknown. progress is
// called after each attempted message.
func ExportWithProgress(filename, outputDir string, heartbeat func(), expectedMessages int, recoveryMode bool, progress func(done, total int)) (Report, error) {
	return exportWithHeartbeatMode(filename, outputDir, heartbeat, expectedMessages, recoveryMode, progress)
}

// ExportRecoveredMessages writes messages found by go-pst's all-node recovery
// iterator into a separate mbox. It is intentionally separate from the normal
// folder export because recovered items may be duplicates and their original
// folder path may be unavailable.
func ExportRecoveredMessages(filename, outputDir string, heartbeat func()) (Report, error) {
	return ExportRecoveredMessagesWithProgress(filename, outputDir, heartbeat, nil)
}

// ExportRecoveredMessagesWithProgress exports all-node recovery messages and
// reports progress against the iterator's known node count.
func ExportRecoveredMessagesWithProgress(filename, outputDir string, heartbeat func(), progress func(done, total int)) (Report, error) {
	r := Report{Directory: outputDir, RecoveryMode: true}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return r, err
	}
	f, err := os.Open(filename)
	if err != nil {
		return r, err
	}
	defer f.Close()
	db, err := pst.NewWithRecoveryMode(f)
	if err != nil {
		return r, err
	}
	defer db.Cleanup()
	iter, err := db.GetRecoverableMessagesIterator()
	if err != nil {
		return r, err
	}
	r.MessagesExpected = iter.Size()
	path := filepath.Join(outputDir, "Local Folders", "OSTOM Recovered")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return r, err
	}
	out, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return r, err
	}
	defer out.Close()
	for {
		callHeartbeat(heartbeat)
		ok, panicText := safeRecoveryNext(iter.Next)
		if panicText != "" {
			r.Errors = append(r.Errors, "recovery iterator panic: "+panicText)
			break
		}
		if !ok {
			break
		}
		if iter.CurrentItemType() != pst.RecoveredItemTypeMessage {
			continue
		}
		r.MessagesAttempted++
		if progress != nil {
			progress(iter.TriedNodes, iter.Size())
		}
		message := iter.Value()
		source := fmt.Sprintf("recovered-node-0x%X", message.Identifier)
		written, partial, subject, writeErr := writeMessageWithSource(out, message, &r, source)
		status := MessageStatus{Index: r.MessagesAttempted, Folder: "OSTOM Recovered", Subject: subject, Attachments: written}
		if writeErr != nil {
			status.Outcome = "failed"
			status.Error = writeErr.Error()
			r.SkippedMessages++
		} else if partial {
			status.Outcome = "partial"
			r.MessagesPartial++
		} else {
			status.Outcome = "exported"
		}
		if writeErr == nil {
			r.Messages++
		}
		recordMessageStatus(&r, status)
	}
	if len(iter.ErrorMessages) > 0 {
		r.Errors = append(r.Errors, iter.ErrorMessages...)
	}
	return r, nil
}

func safeRecoveryNext(next func() bool) (ok bool, panicText string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			panicText = fmt.Sprint(recovered)
			ok = false
		}
	}()
	return next(), ""
}

func exportWithHeartbeatMode(filename, outputDir string, heartbeat func(), expectedMessages int, recoveryMode bool, progress func(done, total int)) (Report, error) {
	r := Report{Directory: outputDir, RecoveryMode: recoveryMode, MessagesExpected: expectedMessages}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return r, err
	}
	f, err := os.Open(filename)
	if err != nil {
		return r, err
	}
	defer f.Close()
	var db *pst.File
	if recoveryMode {
		db, err = pst.NewWithRecoveryMode(f)
	} else {
		db, err = pst.New(f)
	}
	if err != nil {
		return r, err
	}
	defer db.Cleanup()
	root, err := db.GetRootFolder()
	if err != nil {
		return r, err
	}
	if err := exportChildren(root, filepath.Join(outputDir, "Local Folders"), filepath.Join(outputDir, "OSTOM EML"), nil, &r, heartbeat, progress); err != nil {
		return r, err
	}
	return r, nil
}

func exportChildren(parent pst.Folder, root, emlRoot string, parentPath []string, r *Report, heartbeat func(), progress func(done, total int)) error {
	children, err := parent.GetSubFolders()
	if err != nil {
		return fmt.Errorf("read subfolders of %q: %w", parent.Name, err)
	}
	for _, folder := range children {
		callHeartbeat(heartbeat)
		path := append(append([]string{}, parentPath...), folder.Name)
		r.Folders++
		if err := exportFolder(folder, root, emlRoot, path, r, heartbeat, progress); err != nil {
			r.Errors = append(r.Errors, err.Error())
		}
		if err := exportChildren(folder, root, emlRoot, path, r, heartbeat, progress); err != nil {
			r.Errors = append(r.Errors, err.Error())
		}
	}
	return nil
}

func exportFolder(folder pst.Folder, root, emlRoot string, path []string, r *Report, heartbeat func(), progress func(done, total int)) error {
	filePath, err := mboxPath(root, path)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open mbox %q: %w", filePath, err)
	}
	defer f.Close()

	iter, err := folder.GetMessageIterator()
	if err != nil {
		return fmt.Errorf("read messages in %q: %w", strings.Join(path, "\\"), err)
	}
	attemptsBefore := r.MessagesAttempted
	for iter.Next() {
		callHeartbeat(heartbeat)
		r.MessagesAttempted++
		if progress != nil {
			progress(r.MessagesAttempted, r.MessagesExpected)
		}
		messageIndex := r.MessagesAttempted
		written, partial, subject, err := writeMessageWithEML(f, emlRoot, path, messageIndex, iter.Value(), r)
		status := MessageStatus{Index: messageIndex, Folder: strings.Join(path, "\\"), Subject: subject, Attachments: written}
		if err != nil {
			var emlErr *emlWriteError
			if errors.As(err, &emlErr) {
				// The mbox was completed before finalizing the fallback file.
				// Preserve that independently readable message as partial rather
				// than calling it skipped.
				status.Outcome = "partial"
				status.Error = err.Error()
				r.MessagesPartial++
				r.Messages++
				r.EMLFailed++
				r.Errors = append(r.Errors, fmt.Sprintf("EML fallback in %q: %v", strings.Join(path, "\\"), err))
			} else {
				status.Outcome = "failed"
				status.Error = err.Error()
				r.SkippedMessages++
				r.Errors = append(r.Errors, fmt.Sprintf("message in %q: %v", strings.Join(path, "\\"), err))
			}
		} else if partial {
			status.Outcome = "partial"
			r.MessagesPartial++
			r.EMLWritten++
		} else {
			status.Outcome = "exported"
			r.EMLWritten++
		}
		if err == nil {
			r.Messages++
		}
		recordMessageStatus(r, status)
	}
	if err := iter.Err(); err != nil {
		r.Errors = append(r.Errors, fmt.Sprintf("message iterator in %q: %v", strings.Join(path, "\\"), err))
	}
	if r.MessagesAttempted == attemptsBefore {
		r.EmptyFolders++
	} else {
		r.FoldersWithMail++
	}
	return nil
}

type emlWriteError struct{ err error }

func (e *emlWriteError) Error() string { return "write EML fallback: " + e.err.Error() }
func (e *emlWriteError) Unwrap() error { return e.err }

func recordMessageStatus(r *Report, status MessageStatus) {
	if len(r.MessageStatus) < messageStatusLimit {
		r.MessageStatus = append(r.MessageStatus, status)
	} else {
		r.StatusOmitted++
	}
}

func callHeartbeat(heartbeat func()) {
	if heartbeat != nil {
		heartbeat()
	}
}

func writeMessage(out io.Writer, message *pst.Message, r *Report) (int, bool, string, error) {
	return writeMessageWithSource(out, message, r, "indexed")
}

// writeMessageWithEML streams one RFC 5322 message to both its mbox and a
// per-message EML file. The EML is written through a temporary sibling and
// renamed only after the full message is readable, so a failed attachment
// stream cannot be mistaken for a complete fallback message.
func writeMessageWithEML(mboxOut io.Writer, emlRoot string, folderPath []string, index int, message *pst.Message, r *Report) (int, bool, string, error) {
	if len(folderPath) == 0 {
		return 0, false, "", fmt.Errorf("empty folder path for EML export")
	}
	dir := emlRoot
	for _, part := range folderPath {
		dir = filepath.Join(dir, safeName(part))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, false, "", fmt.Errorf("create EML directory: %w", err)
	}
	name := fmt.Sprintf("%08d-%016X.eml", index, message.Identifier)
	finalPath := filepath.Join(dir, name)
	temporary, err := os.CreateTemp(dir, ".ostom-eml-*")
	if err != nil {
		return 0, false, "", fmt.Errorf("create EML file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := writeMboxSeparator(mboxOut, message); err != nil {
		temporary.Close()
		return 0, false, "", err
	}
	written, partial, subject, err := writeMessageContent(io.MultiWriter(newMboxLineWriter(mboxOut), temporary), message, r, "indexed")
	if err == nil {
		_, err = io.WriteString(mboxOut, "\r\n")
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return written, partial, subject, err
	}
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return written, partial, subject, &emlWriteError{err: fmt.Errorf("finalize EML %q: %w", finalPath, err)}
	}
	return written, partial, subject, nil
}

func writeMessageWithSource(out io.Writer, message *pst.Message, r *Report, source string) (int, bool, string, error) {
	if err := writeMboxSeparator(out, message); err != nil {
		return 0, false, "", err
	}
	written, partial, subject, err := writeMessageContent(newMboxLineWriter(out), message, r, source)
	if err != nil {
		return written, partial, subject, err
	}
	if _, err := io.WriteString(out, "\r\n"); err != nil {
		return written, partial, subject, err
	}
	return written, partial, subject, nil
}

func writeMboxSeparator(out io.Writer, message *pst.Message) error {
	props, ok := message.Properties.(*properties.Message)
	if !ok {
		return fmt.Errorf("unsupported message class %T", message.Properties)
	}
	from := props.GetSenderName()
	if email := props.GetSenderEmailAddress(); email != "" && email != from {
		from += " <" + email + ">"
	}
	if from == "" {
		from = "unknown"
	}
	date := time.Unix(0, props.GetClientSubmitTime()).UTC()
	if _, err := fmt.Fprintf(out, "From %s %s\r\n", from, date.Format("Mon Jan _2 15:04:05 2006")); err != nil {
		return err
	}
	return nil
}

// writeMessageContent writes only the RFC 5322 payload. Callers choose
// whether it is sent to an mbox-escaping writer, an EML file, or both.
func writeMessageContent(lw io.Writer, message *pst.Message, r *Report, source string) (int, bool, string, error) {
	props, ok := message.Properties.(*properties.Message)
	if !ok {
		return 0, false, "", fmt.Errorf("unsupported message class %T", message.Properties)
	}
	subject := props.GetSubject()
	from := props.GetSenderName()
	if email := props.GetSenderEmailAddress(); email != "" && email != from {
		from += " <" + email + ">"
	}
	if from == "" {
		from = "unknown"
	}
	date := time.Unix(0, props.GetClientSubmitTime()).UTC()
	fmt.Fprintf(lw, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", props.GetSubject()))
	fmt.Fprintf(lw, "From: %s\r\n", from)
	fmt.Fprintf(lw, "Date: %s\r\n", date.Format(time.RFC1123Z))
	if props.GetDisplayTo() != "" {
		fmt.Fprintf(lw, "To: %s\r\n", props.GetDisplayTo())
	}
	if props.GetDisplayCc() != "" {
		fmt.Fprintf(lw, "Cc: %s\r\n", props.GetDisplayCc())
	}
	if props.GetInternetMessageId() != "" {
		fmt.Fprintf(lw, "Message-ID: %s\r\n", props.GetInternetMessageId())
	}
	fmt.Fprintf(lw, "X-Ostom-Source: %s\r\nMIME-Version: 1.0\r\n", source)

	attachments, err := message.GetAttachmentIterator()
	if err != nil {
		if !strings.Contains(err.Error(), "attachments not found") && !strings.Contains(err.Error(), "message has no attachments") {
			r.Errors = append(r.Errors, "attachment table unavailable: "+err.Error())
		}
		fmt.Fprint(lw, "Content-Type: text/plain; charset=utf-8\r\n\r\n")
		writeBody(lw, props.GetBody())
		fmt.Fprint(lw, "\r\n")
		return 0, !strings.Contains(err.Error(), "attachments not found") && !strings.Contains(err.Error(), "message has no attachments"), subject, nil
	}
	// We need a multipart writer over the mbox line writer. All attachment
	// payload bytes are copied through base64.NewEncoder and Attachment.WriteTo.
	mw := multipart.NewWriter(lw)
	textHeader := make(textproto.MIMEHeader)
	textHeader.Set("Content-Type", "text/plain; charset=utf-8")
	part, err := mw.CreatePart(textHeader)
	if err != nil {
		return 0, false, subject, err
	}
	if _, err := io.WriteString(part, props.GetBody()); err != nil {
		return 0, false, subject, err
	}
	written := 0
	for attachments.Next() {
		attachment := attachments.Value()
		name := attachment.GetAttachLongFilename()
		if name == "" {
			name = attachment.GetAttachFilename()
		}
		contentType := attachment.GetAttachMimeTag()
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		h := make(textproto.MIMEHeader)
		h.Set("Content-Type", contentType)
		if cid := attachment.GetAttachContentId(); cid != "" {
			h.Set("Content-ID", "<"+strings.Trim(cid, "<>")+">")
			h.Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", name))
		} else {
			h.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
		}
		h.Set("Content-Transfer-Encoding", "base64")
		part, err := mw.CreatePart(h)
		if err != nil {
			return written, false, subject, err
		}
		encoder := base64.NewEncoder(base64.StdEncoding, &foldingWriter{w: part})
		if _, err := attachment.WriteTo(encoder); err != nil {
			encoder.Close()
			r.AttachmentsFailed++
			return written, false, subject, fmt.Errorf("read attachment %q: %w", name, err)
		}
		if err := encoder.Close(); err != nil {
			r.AttachmentsFailed++
			return written, false, subject, err
		}
		r.Attachments++
		written++
	}
	partial := false
	if err := attachments.Err(); err != nil {
		// Keep the message: a damaged attachment table must not discard a
		// readable message body already written to the mbox.
		r.Errors = append(r.Errors, "attachment iterator: "+err.Error())
		partial = true
	}
	if err := mw.Close(); err != nil {
		return written, partial, subject, err
	}
	fmt.Fprint(lw, "\r\n")
	return written, partial, subject, nil
}

func writeBody(w io.Writer, body string) {
	body = strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\r", "\n")
	for _, line := range strings.Split(body, "\n") {
		fmt.Fprintf(w, "%s\r\n", line)
	}
}

func mboxPath(root string, path []string) (string, error) {
	if len(path) == 0 {
		return "", fmt.Errorf("empty folder path")
	}
	file := filepath.Join(root, safeName(path[0]))
	for _, part := range path[1:] {
		dir := file + ".sbd"
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		file = filepath.Join(dir, safeName(part))
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return "", err
	}
	return file, nil
}

func safeName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Unnamed"
	}
	return strings.Map(func(r rune) rune {
		switch r {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			return '_'
		default:
			return r
		}
	}, value)
}

type mboxLineWriter struct {
	w           io.Writer
	atLineStart bool
}

func newMboxLineWriter(w io.Writer) *mboxLineWriter { return &mboxLineWriter{w: w, atLineStart: true} }

func (w *mboxLineWriter) Write(p []byte) (int, error) {
	for i, b := range p {
		if w.atLineStart && b == 'F' && bytesHasFromPrefix(p[i:]) {
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

func bytesHasFromPrefix(p []byte) bool {
	return len(p) >= 5 && string(p[:5]) == "From "
}

type foldingWriter struct {
	w    io.Writer
	line int
}

func (w *foldingWriter) Write(p []byte) (int, error) {
	original := len(p)
	for len(p) > 0 {
		n := 57
		if len(p) < n {
			n = len(p)
		}
		if w.line > 0 {
			if _, err := io.WriteString(w.w, "\r\n"); err != nil {
				return 0, err
			}
		}
		if _, err := w.w.Write(p[:n]); err != nil {
			return 0, err
		}
		w.line++
		p = p[n:]
	}
	return original, nil
}
