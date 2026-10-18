[CmdletBinding()]
param(
    [switch]$Force
)

$ErrorActionPreference = 'Stop'

$url = 'https://docs.groupdocs.com/conversion/java/_sample_files/developer-guide/loading-documents/load-personal-storage-pst-or-ost-with-options/sample.ost'
$relativePath = 'testdata\external\sample.ost'
$expectedSha256 = 'C83B72EA3B7ACD5CB14C8DDDD604CC147B0C7587FE9597FE383BDB4A5D73E7E2'
$outputPath = Join-Path (Get-Location) $relativePath
$outputDirectory = Split-Path -Parent $outputPath

New-Item -ItemType Directory -Force -Path $outputDirectory | Out-Null

if ((Test-Path -LiteralPath $outputPath) -and -not $Force) {
    $existingHash = (Get-FileHash -LiteralPath $outputPath -Algorithm SHA256).Hash
    if ($existingHash -eq $expectedSha256) {
        Write-Host "Already present and verified: $outputPath"
        exit 0
    }
    throw "Existing file has SHA-256 $existingHash, expected $expectedSha256. Use -Force to replace it."
}

$temporaryPath = "$outputPath.download"
try {
    Invoke-WebRequest -Uri $url -OutFile $temporaryPath
    $downloadedHash = (Get-FileHash -LiteralPath $temporaryPath -Algorithm SHA256).Hash
    if ($downloadedHash -ne $expectedSha256) {
        throw "Downloaded file has SHA-256 $downloadedHash, expected $expectedSha256."
    }
    Move-Item -LiteralPath $temporaryPath -Destination $outputPath -Force
    Write-Host "Downloaded and verified: $outputPath"
}
finally {
    if (Test-Path -LiteralPath $temporaryPath) {
        Remove-Item -LiteralPath $temporaryPath -Force
    }
}
