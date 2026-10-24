# OSTOM — engineering plan

This file is the active work queue. Historical implementation notes and
completed checklists are kept in [TODO.history.md](TODO.history.md).

## Current priorities

1. **Controlled corruption corpus and recovery oracle**
   - Extend `cmd/ostom-fixture` with deterministic, single-fault PST/OST
     mutations for NBT/BBT links, block trailers, data trees, subnodes, and
     payload ranges.
   - Write a manifest for every damaged fixture: source hash, mutation,
     offset/field, expected mode, and expected recoverable baseline.
   - Assert recovered/partial/lost messages and attachments against the
     healthy baseline; a no-panic smoke test is insufficient.
   - [x] Damage manifests record recovered/partial attachments and compare
     recovered message and attachment identities with the healthy baseline.
   - [x] Synthetic indexed-damage smoke asserts that baseline messages are
     not silently lost for isolated non-root mutations.
   - Keep synthetic OST fixtures clearly marked as parser-valid containers,
     not evidence of Outlook/Exchange-generated corruption.

2. **Resumable inventory and manifest**
   - Stream folders, messages, attachments, and recovery candidates.
   - Make the append-only journal authoritative for item state, output,
     checksums, sizes, and errors.
   - Add interruption/resume coverage for a large synthetic source.
   - Measure RSS/working set and Go runtime memory while processing the 2 GiB
     sparse fixture under a documented `GOMEMLIMIT`; do not confuse file size
     with an allowed heap size.
   - Run an end-to-end interruption/resume test and verify that committed
     batches are reused, incomplete batches are retried, and no duplicates are
     published.
   - [x] Native recovery has a combined interrupted-state test covering
     committed-batch reuse and removal of an uncommitted batch.

3. **Native attachment recovery**
   - Stream arbitrary XBLOCK/XXBLOCK/SLBLOCK trees, including 4 KiB variants.
   - Preserve message association, declared/read/exported sizes, checksums, and
     partial or failed status.
   - Validate the path on real OST/PST files with attachments.

4. **Evidence coverage**
   - Establish a bounded healthy PST/OST indexed baseline.
   - [x] Run the real public 4 KiB OST smoke: compressed blocks, XBLOCK,
     subnodes, attachments, and native recovery all pass.
   - Test additional real OST samples and compression/encryption variants.
   - Clarify and independently verify the 4 KiB block-trailer/CRC layout.
   - Keep unsupported cases explicit; do not claim complete recovery without
     evidence.

5. **Release readiness**
   - Generate dependency notices from the actual Go module graph and retain
     ambiguous licenses for review.
   - Run the full Go suite and the large acceptance smoke.
   - Verify the Windows archive, checksums, release notes, and runtime-only
     contents.
   - Walk the GUI through “no file selected”, “analysis complete”, “0 messages
     found”, and “export complete”: verify that the final progress line does not
     retain stale ETA text and that internal terms are not shown.

## Done means

The change is reproducible, resumable, and evidence-backed. OSTOM never
modifies the source, hides partial results, or mixes indexed and native output.

## Scope boundary

PST/OST generation remains external tooling, not part of the Go runtime.
