# OSTOM — Goals and TODO

> This is a contributor and engineering plan, not end-user documentation. For
> installation and recovery instructions, start with [README](README.md).

## Project goal

Build a safe utility for diagnosing, analyzing, and recovering mail from damaged Outlook `.ost`/`.pst` files, with export to Thunderbird-compatible formats.

## Primary mission: local-only recovery

The main use case is recovering messages that exist in a local Outlook store but may no longer exist on the IMAP/Exchange server. OSTOM must not treat the server mailbox as the source of truth and must not discard folders merely because they look local, non-standard, or unsynchronized.

The first priority is preserving local Sent, Drafts, Outbox, Conflicts, “This computer only”, hidden, and custom folders from every supplied store.

## Principles

- Never modify the source file.
- Separate confirmed data from estimates and assumptions.
- Continue after individual message or attachment errors.
- Report recovered objects and unrecoverable areas.
- Never upload OST contents to the internet.

## Milestones

### M0 — Foundation

- [x] Go CLI skeleton.
- [x] `.gitignore` for source archives, caches, binaries, and exports.
- [x] English-only CLI and JSON report.
- [x] README with usage instructions and format references.
- [x] Add an explicit MIT license for OSTOM source code.

### M1 — Quick inspection

- [x] File existence and size.
- [x] PFF signature and OST/PST type.
- [x] Detect known format version, page size, and the 64-bit 4 KiB variant marker; report possible compression conservatively.
- [ ] Confirm 4 KiB compressed-block semantics on genuine Outlook samples and detect encryption state.
- [x] Header CRC and declared file size for supported header layouts.
- [x] Keep recovery-backend panics inside the diagnostic report.
- [x] Create the destination directory before checking free space.
- [ ] Test against a real OST with local-only Sent/Drafts and attachments.
- [ ] Add a deterministic attachment-bearing fixture path or fixture corpus; the current generator reuses a validated PST corpus and does not synthesize attachment records itself. The present corpus fixture provides 59 real indexed messages for stress tests.
- [x] Report high-level folders, messages, unread messages, and attachments when the backend can enumerate them.
- [ ] Make those counts complete under damaged tables through native payload recovery.
- [ ] Stable exit codes for automation.
- [ ] Support a fast scan without reading every attachment payload.
- [ ] Inspect all Outlook stores in a profile directory, not just one named OST.
- [ ] Add a user checklist for Safe Mode, add-ins, profile failures, and OST rebuild decisions; never treat Safe Mode alone as file corruption evidence.
- [ ] Report the source store for every folder and message.

### M2 — Deep integrity scan

- [ ] Read all folders and message tables.
- [ ] Include local-only, Sent, Drafts, Outbox, Conflicts, hidden, and non-standard folders; never assume a folder is server-backed.
- [ ] Read every message and record errors by folder path.
- [ ] Read all attachment tables.
- [ ] Compare declared attachment size with readable bytes.
- [ ] Validate block/page checksums where supported.
- [ ] Detect broken offsets, missing nodes, invalid rows, and decompression errors.
- [x] Bounded preflight for NBT/BBT root offsets, root-page shape, and branch child-reference bounds.
- [x] Initial allocation-page checks for Unicode 512-byte and 4 KiB layouts.
- [x] Bounded recursive NBT/BBT traversal with depth, page-count, cycle, and child-offset guards.
- [x] Extract and bounds-check Unicode BBT leaf block references (`IB`/`CB`) during the guarded walk; report referenced bytes and invalid physical ranges.
- [x] Join NBT leaf `bidData`/`bidSub` references to BBT `BID` entries and report resolved versus missing block IDs.
- [x] Read unique NBT-referenced payload blocks with a hard count limit and classify external, extended, and subnode block families.
- [x] Add bounded advisory payload trailer/signature/CRC checks; mismatches do not alone become a corruption verdict.
- [x] Parse bounded XBLOCK/SLBLOCK headers and report their internal reference/entry counts and logical sizes.
- [x] Follow discovered XBLOCK and SLBLOCK BIDs with a bounded work queue; report newly discovered versus unreadable blocks.
- [x] Classify bounded NBT data payload headers as Heap-on-Node, Property Context, Table Context, or unknown.
- [ ] Decode XBLOCK/XXBLOCK and SLBLOCK trees completely, including 4 KiB variants.
- [ ] Decode Heap-on-Node/property entries and identify message, folder, table, and attachment nodes.
- [ ] Resolve BBT block references to NBT node/subnode payloads and recover messages that the high-level parser cannot enumerate.
- [ ] Test genuine 4 KiB/DEFLATE OST samples; the current synthetic OST fixture does not cover this variant.
- [ ] Separate structural, message, and attachment damage.
- [ ] Add bounded-memory streaming and progress checkpoints for multi-gigabyte files.
- [x] Add a bounded human-readable progress bar with an honest unknown-total state.
- [x] Run parsing in a supervised worker process with a configurable idle timeout.

### M3 — Recovery report

- [x] JSON report with statistics and bounded errors.
- [ ] CSV list of damaged messages and attachments.
- [ ] Largest folders and attachments.
- [ ] SHA-256 duplicate attachment detection.
- [ ] Separate regular attachments, inline images, embedded messages, and external references.
- [x] Estimate disk space required for export and check target free space before export.
- [ ] Mark server-backed versus local-candidate folders without assuming that the server copy is complete.
- [x] Report elapsed time, throughput, and progress checkpoint; remaining total is marked as declared evidence when available.
- [x] Define a parser backend interface so the high-level CLI is not locked to one reader.
- [ ] Add an optional libpff-backed recovery probe/export path after LGPL integration review; use it as an oracle, not as copied code.
- [x] Use the backend's streaming attachment writer; do not materialize the whole attachment payload for large exports.

### M4 — Recovery

- [ ] Export all readable messages despite partial damage.
- [ ] Salvage mode for missing or damaged indexes.
- [ ] Report orphaned and recovered items separately.
- [ ] Never overwrite the source OST.

### M5 — Thunderbird export

- [x] `mbox + .sbd` export preserving the indexed folder hierarchy.
- [x] Emit an RFC 4155-compatible `ctime` mbox separator so strict Windows MBOX viewers can index messages.
- [x] Preserve local-only messages that the high-level backend can read, including local Sent/Drafts candidates.
- [ ] `.eml` per message fallback.
- [ ] Safe folder names and conflict resolution.
- [ ] Checksums for exported messages.
- [x] Verify Thunderbird Local Folders layout with integration tests and document the import procedure.
- [ ] Export local-only content first and preserve its original store/folder path in the report.

The export path now has bounded per-message statuses, complete aggregate
counts, and an explicit separate mbox for messages returned by the all-node
recovery iterator. Native property/table salvage and duplicate suppression are
still pending.

## Next implementation order

1. Decode and assemble XBLOCK/XXBLOCK and SLBLOCK payload trees without whole-file buffering.
2. Read NBT node data as heap/property/table structures and identify message and attachment nodes.
3. Add native salvage export for messages that the high-level backend cannot enumerate.
4. Add per-message/per-attachment recovery status, `.eml` fallback, and source folder/store paths.
5. Validate against a genuine Outlook OST containing local-only Sent/Drafts and attachments.
6. Add genuine 4 KiB/compressed support and optional libpff comparison after licensing review.

The current task is therefore payload-to-message recovery; further format reconnaissance is supporting work, not the next milestone.

## Progress checkpoint — native message collection

The native read-only path now has a bounded vertical slice from indexed NBT
data through HN, PC, and TC row decoding. It can resolve available property
values, preserve NID/BID provenance, and collect conservative recovered-message
candidates with subject, message class, sender, date, bounded body bytes, and
error status. Basic attachment metadata and readable subnode payloads can also
reach the native mbox bridge. This is not yet full message recovery:
intermediate BTH levels, subnode-backed row matrices, complete MAPI value
decoding, arbitrary attachment streams, and production-grade native salvage
remain pending.

The first native export bridge now writes `RecoveredMessage` candidates to a
separate `OSTOM Native Recovered` mbox tree and marks them with a native source
header. `--recovery-scan` runs the existing go-pst probe and the bounded native
collector in one deep-recovery pass; native results are included in JSON and
the human-readable summary. Native collection now carries basic body/date and
attachment metadata/payloads into MIME export. The next work is resolving
attachment subnodes and retaining per-attachment status details in the final
log.

## Definition of done

A usable version must, against a copy of a real OST:

1. report folders, messages, and attachments;
2. report declared and readable attachment size;
3. localize errors to folders, messages, and attachments;
4. export all readable messages without stopping at the first error;
5. preserve the Thunderbird folder hierarchy;
6. create a reproducible JSON report;
7. never modify the source file.

## Current risk

The repository now has repeatable small PST/OST-like and header-damage smoke fixtures, plus panic-containment tests. They are not a substitute for real Outlook OST samples. Next inputs must include a real OST with local-only Sent/Drafts and separate truncation, zeroed NBT/BBT roots, damaged AMap, broken subnode, and attachment-payload cases.
