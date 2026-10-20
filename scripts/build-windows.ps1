param(
    [string]$OutputDir = "dist\windows-amd64"
)

$ErrorActionPreference = "Stop"
New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null

$env:GOOS = "windows"
$env:GOARCH = "amd64"
go build -trimpath -o (Join-Path $OutputDir "ostom.exe") ./cmd/ostom
# WinForms supplies native Windows dialogs, layout, cancellation, and theme
# integration. Publish self-contained so the GUI starts on supported Windows
# 10/11 machines without a separate .NET Desktop Runtime installation.
dotnet publish .\gui\Ostom.Gui\Ostom.Gui.csproj -c Release -r win-x64 --self-contained true -p:PublishSingleFile=true -p:EnableCompressionInSingleFile=true -o $OutputDir
if ($LASTEXITCODE -ne 0) { throw "dotnet publish failed with exit code $LASTEXITCODE" }

Write-Host "Built Windows CLI and GUI in $OutputDir"
