[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Source,
    [string]$ExportDirectory = "",
    [int]$MinimumNodes = 0,
    [int]$MinimumMessages = 0,
    [int]$MinimumAttachments = 0,
    [switch]$NativeOnly,
    [switch]$SkipCli
)

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$Source = [IO.Path]::GetFullPath($Source)
if (-not (Test-Path -LiteralPath $Source -PathType Leaf)) {
    throw "OST file does not exist: $Source"
}

$env:OSTOM_EXTERNAL_OST = $Source
$env:OSTOM_EXTERNAL_MIN_NODES = [string]$MinimumNodes
$env:OSTOM_EXTERNAL_MIN_MESSAGES = [string]$MinimumMessages
$env:OSTOM_EXTERNAL_MIN_ATTACHMENTS = [string]$MinimumAttachments

Push-Location $repoRoot
try {
    Write-Host "Running external OST format smoke..."
    go test -trimpath ./internal/format -run '^TestExternal4KNativeRecoverySmoke$' | Select-Object -First 40
    if ($LASTEXITCODE -ne 0) { throw "external OST format smoke failed with code $LASTEXITCODE" }

    if ($SkipCli) { return }
    if ([string]::IsNullOrWhiteSpace($ExportDirectory)) {
        $ExportDirectory = Join-Path ([IO.Path]::GetTempPath()) "ostom-external-ost-$([guid]::NewGuid().ToString('N'))"
        $removeExport = $true
    } else {
        $ExportDirectory = [IO.Path]::GetFullPath($ExportDirectory)
        $removeExport = $false
    }
    New-Item -ItemType Directory -Force -Path $ExportDirectory | Out-Null
    try {
        Write-Host "Running external OST native EML smoke (native-only=$NativeOnly)..."
        $cliArgs = @('-trimpath', './cmd/ostom', '-quiet', '-recovery-scan', '-eml', '-export-dir', $ExportDirectory)
        if ($NativeOnly) { $cliArgs += '-native-only' }
        $cliArgs += $Source
        go run @cliArgs | Select-Object -First 40
        if ($LASTEXITCODE -ne 0 -and $LASTEXITCODE -ne 3) {
            throw "external OST CLI smoke failed with code $LASTEXITCODE"
        }
        $emlFiles = @(Get-ChildItem -LiteralPath $ExportDirectory -Recurse -Filter '*.eml' -File)
        if ($emlFiles.Count -lt $MinimumMessages) {
            throw "external OST CLI exported $($emlFiles.Count) EML files; expected at least $MinimumMessages"
        }
        $manifest = Join-Path $ExportDirectory 'recovery-manifest.jsonl'
        if (-not (Test-Path -LiteralPath $manifest -PathType Leaf)) {
            throw "external OST CLI did not write recovery-manifest.jsonl"
        }
        $indexedDirectory = Join-Path $ExportDirectory 'Indexed'
        if ($NativeOnly -and (Test-Path -LiteralPath $indexedDirectory)) {
            throw "native-only smoke unexpectedly created Indexed output"
        }
        Write-Host "External OST smoke passed: EML=$($emlFiles.Count), manifest=$manifest"
    } finally {
        if ($removeExport -and (Test-Path -LiteralPath $ExportDirectory)) {
            Remove-Item -LiteralPath $ExportDirectory -Recurse -Force
        }
    }
} finally {
    Pop-Location
    Remove-Item Env:OSTOM_EXTERNAL_OST -ErrorAction SilentlyContinue
    Remove-Item Env:OSTOM_EXTERNAL_MIN_NODES -ErrorAction SilentlyContinue
    Remove-Item Env:OSTOM_EXTERNAL_MIN_MESSAGES -ErrorAction SilentlyContinue
    Remove-Item Env:OSTOM_EXTERNAL_MIN_ATTACHMENTS -ErrorAction SilentlyContinue
}
