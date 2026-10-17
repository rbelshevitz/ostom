// Package backend defines the small contract between an OST/PST reader and
// ostom's reporting/recovery pipeline.
//
// A backend must not require callers to load a whole message or attachment
// into memory. This is important for multi-gigabyte OST files on machines
// with only a few gigabytes of RAM.
package backend

import (
	"context"
	"io"
)

// Store is one physical Outlook store. It may be an OST, PST, or another
// store discovered in an Outlook profile. Store identity is deliberately
// separate from folder names: a folder called "Sent" is not evidence that it
// belongs to an IMAP/server store.
type Store interface {
	Info() StoreInfo
	Close() error
	WalkFolders(context.Context, func(Folder) error) error
}

type StoreInfo struct {
	Path       string
	Kind       string // ost, pst, unknown
	ReadOnly   bool
	ProviderID string
}

// Folder represents a physical folder and retains its parent relationship.
// The exporter uses the path only for presentation; FolderID/ParentID are the
// stable identity used to preserve hierarchy and avoid name-based guesses.
type Folder interface {
	Info() FolderInfo
	WalkMessages(context.Context, func(Message) error) error
}

type FolderInfo struct {
	ID          string
	ParentID    string
	DisplayName string
	Path        string
	FolderType  string
	Hidden      bool
	Role        string // evidence only: sent, drafts, outbox, etc.
}

type Message interface {
	Info() MessageInfo
	OpenRFC5322(context.Context) (io.ReadCloser, error)
	WalkAttachments(context.Context, func(Attachment) error) error
}

type MessageInfo struct {
	ID           string
	Subject      string
	Sender       string
	DateUnix     int64
	Unread       bool
	SizeDeclared int64
	Recovered    bool
}

// Attachment exposes metadata separately from bytes. A quick scan can count
// declared sizes without touching payloads; a recovery/export scan can stream
// the payload in bounded chunks.
type Attachment interface {
	Info() AttachmentInfo
	Open(context.Context) (io.ReadCloser, error)
}

type AttachmentInfo struct {
	ID           string
	Name         string
	ContentType  string
	DeclaredSize int64
	Inline       bool
}
