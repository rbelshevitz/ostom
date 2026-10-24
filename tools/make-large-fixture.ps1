param(
    [string]$Source = "testdata\fixtures\sample.pst",
    [string]$Output = "testdata\fixtures\large-2gb.pst",
    [long]$Size = 2GB
)

$ErrorActionPreference = "Stop"
$sourcePath = [IO.Path]::GetFullPath($Source)
$outputPath = [IO.Path]::GetFullPath($Output)
$workspacePath = [IO.Path]::GetFullPath((Get-Location).Path)

if (-not $outputPath.StartsWith($workspacePath + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
    throw "Output must stay inside the workspace: $outputPath"
}
if (-not (Test-Path -LiteralPath $sourcePath -PathType Leaf)) {
    throw "Source PST does not exist: $sourcePath"
}
$sourceLength = (Get-Item -LiteralPath $sourcePath).Length
if ($Size -le $sourceLength) {
    throw "Target size must be larger than the source ($sourceLength bytes)"
}

New-Item -ItemType Directory -Force -Path (Split-Path -Parent $outputPath) | Out-Null
Copy-Item -LiteralPath $sourcePath -Destination $outputPath -Force
$stream = [IO.File]::Open($outputPath, [IO.FileMode]::Open, [IO.FileAccess]::ReadWrite, [IO.FileShare]::Read)
try {
    $stream.SetLength($Size)
}
finally {
    $stream.Dispose()
}

& fsutil.exe sparse setflag $outputPath | Out-Null
& fsutil.exe sparse setrange $outputPath $sourceLength ($Size - $sourceLength) | Out-Null

$length = (Get-Item -LiteralPath $outputPath).Length
Write-Output "created $outputPath ($length bytes; sparse tail from $sourceLength)"
