# OSTOM — Engineering plan

OSTOM is a read-only, local-first recovery tool for damaged Outlook
`.ost`/`.pst` files. Its job is to preserve evidence and export whatever is
still readable; it must never imply that a server copy is complete or alter the
source file.

## Current baseline

- Safe header/index diagnostics for Unicode and detected 4 KiB layouts.
- Guarded native traversal from NBT/BBT blocks through bounded HN/PC/TC
  decoding, with conservative message/attachment candidate plumbing. The
  real 4 KiB attachment-bearing sample currently yields 13 candidates from
  14 message/associated-message nodes and associates four recovered attachment
  payloads; each carries a non-fatal PidTagAttachSize warning.
- Native attachment candidates retain message BID/NID association, declared
  and readable sizes, `Content-ID`, and MIME role when their properties are
  readable; payload collection remains bounded in memory.
- Indexed Thunderbird `mbox + .sbd` export, plus a separate native-recovery
  mbox and `.eml` files. Individual message or attachment failures do not stop
  the export.
- Indexed parsing of 4 KiB B-tree pages, BBT-declared compressed blocks,
  XBLOCK and subnode payloads is smoke-tested against a public
  attachment-bearing OST. Full attachment export is still not established.
- JSON reporting, space checks, progress/idle-timeout controls, and provenance
  for native candidates.

This is a useful diagnostic/export baseline, not yet a complete OST recovery
engine. Native items can be partial, duplicate indexed messages, and lack a
reliable original folder path.

## Evidence status — 2026-10-18

- [x] Deterministic synthetic corruption smoke covers header CRC, declared
  size, truncation, missing B-tree roots, and block trailers without panic.
- [x] Public 4 KiB sample header, NBT/BBT traversal, XBLOCK and subnode
  payload paths complete successfully.
- [x] Native recovery now filters NBT message types instead of spending its
  bounded budget on folders and service nodes.
- [x] Native HN/PC decoding produces 13 conservative recovered-message
  candidates from 14 message/associated-message nodes on the public 4 KiB
  sample. Four attachment records are associated with status `recovered` and
  the scan reports zero resolution errors. PidTagAttachSize is larger than
  the data property, as permitted by the MAPI definition.
- [x] The 4 KiB compressed-block model is confirmed for this sample: BBT leaf
  entries carry compressed CB at bytes 16:18 and InflatedSize at bytes 18:20;
  zlib-wrapped payloads inflate to HN roots. This does not yet cover every
  4 KiB compression/encryption variant.
- [ ] End-to-end export of all public-sample messages and attachments is
  reproducible in the current CLI; native candidate collection and attachment
  association and bounded payload recovery are proven for four attachments;
  full CLI export is still a separate end-to-end check.

## Immediate execution queue

- [x] Add a regression assertion for the public sample baseline: 14 attempted
  message/associated-message nodes and at least 13 candidates.
- [x] Validate the 4 KiB subnode leaf stride and normalize 4K subnode NIDs to
  the low 32 bits used by PC references; recursively follow nested SubBIDs.
- [x] Resolve the remaining attachment property references (`0x819F` and
  `0x827F`) by inflating BBT-declared compressed leaf payloads even when the
  BID carries the internal flag.
- [x] Re-run attachment association: four attachments are associated and all
  four have status `recovered`; size differences are non-fatal warnings.
- [x] Confirm the size semantics: PidTagAttachSize includes attachment-object
  properties and must not be compared directly with the 0x3701 data length.
- [ ] Only then implement or claim complete attachment export.

## Now — recover and associate attachments

- [ ] Resolve arbitrary native attachment streams without buffering their
  complete payload; preserve declared/readable size, checksum, output path, and
  per-attachment errors.
- [ ] Deferred: implement this only after the current native-first CLI and
  bounded manifest path remain stable; this is the next deep format task, not a
  prerequisite for safe recovery of the proven sample.
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
- [ ] Smoke-test indexed 4 KiB page/block parsing on two genuine public OSTs:
  the [freepst sample](https://github.com/hrbrmstr/freepst/blob/master/inst/extdata/example-2013.ost)
  exported 3 messages (no attachments), and the
  [GroupDocs sample](https://docs.groupdocs.com/conversion/java/load-personal-storage-pst-or-ost-with-options/)
  passes low-level 4 KiB/index/XBLOCK/subnode smoke. The previous 12-message/
  4-attachment export result is retained as historical evidence only and is
  not reproduced by the current CLI. Samples are local and not committed.
- [ ] Smoke-test indexed/native attachment extraction from a genuine public
  4 KiB OST. Current evidence proves attachment references and some payload
  paths, but not four complete exported attachments.
- [ ] Validate other 4 KiB compression/encryption variants and native/orphan
  attachment-tree recovery against additional attachment-bearing OST samples.

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

## Recovery completion evidence — 2026-10-18

- [x] Full CLI recovery smoke completed on `testdata/external/sample.ost` with
  an external timeout and a 60-second indexed-worker timeout.
- [x] Native-first recovery completes before the optional indexed backend;
  the public 4 KiB sample produced 13 native candidates and 4 attachments.
- [x] `ostom-report.json`, `recovery-manifest.json`, and
  `recovery-manifest.csv` are written even when indexed go-pst stalls.
- [x] Deep recovery no longer enables go-pst recovery heuristics; go-pst is
  ordinary best-effort indexed export only.
- [ ] Indexed go-pst export is not complete on this sample: it stalls while
  reading a later message and is reported as a bounded backend error.
