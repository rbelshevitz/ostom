# OSTOM — Engineering plan

OSTOM is a read-only, local-first recovery tool for damaged Outlook
`.ost`/`.pst` files. Its job is to preserve evidence and export whatever is
still readable; it must never imply that a server copy is complete or alter the
source file.

## Current baseline

- Safe header/index diagnostics for Unicode and detected 4 KiB layouts.
- Guarded native traversal from NBT/BBT blocks through bounded HN/PC/TC
  decoding, including conservative message and attachment candidates.
- Native attachment candidates retain message BID/NID association, declared
  and readable sizes, `Content-ID`, and MIME role when their properties are
  readable; payload collection remains bounded in memory.
- Indexed Thunderbird `mbox + .sbd` export, plus a separate native-recovery
  mbox and `.eml` files. Individual message or attachment failures do not stop
  the export.
- Indexed parsing of 4 KiB B-tree pages and compressed blocks, smoke-tested
  against a real Outlook 2013 OST with 3 indexed messages (no attachments).
- JSON reporting, space checks, progress/idle-timeout controls, and provenance
  for native candidates.

This is a useful diagnostic/export baseline, not yet a complete OST recovery
engine. Native items can be partial, duplicate indexed messages, and lack a
reliable original folder path.

## Now — recover and associate attachments

- [ ] Resolve arbitrary native attachment streams without buffering their
  complete payload; preserve declared/readable size, checksum, output path, and
  per-attachment errors.
- [ ] Recover attachment tables and associate each attachment with its parent
  message using structural identifiers; keep inline, embedded, and external
  cases distinct.
- [ ] Add deterministic fixtures for multiple attachments per message,
  truncation, and association failures.

## Then — make recovery reviewable

- [ ] Write one deterministic recovery manifest (JSON and CSV) covering every
  exported or skipped indexed/native message and attachment: source identity,
  route, NID/BID when known, byte counts, checksum, path, and bounded error
  evidence.
- [x] Emit safe per-message `.eml` fallback for indexed export too, with stable
  collision-resistant names.
- [ ] Preserve source store/folder paths where the reader can establish them;
  clearly label native/orphan items whose path is unknown.
- [ ] Separate and count recovered, partial, skipped, and orphaned items in
  the human report and JSON.

## Next — recover more without losing bounds

- [ ] Complete bounded streaming of arbitrary native XBLOCK/XXBLOCK and
  SLBLOCK attachment payload trees, including 4 KiB variants.
- [ ] Extend native attachment streaming to XBLOCK/XXBLOCK/SLBLOCK and 4 KiB
  payload variants after the common attachment stream path is validated.
- [ ] Add a salvage path for missing/damaged indexes, keeping its output
  separate from indexed data and suppressing only explainable duplicates.
- [ ] Make folder/message/attachment damage independently visible; add stable
  exit codes and a quick scan that does not read attachment payloads.

## Validate before claiming support

- [ ] Add a deterministic attachment-bearing fixture/corpus and corruption
  cases for truncation, roots, allocation maps, subnodes, and payloads.
- [ ] Test a genuine Outlook OST containing local-only Sent/Drafts and
  attachments.
- [x] Smoke-test indexed 4 KiB page/block parsing on a genuine Outlook 2013
  OST: 3 of 3 indexed messages exported; the sample has no attachments.
- [ ] Validate 4 KiB attachment payload trees and encryption semantics on
  attachment-bearing real samples. The current smoke does not cover attachments
  or native attachment-tree recovery.

## Later, only if it strengthens the workflow

- [ ] Inspect all stores in an Outlook profile and identify local candidates
  from evidence without claiming server absence.
- [ ] Add optional, licence-reviewed libpff comparison as an oracle; never
  copy it into the MIT core or silently merge its results.
- [ ] Add attachment duplicate analysis and largest-item summaries once the
  manifest supplies stable identities.

## Done means

On a copy of a real OST, OSTOM reports folders/messages/attachments and their
readability, localizes failures, exports every independently readable item into
a Thunderbird-compatible hierarchy, produces a reproducible recovery record,
and never modifies or uploads the source.
