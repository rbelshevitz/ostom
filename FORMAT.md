# OST/PST format notes

## OSTOM scope

OSTOM is a practical read-only salvage utility, not a forensic suite. It does not establish chain of custody, promise deleted-item recovery, or bypass password, DPAPI, EFS, BitLocker, or account protections.

The primary recovery target is local-only content. OSTOM must inspect and export every physically present folder and item, including Sent, Drafts, Outbox, Conflicts, hidden, custom, and “This computer only” folders. It must not assume that an IMAP/Exchange server contains the complete copy or that a folder name identifies its synchronization status.

OST and PST use the Microsoft PFF/OFF family. The low-level NDB layer contains:

- the NDB header and allocation metadata;
- the NBT (Node B-tree), which maps node IDs to data/subnode BIDs;
- the BBT (Block B-tree), which maps BIDs to physical block locations;
- data and subnode blocks containing the payload referenced by those indexes.

Primary references:

- [Microsoft [MS-PST] specification](https://learn.microsoft.com/en-us/openspecs/office_file_formats/ms-pst)
- [NDB database structure](https://learn.microsoft.com/en-us/openspecs/office_file_formats/ms-pst/e4efaad0-1876-446e-9d34-bb921588f924)
- [FileFormat.com OST overview](https://docs.fileformat.com/email/ost/) (secondary summary; verify byte-level claims against the official MS-PST specification)
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

NBT and BBT are indexes inside the NDB layer, not the complete mailbox database
and not the message contents themselves. The actual folder, message, property,
table, and attachment structures begin in the payload of the referenced node
blocks and are decoded by higher layers.

At higher layers, folder hierarchy tables point to child folders and contents tables point to messages. Messages may contain attachment tables and attachment subnodes.

### PFF block encoding

The header `bCryptMethod` describes the PFF block encoding: `0` (`None`), `1` (`Permute`), or `2` (`Cyclic`). These are reversible format encodings, not a user password or modern disk encryption. OSTOM decodes supported block encodings in memory; it does not write a decrypted copy or bypass account, DPAPI, EFS, or BitLocker protections. Unknown methods are reported rather than guessed.

## Reading diagnostic results

- Header failure: the file may be truncated, not a PFF file, or have a damaged header.
- Index access failure: NBT/BBT roots or referenced pages cannot be read.
- Folder-tree failure: the root is readable, but hierarchy tables or child nodes are damaged.
- Message failure: a folder is readable, but one or more message nodes or content rows fail.
- Attachment failure: an attachment table, attachment node, or attachment data stream fails.

OSTOM should report these levels separately. Being able to open the root only
shows that the first part of the store is readable; it does not confirm that
all messages and attachments are intact.

Outlook behaviour is useful context, but is not a direct integrity test. Safe
Mode disables COM add-ins and Exchange client extensions; if Outlook works only
there, an add-in may be involved. For Cached Exchange Mode, Microsoft documents
rebuilding an OST, but that should not be the first step when local-only
folders may contain data absent from the server. See Microsoft's [Sync
Monitor](https://learn.microsoft.com/en-us/microsoft-365-apps/outlook/configuration/sync-monitor-improvements),
[Outlook performance troubleshooting](https://support.microsoft.com/en-us/outlook/how-to-troubleshoot-performance-issues-in-outlook),
and [synchronization guidance](https://learn.microsoft.com/en-us/troubleshoot/outlook/synchronization/synchronization-issues-occur-in-outlook-owa).

The report's `deep_recovery` plan is evidence-based. `required` is used when a
lower layer is inaccessible (for example, invalid header/size, missing roots,
or index-access failure), or when the normal walk finds no messages despite
declared content. `recommended` requires stronger accumulated evidence, such
as multiple payload read/decode failures, missing NBT-to-BBT links, or a
substantial share of message errors. A lone parser error, allocation-map
anomaly, or advisory trailer/CRC mismatch does not trigger escalation alone.
The scan remains explicit because it can take a long time.

## What the current structural scan checks

Before the message walk, the current Go implementation performs a bounded,
read-only structural scan. It reports the detected PFF variant, declared and
actual file size, header CRC where the layout is known, NBT/BBT root offsets,
allocation-page checks, root-page type and shape, branch-page child-reference
bounds, and a guarded walk through root-connected NBT/BBT pages. For Unicode
BBT leaves it also extracts `IB`/`CB` block references and checks whether their
physical ranges fit inside the file.

Indexed reading handles 4 KiB B-tree pages and compressed blocks. The BBT
provides stored and inflated block lengths; inflation is bounded by the
declared length and checked for an exact match. A public OST sample smoke
exported 12 messages and 4 attachments without failures. This validates the
indexed path on one sample, not every 4 KiB block, compression, encryption, or
native/orphan attachment-tree variant. CRC mismatches remain diagnostic
evidence, not a corruption verdict by themselves.

## Important OST variant: 4 KiB pages and compressed blocks

The ordinary 512-byte-page and Unicode PFF layouts are not the whole OST landscape. The Outlook 2013-era 64-bit Unicode variant uses 4096-byte pages and compressed-block metadata in its BBT entries. Exact page and block layouts are described in the primary specification and implementations linked above.

This matters operationally: a parser that recognizes the `SO` OST signature but only understands the 512-byte/ordinary Unicode layout may produce a false “corrupt” diagnosis. OSTOM reports the detected format variant and treats parser failures as “damaged or unsupported” until structural evidence distinguishes them.

`libpff` remains a useful independent implementation reference. It has a different role and license from OSTOM and is not part of the MIT core.

### External 4 KiB compatibility smoke

Real mail stores are not committed to this repository. Two public OST files
have been used for local smoke testing (2026-10-18):

- [`example-2013.ost` from freepst](https://raw.githubusercontent.com/hrbrmstr/freepst/master/inst/extdata/example-2013.ost), an Apache-2.0 project fixture. It exported 3 indexed messages and has no attachments. Observed SHA-256: `b18ff390d0c22273029ee580ab6025976af69a4f84e276d1b1c01a988650853d`.
- [`sample.ost` from the GroupDocs example page](https://docs.groupdocs.com/conversion/java/load-personal-storage-pst-or-ost-with-options/). Header inspection identified a 4 KiB Unicode OST. OSTOM exported 12 indexed messages and 4 attachments with no attachment failures; the output contained 4 MIME attachment sections.

These are smoke tests, not exhaustive coverage or a representative mailbox
corpus. The GroupDocs file was used locally and is not redistributed by OSTOM.
The opt-in `TestExternal4KOSTCorpusSmoke` test accepts `OSTOM_EXTERNAL_OST` and
exercises the public indexed streaming path with a supplied file. Run it after
independently checking the download and its licence:

```sh
OSTOM_EXTERNAL_OST=/path/to/example-2013.ost go test -trimpath ./internal/format \
  -run 'TestExternal4K.*CorpusSmoke'
```

### Test corpus and fixture provenance

The local developer corpus under
`github.com/useti/go-pst/v6/data/support.pst` contains 59 indexed messages,
including Sent-folder data. It is a PST, not a genuine OST, and is not committed
or included in releases. The `testdata/fixtures/*.pst` and `*.ost` files are
local derivatives used for header, truncation, trailer, B-tree, XBLOCK, and
subnode damage tests.

The fixture generator can also build a parser-validated synthetic OST. For
`-messages 50`, it uses the Enron PST corpus, checks that enough indexed
messages exist, changes the PFF client magic, validates the resulting
container, then applies requested damage. Historical message content and
addresses may be real; these generated fixtures are for local tests only and
are not committed or released. Check the upstream corpus terms before
redistributing copies.

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

Trailer checks are reported as warnings, not as a final verdict. The bundled
corpus can be readable by the high-level parser even when the bytes found at a
BBT-derived trailer location are zeroed or do not match the expected layout.
That result calls for more format investigation; it does not automatically
mean that the file is corrupt.
