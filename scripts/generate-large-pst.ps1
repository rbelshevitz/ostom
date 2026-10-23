[CmdletBinding()]
param(
    [ValidateRange(1, 4096)]
    [int]$SizeMb = 100,
    [ValidateRange(1, 1440)]
    [int]$SmokeTimeoutMinutes = 10,
    [string]$OutputPath = "",
    [string]$WorkDirectory = "",
    [switch]$Force,
    [switch]$SkipSmoke
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

if ([string]::IsNullOrWhiteSpace($WorkDirectory)) {
    $WorkDirectory = Join-Path $outputDirectory ".ostom-pst-builder"
} elseif (-not [IO.Path]::IsPathRooted($WorkDirectory)) {
    $WorkDirectory = Join-Path $repoRoot $WorkDirectory
}
$WorkDirectory = [IO.Path]::GetFullPath($WorkDirectory)
$workRoot = Join-Path $WorkDirectory "run-$([guid]::NewGuid().ToString('N'))"
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

    if ($SkipSmoke) {
        Write-Host "Generated without smoke: $OutputPath ($size bytes)"
        return
    }

    Write-Host "Checking the generated PST with OSTOM..."
    $oldFixture = $env:OSTOM_LARGE_PST
    $oldHeartbeat = $env:OSTOM_LARGE_PST_SMOKE_HEARTBEAT
    $smokeStdout = Join-Path $workRoot 'ostom-large-smoke.stdout.log'
    $smokeStderr = Join-Path $workRoot 'ostom-large-smoke.stderr.log'
    $smokeHeartbeat = Join-Path $workRoot 'ostom-large-smoke.heartbeat.log'
    try {
        $env:OSTOM_LARGE_PST = $OutputPath
        $env:OSTOM_LARGE_PST_SMOKE_HEARTBEAT = $smokeHeartbeat
        $smoke = Start-Process -FilePath 'go' -ArgumentList @(
            'test', '-trimpath', './cmd/ostom-fixture',
            '-run', 'TestLargePSTRecoverySmoke', '-count=1'
        ) -WorkingDirectory $repoRoot -RedirectStandardOutput $smokeStdout -RedirectStandardError $smokeStderr -PassThru
        $timeoutMs = [int64]$SmokeTimeoutMinutes * 60 * 1000
        if (-not $smoke.WaitForExit($timeoutMs)) {
            Write-Host "OSTOM large-PST smoke exceeded ${SmokeTimeoutMinutes} minute(s); stopping process tree..."
            & taskkill.exe /PID $smoke.Id /T /F | Out-Null
            if (Test-Path -LiteralPath $smokeStdout) {
                Write-Host 'OSTOM large-PST smoke stdout:'
                Get-Content -LiteralPath $smokeStdout
            }
            if (Test-Path -LiteralPath $smokeStderr) {
                Write-Host 'OSTOM large-PST smoke stderr:'
                Get-Content -LiteralPath $smokeStderr
            }
            if (Test-Path -LiteralPath $smokeHeartbeat) {
                Write-Host 'OSTOM large-PST smoke heartbeat:'
                Get-Content -LiteralPath $smokeHeartbeat
            }
            throw "OSTOM large-PST smoke timed out after $SmokeTimeoutMinutes minute(s)"
        }
        if (Test-Path -LiteralPath $smokeStdout) {
            Get-Content -LiteralPath $smokeStdout
        }
        if (Test-Path -LiteralPath $smokeStderr) {
            Get-Content -LiteralPath $smokeStderr
        }
        if (Test-Path -LiteralPath $smokeHeartbeat) {
            Write-Host 'OSTOM large-PST smoke heartbeat:'
            Get-Content -LiteralPath $smokeHeartbeat
        }
        if ($smoke.ExitCode -ne 0) {
            throw "OSTOM large-PST test exited with code $($smoke.ExitCode)"
        }
    } finally {
        $env:OSTOM_LARGE_PST = $oldFixture
        $env:OSTOM_LARGE_PST_SMOKE_HEARTBEAT = $oldHeartbeat
    }

    Write-Host "Generated and verified: $OutputPath ($size bytes)"
} finally {
    if (Test-Path -LiteralPath $workRoot) {
        Remove-Item -LiteralPath $workRoot -Recurse -Force
    }
}
