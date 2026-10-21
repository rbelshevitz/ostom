# OSTOM — Engineering plan

OSTOM is a read-only, local-first recovery tool for damaged Outlook
`.ost`/`.pst` files. Its job is to preserve evidence and export whatever is
still readable; it must never imply that a server copy is complete or alter the
source file.

## Short plan — read this first

### Now

- [ ] Design resumable recovery for large OST/PST files, including occasional
  5, 15, and 20+ GB cases: persist inventory, per-item outcomes, checkpoints,
  and enough provenance to resume without repeating completed work.
- [ ] Establish a bounded healthy-PST/OST indexed-export baseline independent
  of native recovery. Verify folder/message/attachment counts and checksums;
  document `go-pst` limitations without treating it as a recovery reference.

### Next

- [ ] Stream arbitrary native XBLOCK/XXBLOCK/SLBLOCK attachment trees,
  including 4 KiB variants.
- [ ] Add a separate salvage path for missing or damaged indexes.
- [ ] Make folder, message, and attachment damage independently visible, with
  stable exit codes and a quick scan that skips attachment payloads.

### Later

- [ ] Validate additional real OST samples and compression/encryption variants.
- [ ] Consider an external, licence-reviewed comparison reference such as libpff.
- [ ] Keep PST/OST generation outside the Go runtime.

The sections below are reference notes and evidence, not a second task queue.

## Large-file recovery — resumable CLI pipeline

Large OST/PST files must be processed as a resumable job rather than one
unbounded command. This is a CLI/core concern and does not belong to the GUI
polish queue.

Implementation order, keeping service data away from the user's folders:

- [ ] Keep all service data under a hidden `.ostom` directory inside the export
  directory; never place journals or unfinished files in `Local Folders`.
- [x] Define a hidden batch record with number, state, size, and checksum;
  reuse it only after both the record and artifact validate.
- [x] Validate a whole batch output directory, not only one file: include
  relative names, total bytes, and the contents of every file.
- [x] For native export, write each batch to a hidden directory first, validate
  and commit it, then assemble visible output only after the pass succeeds.
- [x] Resume native export from committed batches: bind them to the source
  fingerprint session, validate before reuse, and retry only missing or
  damaged portions.
- [x] Stream MIME messages and attachments to disk without assembling a whole
  message or encoded attachment in RAM; keep in-memory limits only for already
  collected native candidates.
- [x] In the CLI native path, stop retaining every structurally recovered
  message before export: emit each candidate to a consumer as soon as it is
  parsed and release its body after writing; retain the old list API for
  compatibility.
- [x] Bound the native export queue to small batches: at most 64 messages or
  about 8 MiB of data, in addition to the one message currently being parsed.
- [ ] Separate transient and resumable data: use the system `Temp` only for
  short-lived small files, while large bodies, batches, and the journal live
  in a hidden OSTOM work area on the same volume as the result.
- [x] Move attachment-body spooling out of anonymous system `Temp` and into
  the session work area; if space is insufficient, stop the current batch
  clearly while preserving already committed batches.
- [ ] Split the work into self-contained batches and write each batch first to
  an internal temporary destination.
- [ ] Mark a batch complete only after validation; discard and retry an
  incomplete batch on the next run.
- [ ] Build visible mbox output from committed batches into a temporary file,
  then replace the previous result with one safe move.
- [ ] Resume the same job automatically from hidden state; users should not
  need separate commands or service flags.

Do not enable journal-only message skipping before these guarantees exist: a
journal does not prove that visible output was not deleted, changed, or
truncated.

- [x] Define a session sidecar with source fingerprint (size, modification
  metadata, header identity), schema version, phase, cursor, and options.
- [ ] Stream a durable inventory of discovered folders, messages, attachments,
  and native candidates instead of retaining the complete worklist in memory.
- [ ] Write an append-only manifest with a stable source identity, route,
  status, output path, byte counts, checksum, and error evidence for every
  item.
- [ ] Use explicit item states: `discovered`, `in_progress`, `exported`,
  `partial`, `skipped`, and `failed`; treat an interrupted `in_progress` item
  as safely retryable.
- [ ] Make each batch transactional: write to a temporary destination, verify
  it, atomically finalize it, then commit the manifest record and checkpoint.
- [ ] Resume only when the source fingerprint and relevant options match;
  refuse to mix results from different source files or incompatible runs.
- [ ] Add deterministic deduplication for indexed/native results so a resumed
  or fallback pass cannot export the same message or attachment twice.
- [x] Replace the fixed indexed-export watchdog with heartbeat/idle detection
  plus a size-aware absolute cap; a large healthy file must not be killed just
  because it needs more than 60 seconds.
- [ ] Make native recovery budgets phase-aware and resumable; report when a
  node, byte, message, or attachment limit stopped a batch.
- [ ] Keep mbox generation restart-safe: do not mark an item complete until
  its output is durable, and define how committed batches are merged without
  duplicate messages.
- [ ] Add interruption/resume tests for large synthetic sources and record a
  validation plan covering real OST/PST files across several sizes, including
  5, 15, and 20+ GB cases, before claiming broad large-file support.

## .NET GUI — compact utility polish

- [ ] Replace the plain report text with compact result cards for structure,
  messages, attachments, storage, warnings, and errors.
- [ ] Keep the main window compact: no decorative hero heading, equal-width
  file/action buttons, and enough vertical space for the report.
- [x] Apply a compact Rufus-inspired utility layout: grouped source/export
  inputs, aligned actions, a dedicated result panel, and a persistent status
  strip without adding decorative branding.
- [x] Keep the startup state explicit (`Waiting for file`) and distinguish it
  from running, completed, warning, partial, cancelled, and failed states.
- [x] Add a real inline status/info area with text and severity, not color alone;
  keep technical details behind an optional details view.
- [ ] Verify the layout at 100%, 125%, 150%, and 200% Windows display scale,
  including long Russian and English strings.
- [x] Package `ostom-gui.exe` and the matching `ostom.exe` side by side in a
  reproducible Release build.
- [x] Rebuild `ostom.ico` from the canonical PNG with explicit 16×16 through
  256×256 layers; embed the source icon in the GUI so it is not required as a
  separate file beside the published EXE.
- [x] Publish the user-facing Windows package as a self-contained single-file
  GUI; treat the framework-dependent `ostom-gui.dll` output as an intermediate
  build layout, not the primary handoff.
- [ ] Add a small GUI smoke checklist for empty input, missing CLI, cancelled
  work, failed export, partial recovery, and successful export.

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

## Test-fixture strategy

- [x] Add a deterministic Go EML corpus generator to the service fixture
  utility (`ostom-fixture -kind eml`) with Unicode messages, multiple and
  inline attachments, stable MIME boundaries, and a SHA-256 manifest.
- [x] Add a `-min-size` mode that grows a validated small PST/OST into a large
  test file without loading the whole file into memory.
- [x] Add an opt-in large-PST test through `OSTOM_LARGE_PST`, so ordinary CI
  never downloads or stores the large file.
- [x] Add an optional external PST builder for controlled fixtures through
  `scripts/generate-large-pst.ps1`. Keep it outside the OSTOM runtime, pin the
  builder version, and validate generated PSTs with OSTOM. Large files stay out
  of ordinary CI and Git.
- [x] Record the external builder workflow: download the pinned source, run the
  `PstBuilder.LargeTest` sample with `dotnet run`, generate the PST, and run
  `TestLargePSTRecoverySmoke`.
- [ ] Do not implement a PST writer in Go as part of OSTOM. The existing Go
  writer dependency is not a reliable generator; PST creation is a separate
  tool concern and must not block OST recovery work.
- [ ] Keep real public OST files as the evidence source for OST-specific
  behavior: provider/sync metadata, local-only folders, and compressed 4 KiB
  variants. PST and synthetic-OST fixtures cover the common PFF/MAPI paths
  only.

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
- [x] Preserve source store/folder paths where the reader can establish them;
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
- [ ] Add optional, licence-reviewed libpff comparison as a reference; never
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
## Next implementation slice

- [x] Add a conservative file-level integrity assessment from header, size,
  allocation, and index-root evidence; keep storage-device diagnosis out of
  scope.
- [x] Separate native recovery success from optional indexed-export timeout in
  the user-facing report.
- [x] Use the assessment to choose or recommend recovery mode automatically;
  keep manual override and explain the decision in calm language.
- [x] Replace the attachment export boundary with an `io.Reader`/`Open`-based payload interface; keep the current byte-slice path as a compatibility fallback.
- [x] Remove full attachment materialization from structural recovery: spool large payloads to bounded temporary storage and expose them through a reader with cleanup ownership.
- [x] Preserve declared size, readable size, truncation/incomplete status, and recovery errors for every attachment.
- [x] Extend the export manifest with checksum, declared/readable/exported byte counts, output path, and per-attachment errors.
- [x] Add regression coverage for large, truncated, inline, Unicode-named, and multiple attachments using the deterministic EML corpus.
- [x] Validate the complete path against the public 4 KiB OST sample and record what remains unproven. On 2026-10-19 the forced CLI scan wrote 13/13 native messages and 4/4 attachments with zero collection errors; indexed go-pst still reached its bounded 60-second timeout. The sample exposed zero readable hierarchy-table rows, so hierarchy consistency remains unproven on this corpus; arbitrary attachment trees and complete indexed export also remain unproven.
- [ ] Keep PST/OST generation out of the Go runtime: an external PST builder remains optional tooling, not a prerequisite for OSTOM recovery.
## Reference — completed hierarchy diagnostics

- [x] Validate hierarchy-table consistency: compare child-folder `RowID` with `parentNID` and row name with `PidTagDisplayName`; report mismatches as diagnostics only and never trigger native recovery automatically.
