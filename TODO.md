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
- JSON reporting, space checks, progress/idle-timeout controls, and provenance
  for native candidates.

This is a useful diagnostic/export baseline, not yet a complete OST recovery
engine. Native items can be partial, duplicate indexed messages, and lack a
reliable original folder path.

## Now — make recovery reviewable

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

- [ ] Complete bounded streaming of XBLOCK/XXBLOCK and SLBLOCK payload trees,
  including 4 KiB variants.
- [ ] Resolve arbitrary attachment streams without buffering their complete
  payload, including 4 KiB variants; retain declared versus readable size and
  per-attachment status. Basic bounded native subnode attachment association
  and MIME roles are already present.
- [ ] Add a salvage path for missing/damaged indexes, keeping its output
  separate from indexed data and suppressing only explainable duplicates.
- [ ] Make folder/message/attachment damage independently visible; add stable
  exit codes and a quick scan that does not read attachment payloads.

## Validate before claiming support

- [ ] Add a deterministic attachment-bearing fixture/corpus and corruption
  cases for truncation, roots, allocation maps, subnodes, and payloads.
- [ ] Test a genuine Outlook OST containing local-only Sent/Drafts and
  attachments.
- [ ] Validate 4 KiB compressed payload trees and encryption semantics on real
  samples. The opt-in freepst smoke covers index traversal only, not XBLOCK
  trees or attachments.

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
