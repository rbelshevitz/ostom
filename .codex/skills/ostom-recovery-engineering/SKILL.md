---
name: ostom-recovery-engineering
description: Maintain OSTOM's read-only PST/OST recovery pipeline, indexed/native export, resumability, progress JSON, watchdogs, and evidence-oriented tests.
---

# OSTOM recovery engineering

Use this skill for changes to the Go recovery CLI, indexed/native export,
resumable sessions, progress JSON/JSONL, watchdog behavior, or the related
evidence and smoke tests. The existing `ostom-dotnet-gui` skill remains the
source for detailed WinForms layout work.

## Product invariants

- OSTOM reads the source PST/OST and never modifies it.
- Recovery and export results must preserve provenance and expose partial or
  failed work instead of presenting it as complete.
- Keep indexed and native output paths separate when both are used; an indexed
  failure must not mix fallback output into the indexed result.
- Preserve the CLI arguments, stdout report JSON, stderr progress JSONL, and
  cancellation behavior unless the task explicitly changes the contract.
- Durable output and journal state must be resumable without duplicating
  committed messages or attachments.

## Indexed export and safety

- The indexed path is optional; a timeout or parser error must remain visible
  and must not prevent a native fallback when the caller selected one.
- Indexed message totals are obtained by an isolated count worker. It has a
  30-second timeout; if it fails, the main export proceeds with an unknown
  total. The count worker reads folder message tables, not message bodies or
  attachments.
- The main indexed worker has heartbeat and absolute-timeout protection. Do not
  move preflight back into the main export worker. The idle watchdog is
  5 minutes; the absolute indexed limit starts at 10 minutes, adds 5 minutes
  per GiB, and is capped at 2 hours.
- A total obtained by preflight must be discarded if the source snapshot changes
  before export. The current snapshot check uses source size and modification
  time; it is not a full-content hash.
- `--native-only` skips the optional indexed path and must not create indexed
  output or start the indexed watchdog.
- `go-pst` is vendored at `third_party/go-pst` through the `replace` directive;
  preserve its source and `LICENSE.txt` when packaging source distributions.

## Progress and JSON

- ETA is advisory: calculate it from recent smoothed throughput, only after
  warm-up and only when a reliable total exists.
- With an unknown total, report throughput but never invent an ETA.
- Keep progress events valid single-line JSON. Use `encoding/json`; do not build
  progress or worker reports through string concatenation.
- Validate both the final report JSON and every JSONL record in smoke tests.
- GUI-facing labels must remain localizable for Russian and English regions;
  machine-readable progress units may remain stable English values.
- The GUI displays throughput and ETA only as advisory status; unknown totals
  must remain unknown rather than being inferred from elapsed time.

## Verification

Run focused tests while iterating, then the full suite:

```powershell
go test -trimpath ./...
dotnet build .\gui\Ostom.Gui\Ostom.Gui.csproj -v:q
```

The long-run acceptance fixture is `testdata/fixtures/fifty.ost`. The known
green result is 2,178 messages, 168 attachments, zero partial messages, and
zero attachment failures. Keep this as an acceptance smoke, not a fast unit
test.

Before a public release, generate a complete dependency license report from the
actual `go list -m -json all` graph and preserve upstream license/NOTICE text.
Do not infer or rewrite terms for dependencies whose license files are missing
or ambiguous.

## Current boundaries

The resumable inventory/manifest work, arbitrary native attachment trees,
additional real corpora, full GUI smoke execution, and complete dependency
license report remain follow-up work unless the repository TODO marks them
complete.
