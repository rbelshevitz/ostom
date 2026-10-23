# OSTOM

Read-only extraction of readable mail from Outlook OST/PST files.

OSTOM is useful when an Outlook data file is still partly readable but should
not be trusted completely. It copies what can be read into a Thunderbird-
compatible mbox tree, keeps damaged parts visible, and never changes the source
file.

It is not a PST repair or PST writer. Its practical value is a safe, local
salvage pass with an honest report:

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
.\ostom.exe --json --output-dir E:\recovered source-copy.ost
```

Export readable mail:

```powershell
.\ostom.exe --export-dir E:\recovered source-copy.ost
```

If the file is damaged or the ordinary path is insufficient, force the
separate recovery pass:

```powershell
.\ostom.exe --recovery-scan --export-dir E:\recovered source-copy.ost
```

Close Thunderbird, copy the resulting `Local Folders` directory into its
profile, and check the recovered messages and attachments before relying on
them.

## Options

```text
ostom [--json] [--check|--analyze] [--progress-json] [--eml]
      [--output-dir DIR] [--export-dir DIR]
      [--recovery-scan] [--native-only] [--quiet] file.ost
```

- `--check` — fast structure check without reading message content.
- `--analyze` — bounded content analysis without export.
- `--export-dir DIR` — write the Thunderbird-compatible export.
- `--output-dir DIR` — write `ostom-report.json` there.
- `--recovery-scan` — force the separate bounded recovery path.
- `--native-only` — skip the optional indexed `go-pst` path.
- `--eml` — additionally write one `.eml` per recovered message.
- `--json` — write the final report as JSON.
- `--progress-json` — write machine-readable progress events to stderr.
- `--quiet` — suppress progress text.

`--recovery-scan` and `--native-only` require `--export-dir`. The indexed
`go-pst` path is optional; its timeout or error is reported and does not change
the read-only bounded recovery path.

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

Go 1.24 or newer is required:

```powershell
go build -trimpath -o ostom.exe ./cmd/ostom
go test -trimpath ./...
```

- [Format and recovery details](FORMAT.md)
- [Thunderbird import](THUNDERBIRD.md)
- [Development plan](TODO.md)
- [Third-party notices](THIRD_PARTY_NOTICES.md)

The GUI release is framework-dependent and therefore small. It requires the
[.NET 8 Desktop Runtime for Windows x64](https://dotnet.microsoft.com/download/dotnet/8.0).

OSTOM is independent of Microsoft and does not send source data over the
network. Release binaries are currently unsigned; verify downloaded archives
with their published SHA-256 checksums.
