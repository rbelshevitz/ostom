# OSTOM

[![CI](https://github.com/rbelshevitz/ostom/actions/workflows/ci.yml/badge.svg)](https://github.com/rbelshevitz/ostom/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/doc/install)
[![.NET](https://img.shields.io/badge/.NET-8.0-512BD4?logo=dotnet&logoColor=white)](https://dotnet.microsoft.com/download/dotnet/8.0)

OSTOM is a read-only utility for recovering readable mail from Outlook PST/OST
files and exporting it to mbox and EML.

OSTOM reads Outlook data files without changing them. When an OST/PST is only
partly readable, it copies the mail and attachments it can recover into a
Thunderbird-compatible mbox tree, reports damaged parts, and leaves the source
file untouched.

In short, OSTOM is a local salvage tool with an honest report:

Writing a new “repaired” PST/OST file is outside OSTOM’s scope. The utility
does not try to rebuild a replacement Outlook data file; it saves recovered
messages and attachments in open export formats instead.

- the normal indexed export preserves readable folders and messages;
- a separate bounded recovery path can find additional messages when the index
  is damaged;
- messages and attachments are written as they are read, so one bad item does
  not discard the readable remainder;
- the report and manifest distinguish exported, partial, failed, and duplicate
  candidates.

## Quick start

Use a copy of the source file and keep the original unchanged.

Inspect the structure and save a JSON report:

```powershell
 .\ostom.exe -json -output-dir E:\recovered source-copy.ost
```

Export readable mail (the report is saved in the export directory too):

```powershell
 .\ostom.exe -export-dir E:\recovered source-copy.ost
```

If the file is damaged or the ordinary path is insufficient, force the
separate recovery pass:

```powershell
 .\ostom.exe -recovery-scan -export-dir E:\recovered source-copy.ost
```

Close Thunderbird, copy the resulting `Local Folders` directory into its
profile, and check the recovered messages and attachments before relying on
them.

## Options

```text
ostom [-json] [-check|-analyze] [-progress-json] [-eml]
      [-output-dir DIR] [-export-dir DIR]
      [-recovery-scan] [-native-only] [-quiet] file.ost
```

- `-check` — fast structure check without reading message content.
- `-analyze` — bounded content analysis without export.
- `-export-dir DIR` — destination for recovered mail. The normal export is
  written below `Local Folders`.
- `-output-dir DIR` — separate destination for `ostom-report.json` and, when
  recovery runs, `recovery-manifest.json`/`.csv`. If omitted, these files are
  written to `-export-dir`.
- `-recovery-scan` — force the separate bounded recovery path.
- `-native-only` — skip the optional indexed `go-pst` path.
- `-eml` — additionally write one `.eml` per recovered message on every
  selected export path, including native recovery.
- `-json` — write the final report as JSON.
- `-progress-json` — write machine-readable progress events to stderr.
- `-quiet` — suppress progress text.

`-recovery-scan` and `-native-only` require `-export-dir`. The indexed
`go-pst` path is optional; its timeout or error is reported and does not change
the read-only bounded recovery path.

When native recovery and indexed export both run, their outputs are kept apart:
native recovery uses `Local Folders`, while the indexed export uses `Indexed`.
This prevents duplicate messages from being silently merged into one mailbox
tree.

## What to expect

OSTOM reports what was readable from this particular file. A successful run is
not proof that unreadable data is absent, and native-recovery findings may lack
their original folder or duplicate an indexed message. Empty mbox files can
simply represent empty source folders. Review the report before deleting the
source or its backup.

The Windows GUI is a thin front end for the same CLI. Keep `ostom-gui.exe` and
the matching `ostom.exe` in one directory. The GUI targets the system .NET 8
Desktop Runtime; install the x64 .NET 8 Desktop Runtime if Windows reports that
the application cannot start because a framework is missing.

## Build and documentation

### Large-file smoke test

Create a two-gigabyte, parser-readable test file with a sparse zero-filled
tail:

```powershell
.\tools\make-large-fixture.ps1
$env:OSTOM_LARGE_PST = (Join-Path (Get-Location) 'testdata\fixtures\large-2gb.pst')
go test -trimpath -run TestLargePSTRecoverySmoke ./cmd/ostom-fixture
```

This tests file-size handling, address-based reads, limits, and recovery
progress. It is not a two-gigabyte mailbox: the valid PST content remains the
small source fixture, and the tail contains no messages.

To exercise the same run with a bounded Go runtime heap, set a comfortable
soft limit before starting the test:

```powershell
$env:GOMEMLIMIT = '512MiB'
go test -trimpath -run TestLargePSTRecoverySmoke ./cmd/ostom-fixture
Remove-Item Env:GOMEMLIMIT
```

`GOMEMLIMIT` is a soft runtime limit, not a promise that the whole Windows
process can never exceed that amount. OSTOM also keeps recovery bounded by
payload, block, node, message, and attachment limits; the file size itself is
never used as a memory allocation size.

Recovery sessions are tied to the exact source contents. Before resuming,
OSTOM verifies the source size, modification time, header sample, and a full
streamed SHA-256. The full hash is memory-bounded but adds one sequential read
of a very large file.

Go 1.24 or newer is required:

```powershell
go build -trimpath -o ostom.exe ./cmd/ostom
go test -trimpath ./...
```

- [Format and recovery details](FORMAT.md)
- [Thunderbird import](THUNDERBIRD.md)
- [Development plan](TODO.md)
- [Third-party notices](THIRD_PARTY_NOTICES.md)

The GUI release is framework-dependent and single-file for Windows x64. It
requires the [.NET 8 Desktop Runtime for Windows x64](https://dotnet.microsoft.com/download/dotnet/8.0).

OSTOM is independent of Microsoft and does not send source data over the
network. Release binaries are currently unsigned; verify downloaded archives
with their published SHA-256 checksums.
