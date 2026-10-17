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

Make a backup copy of the OST/PST file and run OSTOM against the copy. Keep the
original unchanged. OSTOM opens the source read-only, but damaged data may be
unreadable and recovery is not guaranteed. OSTOM is an independent,
best-effort recovery utility, not a forensic tool.

The project is deliberately not a forensic suite. It does not claim chain of custody, deleted-record recovery, password cracking, DPAPI/EFS/BitLocker decryption, or a guarantee that every damaged item can be reconstructed. The source store is opened read-only and all recovery results are best-effort.

## Quick start

If you only need to rescue mail, you do not need to understand the file format
or build the source code. Download the latest [Windows amd64 release](https://github.com/rbelshevitz/ostom/releases), close Outlook, and make a copy of the OST/PST file.

1. Inspect the copy and check the destination disk:

   ```powershell
   .\ostom.exe --output-dir E:\recovered source-copy.ost
   ```

2. Read the result in the console. If it says that an extended recovery scan
   is recommended or required, run:

   ```powershell
   .\ostom.exe --recovery-scan --output-dir E:\recovered source-copy.ost
   ```

3. Export readable mail to Thunderbird:

   ```powershell
   .\ostom.exe --export-dir E:\recovered source-copy.ost
   ```

   If you also want the extended recovery output, combine both options:

   ```powershell
   .\ostom.exe --recovery-scan --export-dir E:\recovered source-copy.ost
   ```

4. Close Thunderbird, copy the generated `Local Folders` directory into the
   Thunderbird profile's `Mail` directory, and start Thunderbird. Check the
   recovered messages and attachments before deleting anything.

## What “encrypted OST” means here

The PFF header has a `bCryptMethod` field. In the common OST/PST format this is normally `None`, `Permute`, or `Cyclic`: reversible block encoding used by the file format, not a user password and not modern disk encryption. OSTOM decodes these block bytes in memory because otherwise a valid external data block can be mistaken for an unknown payload. It never writes a decrypted copy of the source and does not attempt to defeat account, EFS, BitLocker, or other access controls. Unsupported or unknown methods are reported rather than silently guessed.

## What OSTOM does

OSTOM is not presented as a universal replacement for every PFF reader or
forensic recovery suite. Its practical difference is the recovery workflow:

- **Local-first:** physically present Sent, Drafts, Outbox, hidden, custom, and
  “This computer only” folders are treated as worth recovering.
- **Inspect before extracting:** header, allocation, NBT/BBT, folder, message,
  payload, and attachment evidence are reported separately.
- **Honest resource planning:** export space is estimated before a long export,
  with a clear insufficient-space result.
- **Partial results are first-class:** one bad node, message, or attachment does
  not discard the readable remainder; indexed and recovery-found items are kept
  separate.
- **Operationally useful on Windows:** bounded memory, honest `N/M` progress,
  idle watchdogs, JSON output, and a Thunderbird-ready folder tree are built in.
- **Explainable escalation:** extended recovery is recommended only with explicit
  evidence, not because of one advisory CRC mismatch.

These are focus and workflow advantages, not a claim that OSTOM currently
decodes more PFF variants than mature projects such as libpff. Native property,
table, and orphan-node salvage remains an active v0.2 milestone.

Projects such as `libpff` have a different role: they are general-purpose,
low-level PFF libraries and recovery references. OSTOM uses them as format and
behavioral references, but focuses on a local-first, explainable recovery path
for a person dealing with a damaged Outlook store. These projects should not
be compared as if they had the same product goal.

### What can be recovered

OSTOM preserves the folder hierarchy for readable indexed content and exports
as much recoverable mail and attachment data as the file still exposes. A bad
message or attachment should not discard the readable remainder. Damaged or
orphaned items may be incomplete, duplicated, missing their original folder
path, or placed in the separate `OSTOM Recovered` folder. The normal indexed
export and the extended recovery export are intentionally kept distinguishable.

## Current status

The current milestone implements a bounded-memory inspection and Thunderbird
export CLI on top of `github.com/grokify/outlook-pst-go`:

```text
ostom [--json] [--memory-limit 256] file.ost
```

## Build and run

Install Go 1.24 or newer, then run these commands from the repository root.
Go downloads the declared modules automatically on the first build:

```powershell
go build -o ostom.exe ./cmd/ostom
go build -o ostom-fixture.exe ./cmd/ostom-fixture
go test ./...
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

The export directory contains `Local Folders` mbox files. In Thunderbird,
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
Get-FileHash .\ostom-v0.2.2-windows-amd64.zip -Algorithm SHA256
```

An administrator may sign a private/internal build with an organization-owned
or self-signed Authenticode certificate and deploy that certificate to trusted
computers. This is free for internal use, but it does not create public trust
and may not remove SmartScreen warnings on unrelated computers. Public
distribution normally requires a certificate or signing service from a trusted
certificate authority. Never publish the private `.pfx` signing key.

### Command-line options

The main options are:

- `--json` — machine-readable report.
- `--output-dir DIR` — volume to check for required free space.
- `--export-dir DIR` — export indexed readable mail into a Thunderbird `Local Folders` tree.
- `--recovery-scan` — explicitly run the slower best-effort recovery probe.
- `--memory-limit MB` — soft process memory limit; default `256`, `0` disables it.
- `--idle-timeout DUR` — worker idle timeout; default `10m`, `0` disables it.

### Inspection and recovery

The report can recommend or require an extended recovery scan; `--recovery-scan` remains explicit because it may take a long time on a large OST.

When combined with `--export-dir`, `--recovery-scan` also enables the
dependency's lenient recovery parser for export. The report marks this as
`recovery_mode`; unreadable messages and attachments are still reported rather
than presented as successfully recovered.

The same command also writes messages found by the all-node recovery iterator
to `Local Folders\OSTOM Recovered`. This is deliberately a separate mbox:
recovered items may lack a reliable original folder and may duplicate an
indexed export. Their source node is recorded in `X-Ostom-Source`.

Use `--output-dir` during inspection to check the destination volume before recovery:

```text
ostom --output-dir E:\recovered file.ost
```

The report includes a conservative output-size estimate and marks the result `INSUFFICIENT_SPACE` when the target volume cannot hold it. Recovery must refuse to start in that state.

For a slower best-effort node recovery probe:

```text
ostom --recovery-scan --output-dir E:\recovered file.ost
```

This uses the separate `go-pst` recovery backend and reports attempted, successful, failed, orphan, and error-category counts. It still does not write output; extraction is the next stage.

The CLI runs the parser in a supervised worker process on Windows. By default,
the worker is terminated after 10 minutes without a heartbeat from message
processing. Change it for a large or slow store, or disable it explicitly:

```text
ostom --idle-timeout 30m file.ost
ostom --idle-timeout 0 file.ost
```

This is an idle timeout, not a total runtime limit. A timeout never modifies the
source file and returns `TIMEOUT` instead of presenting partial JSON as a
successful report.

### Test fixture generator

For a small local parser fixture:

```text
go run ./cmd/ostom-fixture -kind pst -out testdata\fixtures\sample.pst
go run ./cmd/ostom-fixture -kind ost -messages 50 -out testdata\fixtures\fifty.ost
go run ./cmd/ostom-fixture -kind pst -damage header-crc -out testdata\fixtures\sample-header-crc.pst
```

`ostom-fixture.exe` is a developer and test helper, not the recovery tool for
your mailbox. Ordinary users only need `ostom.exe`.

It exists because development and regression tests should not depend on a
private, damaged customer mailbox. The generator provides a repeatable small
fixture and controlled corruption cases so parser, export, progress, and
recovery behavior can be tested without modifying a real OST.

The generated OST is a parser-validated synthetic OST container, not a mailbox created by Outlook. With `-messages 50`, the generator selects the bundled Enron PST corpus, verifies that it contains at least 50 indexed messages, changes the PFF client magic, validates the result, and only then applies requested damage. The current source corpus contains 59 indexed messages and includes Sent folders. Its messages and addresses may be real historical data rather than invented identities, so the corpus and generated fixtures are for local testing only: they are not committed or included in releases. Check the upstream corpus terms before redistributing any copy.

### Thunderbird export

To export indexed readable messages directly into a Thunderbird-compatible `Local Folders` tree:

```text
ostom --export-dir E:\recovered file.ost
```

Attachments are associated through the message attachment table and copied through `go-pst`'s streaming attachment writer. Inline attachments retain `Content-ID` when available. The exporter writes no `.msf` files.

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

Human-readable scans show a 30-character progress bar, `N/M declared`, rate, and elapsed time. The denominator is collected before the message pass so it does not move backwards; it is the sum of folder-table declared counts and is itself evidence, not ground truth. If it cannot be established, the bar uses `?` instead of inventing a percentage. A watchdog warns after 60 seconds without message progress but does not forcibly kill a read in the middle of a damaged node.

The parser dependency is intentionally behind `internal/backend`. The final recovery path must provide streaming message/attachment readers; a backend that materializes an entire attachment is not acceptable for multi-gigabyte files.

OSTOM code is released under the MIT License. Licensing and attribution notes for dependencies are in `THIRD_PARTY_NOTICES.md`.

## When does Outlook stop recognizing the mail database?

Outlook can fail at several different layers. “The file is not an Outlook data file” or “the set of folders cannot be opened” does not identify the failed layer by itself.

| Layer | Typical failure | What OSTOM checks |
|---|---|---|
| File/header | truncation, wrong `!BDN`/`SO`, unsupported version, damaged header CRC | signature, type, version, page variant, header CRC where defined |
| Root/index | invalid NBT/BBT roots or unreadable index page | declared size, root offsets, page type, root-page shape |
| Allocation/page | damaged AMap/PMap, page type, CRC or back-pointer | known allocation pages and supported page footers |
| Folder tree | unreadable hierarchy table or child node | folder paths and structural errors |
| Message table | damaged contents table or message node | readable messages and failed folder/message paths |
| Block/payload | bad block size, back-pointer, decompression or attachment stream | declared versus readable payload and attachment errors |

Outlook may open a partially damaged file, or reject it before messages are visible. Microsoft documents both data-file repair and synchronization repair; the synchronization monitor can repair local sync state, scan the OST, and clear offline items, causing a full server resynchronization. [Microsoft synchronization monitor](https://learn.microsoft.com/en-us/microsoft-365-apps/outlook/configuration/sync-monitor-improvements)

Safe Mode is separate: it disables COM add-ins and Exchange client extensions. If Outlook works only in Safe Mode, an add-in is a likely cause, but this is not proof that the OST is corrupt. This is user context, not part of the file-integrity verdict. [Microsoft Outlook troubleshooting](https://support.microsoft.com/en-us/outlook/how-to-troubleshoot-performance-issues-in-outlook)

For Cached Exchange Mode, rebuilding an OST is a normal Microsoft remedy, but it is unsafe as a first recovery action when local-only Sent, Drafts, Outbox, or custom-folder content may not exist on the server. [Microsoft synchronization guidance](https://learn.microsoft.com/en-us/troubleshoot/outlook/synchronization/synchronization-issues-occur-in-outlook-owa)

OSTOM therefore reports evidence by layer instead of returning one unexplained “broken” flag. A parser failure remains “damaged or unsupported” until structural scanning distinguishes corruption from an unsupported 4 KiB/compressed variant.

### When does OSTOM recommend an extended recovery scan?

The report includes an explainable `deep_recovery` plan. It is `required` when a lower layer is unusable (header/declared-size failure, missing or invalid roots, index access failure) or the high-level walk recovers no messages despite declared content. It is `recommended` only when there is accumulated strong evidence, such as multiple payload decode/read failures, missing NBT-to-BBT links, or a substantial message-error ratio. A single parser error, allocation anomaly, or advisory trailer/CRC mismatch alone does not force escalation. The report also lists the reasons.

- The source OST/PST is never modified.
- Quick inspection may read indexed node blocks for structural classification, but does not reconstruct or export attachment streams.
- Declared attachment size is reported separately from bytes successfully read.
- A successful parser open is not treated as proof of full integrity.
- A recovery-backend panic is converted into a report error category; one malformed node must not terminate the whole inspection.
- Export will preserve physical folder parentage and store identity, including local-only folders.

Build requires network access to download Go modules. Until dependencies are available locally, source formatting and documentation can still be checked, but a complete build cannot be claimed.

## Documentation

- [TODO](TODO.md) — current implementation plan.
- [FORMAT](FORMAT.md) — PFF/OST/PST structure and diagnostic evidence.
- [THUNDERBIRD](THUNDERBIRD.md) — importing `Local Folders` output.
- [Third-party notices](THIRD_PARTY_NOTICES.md) — direct dependency licenses.

## Disclaimer

OSTOM is provided as an independent, best-effort recovery utility. Use it on
a copy of the source OST/PST, not on the only original. No recovery result is
guaranteed, and a report that opens successfully is not proof that every item
is intact.

The `ostom` CLI opens the source store read-only and does not upload its
contents. During inspection it writes no recovery files. When an explicit
`--export-dir` is supplied, it creates or appends Thunderbird-compatible files
only under that directory. `--output-dir` may also create the explicitly named
directory so its free space can be checked. The source store is never modified
by the CLI. The separate `ostom-fixture` developer tool writes only to its
explicit fixture path and may create a temporary sibling file while generating
the fixture.

OSTOM is not affiliated with or endorsed by Microsoft and is not a forensic
tool. You are responsible for preserving the original, checking exported
messages and attachments, and complying with applicable privacy and data-
handling requirements.
