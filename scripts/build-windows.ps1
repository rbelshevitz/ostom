param(
    [string]$OutputDir = "dist\windows-amd64"
)

$ErrorActionPreference = "Stop"
New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null

$env:GOOS = "windows"
$env:GOARCH = "amd64"
go build -trimpath -o (Join-Path $OutputDir "ostom.exe") ./cmd/ostom
# WinForms supplies native Windows dialogs, layout, cancellation, and theme
# integration. Keep the GUI framework-dependent so the download stays small.
dotnet publish .\gui\Ostom.Gui\Ostom.Gui.csproj -c Release -r win-x64 --self-contained false -p:PublishSingleFile=true -o $OutputDir
if ($LASTEXITCODE -ne 0) { throw "dotnet publish failed with exit code $LASTEXITCODE" }

Write-Host "Built Windows CLI and GUI in $OutputDir"
