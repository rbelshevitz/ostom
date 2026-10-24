# External OST corpus

The external smoke tests use a public GroupDocs `sample.ost` fixture. The
binary is deliberately not committed to this repository: it is downloaded
locally, is covered by the repository's `*.ost` ignore rule, and is only used
when `OSTOM_EXTERNAL_OST` is set.

## Long-run acceptance corpus

The separate `testdata/fixtures/fifty.ost` file is the local long-run
acceptance corpus. It is also ignored and is not the downloaded GroupDocs
sample above. Use it only for local development and acceptance testing; do not
commit, publish, or include it in release or CI artifacts. The known-good
baseline is:

- file: `testdata/fixtures/fifty.ost`
- size: `28967936` bytes
- SHA-256: `F3C998CFA2635C214EC538297BB29B59D43415445FB97D5EAE7C49DA229B992B`
- expected result: 2,178 messages, 168 attachments, zero partial messages,
  and zero attachment failures

Run the normal export against a copy of the fixture and inspect
`ostom-report.json`:

```powershell
go run -trimpath ./cmd/ostom -export-dir .\testdata\fixtures\fifty.ost.ostom-export .\testdata\fixtures\fifty.ost
```

Source page:

<https://docs.groupdocs.com/conversion/java/load-personal-storage-pst-or-ost-with-options/>

Direct download:

<https://docs.groupdocs.com/conversion/java/_sample_files/developer-guide/loading-documents/load-personal-storage-pst-or-ost-with-options/sample.ost>

Expected fixture:

- file: `sample.ost`
- size: `16818176` bytes
- SHA-256: `C83B72EA3B7ACD5CB14C8DDDD604CC147B0C7587FE9597FE383BDB4A5D73E7E2`
- observed format: Unicode OST, 4 KiB pages
- observed content: 12 indexed messages and 4 attachments

Download it from the repository root with:

```powershell
.\scripts\fetch-external-ost.ps1
$env:OSTOM_EXTERNAL_OST = "$PWD\testdata\external\sample.ost"
go test -trimpath ./internal/format -run 'TestExternal4K.*Smoke'
```
