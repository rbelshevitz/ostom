# ostom

[![CI](https://github.com/rbelshevitz/ostom/actions/workflows/ci.yml/badge.svg)](https://github.com/rbelshevitz/ostom/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/doc/install)

> A mailbox archaeologist for OST/PST files, recovering readable mail and carrying it to your favorite mail app.

`ostom` is a read-only Outlook OST/PST inspection and recovery tool written in Go.
Its primary purpose is recovering mail that exists only in a local store, including local Sent, Drafts, Outbox, hidden and non-standard folders. Folder names are never used as proof that data is server-backed.

Build requirement: Go 1.24 or newer. The CI matrix currently validates the
Windows build with Go 1.24.x.

OSTOM means **OST Mail Recovery**. The name describes the practical goal, not a forensic or cryptographic product claim: inspect a local Outlook store, recover readable items, preserve folder hierarchy, and export them for ordinary mail software such as Thunderbird.

OSTOM targets data files produced by **classic Outlook for Windows**, including
files associated with Outlook 2016, Outlook 2019, Outlook 2021, Outlook 2024,
and Outlook for Microsoft 365. This is a compatibility target, not a promise
that every release, account type, file variant, or damaged file will be fully
readable.

Microsoft, Outlook, Windows, Microsoft 365, and related names are trademarks of
the Microsoft group of companies. OSTOM is an independent project and is not
affiliated with, sponsored, or approved by Microsoft.

## Before you start

Use a working copy and keep the original unchanged. OSTOM opens the source
read-only and writes exports only to the destination you specify. Recovery is
best-effort, so review the exported messages and attachments before relying on
them.

## Quick start

If you only need to rescue mail, you do not need to understand the file format
or build the source code. Download the latest [Windows amd64 release](https://github.com/rbelshevitz/ostom/releases), close Outlook, and make a copy of the OST/PST file.

1. Inspect the copy:

   ```powershell
   .\ostom.exe source-copy.ost
   ```

2. Export readable mail to Thunderbird:

   ```powershell
   .\ostom.exe --export-dir E:\recovered source-copy.ost
   ```

3. If you also want to run the optional extended recovery pass, use:

   ```powershell
   .\ostom.exe --recovery-scan --export-dir E:\recovered source-copy.ost
   ```

4. Close Thunderbird, copy the generated `Local Folders` directory into the
   Thunderbird profile's `Mail` directory, and start Thunderbird. Check the
   recovered messages and attachments before deleting anything.

## What OSTOM does

OSTOM focuses on a practical recovery workflow:

- **Local-first:** physically present Sent, Drafts, Outbox, hidden, custom, and
  “This computer only” folders are treated as worth recovering.
- **Inspect before extracting:** file, folder, message, and attachment findings
  are reported separately.
- **Partial results are first-class:** one bad node, message, or attachment does
  not discard the readable remainder; indexed and recovery-found items are kept
  separate.
- **Operationally useful on Windows:** progress output, JSON reports, and a
  Thunderbird-ready folder tree are built in.
- **Optional recovery scan:** the report explains when a deeper scan may help.

Extended recovery keeps found items separate from the normal indexed export.
More about the file format and diagnostic details is in [FORMAT](FORMAT.md).

### What can be recovered

OSTOM preserves the folder hierarchy for readable indexed content and keeps
going when an individual message or attachment cannot be read. Native-recovery
results are kept separate because their metadata or original folder path may
be incomplete, and some may duplicate indexed items.

Indexed mail and readable attachments are exported in the usual folder
hierarchy. A public 4 KiB OST smoke test now yields 13 bounded native message
candidates; complete attachment export remains unproven
without failures. Extended recovery can find additional candidates, but complex
attachment layouts are still being explored.

### Attachment handling

Indexed attachments are streamed into the message export. Extended-recovery
attachments are associated only when the file provides explicit evidence.
`partial` and `unavailable` describe what was readable in this run. See
[FORMAT](FORMAT.md) for details on attachment structures and open cases.

## CLI syntax

```text
ostom [--json] [--output-dir DIR] [--export-dir DIR] [--recovery-scan] [--quiet] file.ost
```

## Build and run

Install Go 1.24 or newer, then run these commands from the repository root.
Go downloads the declared modules automatically on the first build:

```powershell
go build -trimpath -o ostom.exe ./cmd/ostom
go build -trimpath -o ostom-fixture.exe ./cmd/ostom-fixture
go test -trimpath ./...
go vet ./...
```

Run the inspection against a copy of the source file:

```powershell
.\ostom.exe --output-dir E:\recovered source-copy.ost
```

Export readable indexed mail into a Thunderbird-compatible tree:

```powershell
.\ostom.exe --export-dir E:\recovered source-copy.ost
```

The export directory contains `Local Folders` mbox files and an `OSTOM EML`
tree with one raw RFC 5322 fallback file per readable indexed message. Its
names include the folder path, export index, and source message identifier to
avoid collisions. In Thunderbird,
close the application, copy that `Local Folders` directory into the profile's
`Mail` directory, then start Thunderbird and let it rebuild folder indexes.
Keep the generated export as a backup until the messages and attachments have
been checked.

### Windows signature and SmartScreen

Release binaries are currently unsigned. Windows SmartScreen may therefore
show an “unrecognized app” warning when `ostom.exe` is started after download.
This does not by itself mean that the file is malware. Verify the archive
against the published `SHA256SUMS.txt` before running it; in PowerShell:

```powershell
Get-FileHash .\ostom-v0.2.5-windows-amd64.zip -Algorithm SHA256
```

### Command-line options

The main options are:

- `--json` — machine-readable report.
- `--output-dir DIR` — directory for `ostom-report.json`; defaults to
  `--export-dir` when set. It does not check free space.
- `--export-dir DIR` — export indexed readable mail into a Thunderbird `Local Folders` tree.
- `--recovery-scan` — force the additional, slower native recovery pass.
  Requires `--export-dir`; otherwise the pass is selected automatically only
  when the file assessment recommends it.
- `--quiet` — suppress progress messages on stderr; warnings and the final
  report are still printed.

### Inspection and recovery

The report assesses the file before export. Native recovery runs automatically
only when the assessment says it is recommended or required, and can always be
forced with `--recovery-scan`. It can take longer on large files; additional
candidates are kept separate from indexed mail.

Use `--output-dir` to choose where the JSON report is saved:

```text
ostom --output-dir E:\recovered file.ost
```

If omitted, the report is saved in `--export-dir` when exporting. This option
does not estimate output size or check available disk space.

To include the extended recovery scan:

```text
ostom --recovery-scan --export-dir E:\recovered file.ost
```

Additional candidates are written to `OSTOM Native Recovered` and kept separate
from indexed mail.

### Test fixture generator

For a small local parser fixture:

```text
go run ./cmd/ostom-fixture -kind pst -out testdata\fixtures\sample.pst
go run ./cmd/ostom-fixture -kind ost -messages 50 -out testdata\fixtures\fifty.ost
go run ./cmd/ostom-fixture -kind pst -damage header-crc -out testdata\fixtures\sample-header-crc.pst
```

`ostom-fixture.exe` is a developer helper that creates repeatable synthetic
test files. It is not needed for ordinary recovery. See [FORMAT](FORMAT.md) for
test-corpus provenance and public OST samples used for validation.

### Thunderbird export

To export indexed readable messages directly into a Thunderbird-compatible `Local Folders` tree:

```text
ostom --export-dir E:\recovered file.ost
```

Attachments are associated through the message attachment table and copied through `go-pst`'s streaming attachment writer. Inline attachments retain `Content-ID` when available. The exporter writes no `.msf` files.

The same export also creates `OSTOM EML`, a safe per-message fallback tree for
readable indexed messages. EML files are streamed alongside mbox output and
are atomically finalized only after the complete message has been read.

An empty mbox file is not automatically an export failure: OSTOM creates one
for every readable source folder so Thunderbird can preserve the hierarchy.
Empty source folders therefore remain zero bytes. Check the human summary or
the JSON fields `folders_with_mail`, `empty_folders`, `messages`,
`messages_attempted`, `skipped_messages`, and `errors` before deciding that an
export failed.

The export report includes complete aggregate counts for attempted, exported,
partial, and skipped messages, plus attachment failures. JSON also contains a
bounded sample of per-message statuses with the source folder and subject;
large stores report how many status records were omitted instead of retaining
an unbounded in-memory list.

### Reports, progress, and limits

It reports file/header recognition, parser/index access, folder and message counts, unread count, attachment count, declared attachment bytes, and errors encountered while walking the physical tree.

During export, the CLI reports indexed message counts and native recovery phases
to stderr. `--quiet` suppresses this progress output. The CLI does not impose a
process-wide memory cap or an idle timeout.

The CLI reports file, folder, message, and attachment counts and errors. See
[FORMAT](FORMAT.md) for how structural findings are interpreted.

OSTOM code is released under the MIT License. Licensing and attribution notes for dependencies are in `THIRD_PARTY_NOTICES.md`.

## Diagnosis and extended recovery

The report summarizes structural findings and may recommend an extended scan.
The scan is optional, takes longer, and keeps additional candidates separate
from indexed mail. See [FORMAT](FORMAT.md) for diagnostic layers and details on
when the report recommends it.

## Documentation

- [TODO](TODO.md) — current implementation plan.
- [FORMAT](FORMAT.md) — PFF/OST/PST structure and diagnostic evidence.
- [THUNDERBIRD](THUNDERBIRD.md) — importing `Local Folders` output.
- [Third-party notices](THIRD_PARTY_NOTICES.md) — direct dependency licenses.

## Disclaimer

OSTOM reads the source store without modifying or uploading it. Exports and
reports are written only to the directories you specify. Use a working copy
and review the result before relying on it. OSTOM is independent and not
affiliated with Microsoft.
