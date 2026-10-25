# OSTOM — what works and what remains

This file answers the practical question: “What can OSTOM do today?”
The final sections list known limitations and the next engineering tasks.

Historical implementation notes are kept in
[TODO.history.md](TODO.history.md). That file is an archive, not a second
active work queue.

## What works now

### File reading and inspection

- OSTOM opens `.ost` and `.pst` files read-only and never modifies the source.
- `--check` inspects the header, file size, allocation tables, and index roots
  without reading message or attachment bodies.
- `--analyze` performs bounded content analysis without export.
- The result includes a JSON report with separate file, folder, message, and
  attachment details.

### Mail export

- The normal indexed export preserves readable folders and messages in a
  Thunderbird-compatible mbox tree.
- A separate structural recovery pass can find messages hidden from the
  normal indexed path because their indexes are damaged.
- The two output paths remain separate; results found by different methods are
  not silently mixed or turned into hidden duplicates.
- A failed message or attachment does not discard results already written.
- The `--eml` option can additionally write one `.eml` file per recovered
  message.

### Attachments and large files

- Large attachments are read as streams instead of requiring the entire
  attachment in memory.
- The report preserves declared and actually read sizes, truncation flags,
  errors, and the output path for each attachment.
- Partial and failed recovery is reported explicitly and is never presented as
  complete.
- `--native-only` can run the additional recovery path without starting the
  optional indexed `go-pst` path.
- A real public OST with 4 KiB pages is covered by smoke tests for compressed
  blocks, XBLOCK, subnodes, attachments, and native recovery.

### Reports and GUI

- The final report can be written as JSON.
- Machine-readable progress is emitted as one JSON object per line on `stderr`.
- The recovery manifest distinguishes exported, partial, failed, and duplicate
  results.
- The Windows GUI uses the same CLI and provides source-file selection,
  destination selection, progress, and final status.

## What is not guaranteed

- Complete recovery from every damaged OST/PST file.
- Support for every arbitrary `XBLOCK`/`XXBLOCK`/`SLBLOCK` attachment chain or
  every multi-part attachment stream.
- A complete indexed export from every real OST file.
- Recovery of deleted data, password bypassing, DPAPI, EFS, or BitLocker data.
- Any conclusion about disk health or proof that a server has a complete copy
  of the local data.

## Next engineering tasks

1. **Reproducible corruption corpus and recovery oracle**
   - Extend `cmd/ostom-fixture` with deterministic single-fault PST/OST
     mutations for NBT/BBT links, block trailers, data trees, subnodes, and
     payload ranges.
   - Write a manifest for each damaged fixture containing the source hash,
     mutation, offset or field, expected mode, and expected baseline result.
   - Compare recovered, partial, and lost messages and attachments with the
     healthy baseline; a “does not crash” smoke test is not enough.
   - [x] Damage manifests record recovered and partial attachments and compare
     message and attachment identities with the healthy baseline.
   - [x] Synthetic indexed-damage smoke coverage confirms that isolated
     non-root mutations do not silently lose baseline messages.
   - Keep synthetic OST fixtures clearly marked as parser-valid containers, not
     evidence of Outlook- or Exchange-generated corruption.

2. **Resumable inventory and manifest**
   - Stream folders, messages, attachments, and recovery candidates.
   - Make the append-only journal authoritative for item state, output,
     checksums, sizes, and errors.
   - Add interruption/resume coverage for a large synthetic source.
   - Measure RSS/working set and Go runtime memory on the 2 GiB sparse fixture
     under a documented `GOMEMLIMIT`; do not confuse file size with allowed
     heap size.
   - Run an end-to-end interruption/resume test and verify that committed
     batches are reused, incomplete batches are retried, and no duplicates are
     published.
   - [x] Native recovery has a combined interrupted-state test covering
     committed-batch reuse and removal of an uncommitted batch.

3. **Native attachment recovery**
   - Stream arbitrary `XBLOCK`/`XXBLOCK`/`SLBLOCK` trees, including 4 KiB
     variants.
   - Preserve message association, declared/read/exported sizes, checksums,
     and partial or failed status.
   - Validate this path on additional real OST/PST files with attachments.

4. **Evidence coverage**
   - Establish a bounded healthy PST/OST baseline for indexed reading.
   - [x] Run the real public 4 KiB OST smoke: compressed blocks, `XBLOCK`,
     subnodes, attachments, and native recovery all pass.
   - Test additional real OST samples and compression/encryption variants.
   - Clarify and independently verify the 4 KiB block-trailer and CRC layout.
   - Keep unsupported cases explicit; do not claim complete recovery without
     evidence.

5. **Release readiness**
   - Generate dependency notices from the actual Go module graph and retain
     ambiguous licenses for review.
   - Run the full Go suite and the large acceptance smoke.
   - Verify the Windows archive, checksums, release notes, and files required
     only at runtime.
   - Walk the GUI through “no file selected”, “analysis complete”, “0 messages
     found”, and “export complete”; verify that the final progress line does
     not retain stale ETA text and that internal terms are not shown.

## Project guarantees

OSTOM does not modify the source, hide partial results, or mix indexed and
native output. PST/OST generation remains external tooling and is not part of
the Go runtime.
