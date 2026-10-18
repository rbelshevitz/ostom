# External OST corpus

The external smoke tests use a public GroupDocs `sample.ost` fixture. The
binary is deliberately not committed to this repository: it is downloaded
locally, is covered by the repository's `*.ost` ignore rule, and is only used
when `OSTOM_EXTERNAL_OST` is set.

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
go test ./internal/format -run 'TestExternal4K.*Smoke' -v
```
