# OST/PST format notes

## OSTOM scope

OSTOM is a practical read-only salvage utility, not a forensic suite. It does not establish chain of custody, promise deleted-item recovery, or bypass password, DPAPI, EFS, BitLocker, or account protections.

The primary recovery target is local-only content. OSTOM must inspect and export every physically present folder and item, including Sent, Drafts, Outbox, Conflicts, hidden, custom, and “This computer only” folders. It must not assume that an IMAP/Exchange server contains the complete copy or that a folder name identifies its synchronization status.

OST and PST use the Microsoft PFF/OFF family. The low-level NDB layer contains:

Primary references:

- [Microsoft [MS-PST] specification](https://learn.microsoft.com/en-us/openspecs/office_file_formats/ms-pst)
- [NDB database structure](https://learn.microsoft.com/en-us/openspecs/office_file_formats/ms-pst/e4efaad0-1876-446e-9d34-bb921588f924)
- [outlook-pst-go indexed reader](https://github.com/grokify/outlook-pst-go)
- [go-pst recovery reader](https://github.com/useti/go-pst)
- [libpff PFF/OFF format notes](https://github.com/libyal/libpff/blob/main/documentation/Personal%20Folder%20File%20(PFF)%20format.asciidoc)
- [libpff reference implementation](https://github.com/libyal/libpff)

```text
Header
├── allocation metadata: AMap, PMap, FMap, FPMap, DList
├── NBT — Node B-tree
└── BBT — Block B-tree
```

The NBT maps a node ID to data and subnode block IDs. The BBT maps block IDs to file offsets, stored byte counts, and reference counts. The roots of both trees are referenced from the file header.

At higher layers, folder hierarchy tables point to child folders and contents tables point to messages. Messages may contain attachment tables and attachment subnodes.

### PFF block encoding

The header `bCryptMethod` describes the PFF block encoding: `0` (`None`), `1` (`Permute`), or `2` (`Cyclic`). These are reversible format encodings, not a password-encryption layer. OSTOM validates trailers against the on-disk bytes, then decodes external data blocks in memory before recognizing XBLOCK/heap payloads. Unknown methods are reported as unsupported rather than guessed.

## Diagnostic interpretation

- Header failure: the file may be truncated, not a PFF file, or have a damaged header.
- Index access failure: NBT/BBT roots or referenced pages cannot be read.
- Folder-tree failure: the root is readable, but hierarchy tables or child nodes are damaged.
- Message failure: a folder is readable, but one or more message nodes or content rows fail.
- Attachment failure: an attachment table, attachment node, or attachment data stream fails.

OSTOM must report these levels separately. Opening the root does not prove that every message and attachment is intact.

## What the current structural preflight proves

The current Go implementation performs a bounded, read-only preflight before the high-level message walk. It reports the detected PFF variant, declared versus actual file size, header CRC where the layout is known, NBT/BBT root offsets, allocation-page checks, root-page type/shape, branch-page child-reference bounds, and a guarded traversal of root-connected NBT/BBT pages. For Unicode BBT leaves it also extracts `IB`/`CB` block references and checks their physical ranges, but it does not yet decode every block or decompress every 4 KiB data block. A CRC mismatch from a third-party corpus is therefore retained as evidence, but is not alone promoted to a definitive corruption verdict until the algorithm and page variant are independently confirmed.

## Important OST variant: 4 KiB pages and compressed blocks

The ordinary 512-byte-page and Unicode PFF layouts are not the whole OST landscape. Independent format research and the libpff implementation document an Outlook 2013-era 64-bit OST variant with 4096-byte pages. Its data blocks can contain raw DEFLATE/RFC 1951 payloads; the size in the offset index is then the compressed size, while the block footer carries the uncompressed size. The same research describes distinct 4 KiB B-tree page and block footers.

This matters operationally: a parser that recognizes the `SO` OST signature but only understands the 512-byte/ordinary Unicode layout may produce a false “corrupt” diagnosis. OSTOM must therefore report the detected format variant and distinguish “unsupported variant” from “confirmed damaged.” The current Go reader path is not yet proven against a genuine compressed 4 KiB OST; the synthetic `.ost` fixture is only a parser-validated PFF sample and does not exercise this variant.

`libpff` is the strongest recovery reference found so far: it explicitly lists 64-bit 4 KiB compressed OST support, item recovery, corruption tolerance, and test profiles. It is licensed LGPL-3.0-or-later, so it should be an optional external backend or a separately reviewed component, not copied into the MIT core blindly. OSTOM should use it as a behavioural oracle and fallback candidate while keeping the Go-native path independently tested.

## Recovery strategy

1. Validate the header and page/block metadata.
2. Walk the indexed NBT/BBT structure.
3. Walk the folder hierarchy.
4. Read message tables and message nodes.
5. Read attachment tables and data streams.
6. Optionally scan for orphaned nodes when the indexed tree is incomplete.

The indexed scan and orphan recovery must be separate results. Recovered objects must never be silently mixed with normally indexed objects.

These references define the byte-level format. Library behaviour is a separate implementation fact: OSTOM treats library errors, panics, missing tables, and materialized payloads as backend limitations and records them in tests rather than treating them as format rules.

Primary references: [MS-PST NDB layer](https://learn.microsoft.com/en-us/openspecs/office_file_formats/ms-pst/e4efaad0-1876-446e-9d34-bb921588f924), [BBT leaf entry](https://learn.microsoft.com/en-us/openspecs/office_file_formats/ms-pst/53a4b926-8ac4-45c9-9c6d-8358d951dbcd), and [NBT leaf entry](https://learn.microsoft.com/en-us/openspecs/office_file_formats/ms-pst/28fb2116-0998-4485-9844-9711b95603ba).

The payload pre-scan currently reads unique NBT-referenced blocks under a hard limit, classifies their first byte as external data, extended block, or subnode block, performs bounded advisory trailer/signature/CRC checks, parses bounded XBLOCK/SLBLOCK headers, and follows newly discovered BIDs through a bounded work queue. It reports internal references, subnode entry counts, logical sizes, and unreadable discovered blocks, but does not yet assemble a complete logical stream or reconstruct message/attachment streams.

Trailer checks are now reported as advisory diagnostics. The bundled corpus can be readable by the high-level parser while exposing zeroed or layout-incompatible trailer bytes at the BBT-derived location; this is evidence for further format investigation, not an automatic corruption verdict.
