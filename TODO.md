# OSTOM — engineering plan

This file is the active work queue. Historical implementation notes and
completed checklists are kept in [TODO.history.md](TODO.history.md).

## Current priorities

1. **Resumable inventory and manifest**
   - Stream folders, messages, attachments, and recovery candidates.
   - Make the append-only journal authoritative for item state, output,
     checksums, sizes, and errors.
   - Add interruption/resume coverage for a large synthetic source.

2. **Native attachment recovery**
   - Stream arbitrary XBLOCK/XXBLOCK/SLBLOCK trees, including 4 KiB variants.
   - Preserve message association, declared/read/exported sizes, checksums, and
     partial or failed status.
   - Validate the path on real OST/PST files with attachments.

3. **Evidence coverage**
   - Establish a bounded healthy PST/OST indexed baseline.
   - Test additional real OST samples and compression/encryption variants.
   - Keep unsupported cases explicit; do not claim complete recovery without
     evidence.

4. **Release readiness**
   - Generate dependency notices from the actual Go module graph and retain
     ambiguous licenses for review.
   - Run the full Go suite and the large acceptance smoke.
   - Verify the Windows archive, checksums, release notes, and runtime-only
     contents.

## Done means

The change is reproducible, resumable, and evidence-backed. OSTOM never
modifies the source, hides partial results, or mixes indexed and native output.

## Scope boundary

PST/OST generation remains external tooling, not part of the Go runtime.
