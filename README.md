# OSTOM

> Sometimes, all your mail in Outlook feels like a heavy load.

[![CI](https://github.com/rbelshevitz/ostom/actions/workflows/ci.yml/badge.svg)](https://github.com/rbelshevitz/ostom/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/doc/install)
[![.NET](https://img.shields.io/badge/.NET-8.0-512BD4?logo=dotnet&logoColor=white)](https://dotnet.microsoft.com/download/dotnet/8.0)

OSTOM is a read-only utility for recovering readable mail from Outlook PST/OST
files and exporting it to mbox and EML.

OSTOM reads Outlook data files without changing them. When an OST/PST is only
partly readable, it copies the mail and attachments it can recover into a
Thunderbird-compatible mbox tree, reports damaged parts, and leaves the source
file untouched.

In short, OSTOM is a local recovery tool with an honest report. Writing a new
“repaired” PST/OST file is outside its scope. It saves recovered messages and
attachments in open export formats instead.

<p align="center">
  <img src="docs/accountant-recovered-mail.png"
       alt="Happy accountant celebrating recovered email"
       width="900">
</p>

- the normal indexed export preserves readable folders and messages;
- a separate bounded recovery path can find additional messages when the index
  is damaged;
- messages and attachments are written as they are read, so one bad item does
  not discard the readable remainder;
- the report and manifest distinguish exported, partial, failed, and duplicate
  results.

## Quick start

Use a copy of the source file and keep the original unchanged.

The key idea is simple: open OSTOM's result in Thunderbird, then organize the
messages into IMAP folders there. First load the export into Thunderbird's
local `Local Folders` and check it. Then create the needed IMAP folders and
copy or move the messages there. OSTOM recovers readable data; Thunderbird
displays it and can upload it to the server. OSTOM never connects to IMAP or
modifies the source PST/OST.

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
them. See the [Thunderbird import guide](docs/THUNDERBIRD.md) for the full
local-then-IMAP workflow.

## Options

```text
ostom [-json] [-check|-analyze] [-progress-json] [-eml]
      [-output-dir DIR] [-export-dir DIR]
      [-recovery-scan] [-native-only] [-quiet] file.pst|file.ost
```

- `-check` — fast structure check without reading message content.
- `-analyze` — bounded content analysis without export.
- `-export-dir DIR` — destination for recovered mail. The normal export is
  written below `Local Folders`.
- `-output-dir DIR` — separate destination for `ostom-report.json` and, when
  recovery runs, `recovery-manifest.json`/`.csv`. If omitted, these files are
  written to `-export-dir`.
- `-recovery-scan` — run an additional recovery pass for damaged files.
- `-native-only` — run only the additional recovery pass.
- `-eml` — additionally write one `.eml` per recovered message on every
  selected export path, including native recovery.
- `-json` — write the final report as JSON.
- `-progress-json` — write machine-readable progress events to stderr.
- `-quiet` — suppress progress text.

`-recovery-scan` and `-native-only` require `-export-dir`.

## What to expect

OSTOM reports what was readable from this particular file. A successful run is
not proof that unreadable data is absent. Some recovered messages may not keep
their original folder. An empty mbox file can simply represent an empty source
folder. Review the report before deleting the source or its backup.

The Windows GUI is a thin front end for the same CLI. Keep `ostom-gui.exe` and
the matching `ostom.exe` in one directory. The GUI targets the system .NET 8
Desktop Runtime; install the x64 .NET 8 Desktop Runtime if Windows reports that
the application cannot start because a framework is missing.

## Build and documentation

Go 1.24 or newer is required:

```powershell
go build -trimpath -o ostom.exe ./cmd/ostom
go test -trimpath ./...
```

- [Format and recovery details](docs/FORMAT.md)
- [Thunderbird import](docs/THUNDERBIRD.md)
- [Development plan](docs/TODO.md)
- [Third-party notices](docs/THIRD_PARTY_NOTICES.md)

OSTOM is independent of Microsoft and does not send source data over the
network. Release binaries are currently unsigned; verify downloaded archives
with their published SHA-256 checksums.
