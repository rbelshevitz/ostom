[CmdletBinding()]
param(
    [ValidateRange(1, 4096)]
    [int]$SizeMb = 100,
    [string]$OutputPath = "",
    [switch]$Force
)

$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot
$builderCommit = 'a0d3f9c488f789c1f585f3c0d0083c9cd4bcd718'
$builderRepo = 'https://github.com/ml-ls/PST-Builder'
$builderZipUrl = "$builderRepo/archive/$builderCommit.zip"

if ([string]::IsNullOrWhiteSpace($OutputPath)) {
    $OutputPath = Join-Path $repoRoot "testdata\generated\large-$SizeMb`mb.pst"
} elseif (-not [IO.Path]::IsPathRooted($OutputPath)) {
    $OutputPath = Join-Path $repoRoot $OutputPath
}
$OutputPath = [IO.Path]::GetFullPath($OutputPath)
$outputDirectory = Split-Path -Parent $OutputPath

if ((Test-Path -LiteralPath $OutputPath) -and -not $Force) {
    throw "Output already exists: $OutputPath. Use -Force to replace it."
}

$workRoot = Join-Path ([IO.Path]::GetTempPath()) "ostom-pst-builder-$([guid]::NewGuid().ToString('N'))"
$zipPath = Join-Path $workRoot 'PST-Builder.zip'
$sourceRoot = Join-Path $workRoot "PST-Builder-$builderCommit"
$projectPath = Join-Path $sourceRoot 'samples\PstBuilder.LargeTest\PstBuilder.LargeTest.csproj'

try {
    New-Item -ItemType Directory -Force -Path $workRoot, $outputDirectory | Out-Null
    Write-Host "Downloading PST-Builder $builderCommit..."
    Invoke-WebRequest -Uri $builderZipUrl -OutFile $zipPath
    Expand-Archive -LiteralPath $zipPath -DestinationPath $workRoot

    if (-not (Test-Path -LiteralPath $projectPath)) {
        throw "PST-Builder source layout changed; project was not found: $projectPath"
    }

    if (Test-Path -LiteralPath $OutputPath) {
        Remove-Item -LiteralPath $OutputPath -Force
    }
    Write-Host "Generating PST of at least $SizeMb MiB..."
    dotnet run --project $projectPath --configuration Release -- --size $SizeMb $OutputPath
    if ($LASTEXITCODE -ne 0) {
        throw "PST-Builder exited with code $LASTEXITCODE"
    }

    if (-not (Test-Path -LiteralPath $OutputPath)) {
        throw "PST-Builder did not create the output file: $OutputPath"
    }
    $size = (Get-Item -LiteralPath $OutputPath).Length
    if ($size -lt ([int64]$SizeMb * 1MB)) {
        throw "Generated PST is too small: $size bytes"
    }

    Write-Host "Checking the generated PST with OSTOM..."
    $oldFixture = $env:OSTOM_LARGE_PST
    try {
        $env:OSTOM_LARGE_PST = $OutputPath
        go test -trimpath ./cmd/ostom-fixture -run TestLargePSTRecoverySmoke -count=1
        if ($LASTEXITCODE -ne 0) {
            throw "OSTOM large-PST test exited with code $LASTEXITCODE"
        }
    } finally {
        $env:OSTOM_LARGE_PST = $oldFixture
    }

    Write-Host "Generated and verified: $OutputPath ($size bytes)"
} finally {
    if (Test-Path -LiteralPath $workRoot) {
        Remove-Item -LiteralPath $workRoot -Recurse -Force
    }
}
