[CmdletBinding()]
param(
    [string]$OutputPath = "",
    [ValidateRange(1, 256)]
    [int]$AttachmentSizeMb = 8,
    [switch]$IndexedRecovery,
    [switch]$NativeOnly,
    [switch]$Force
)

$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot
$builderCommit = 'a0d3f9c488f789c1f585f3c0d0083c9cd4bcd718'
$builderRepo = 'https://github.com/ml-ls/PST-Builder'
$builderZipUrl = "$builderRepo/archive/$builderCommit.zip"

if ([string]::IsNullOrWhiteSpace($OutputPath)) {
    $OutputPath = Join-Path $repoRoot "testdata\generated\attachment-smoke.pst"
} elseif (-not [IO.Path]::IsPathRooted($OutputPath)) {
    $OutputPath = Join-Path $repoRoot $OutputPath
}
$OutputPath = [IO.Path]::GetFullPath($OutputPath)
$outputDirectory = Split-Path -Parent $OutputPath

if ((Test-Path -LiteralPath $OutputPath) -and -not $Force) {
    throw "Output already exists: $OutputPath. Use -Force to replace it."
}

$workRoot = Join-Path ([IO.Path]::GetTempPath()) "ostom-pst-attachment-$([guid]::NewGuid().ToString('N'))"
$zipPath = Join-Path $workRoot 'PST-Builder.zip'
$sourceRoot = Join-Path $workRoot "PST-Builder-$builderCommit"
$projectPath = Join-Path $workRoot 'AttachmentSmoke.csproj'
$programPath = Join-Path $workRoot 'Program.cs'
$payloadPath = Join-Path $workRoot 'large-attachment.bin'
$exportPath = Join-Path $workRoot 'ostom-export'

try {
    New-Item -ItemType Directory -Force -Path $workRoot, $outputDirectory | Out-Null
    Write-Host "Downloading PST-Builder $builderCommit..."
    Invoke-WebRequest -Uri $builderZipUrl -OutFile $zipPath
    Expand-Archive -LiteralPath $zipPath -DestinationPath $workRoot

    $project = @"
<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType>
    <TargetFramework>net9.0</TargetFramework>
    <ImplicitUsings>enable</ImplicitUsings>
    <Nullable>enable</Nullable>
    <EnableDefaultCompileItems>false</EnableDefaultCompileItems>
  </PropertyGroup>
  <ItemGroup>
    <Compile Include="Program.cs" />
  </ItemGroup>
  <ItemGroup>
    <ProjectReference Include="$sourceRoot\src\PstBuilder\PstBuilder.csproj" />
    <ProjectReference Include="$sourceRoot\src\PstBuilder.Eml\PstBuilder.Eml.csproj" />
  </ItemGroup>
</Project>
"@
    Set-Content -LiteralPath $projectPath -Value $project -Encoding UTF8

    $program = @"
using PstBuilder;
using PstBuilder.Eml;
using PstBuilder.Messaging;
using MimeKit;

if (args.Length != 2) throw new ArgumentException("output and attachment paths are required");
var output = args[0];
var attachmentPath = args[1];
using var session = PstExportSession.Create(output);

static byte[] MakeMessage(string subject, string attachmentPath)
{
    var message = new MimeMessage();
    message.From.Add(new MailboxAddress("OSTOM", "ostom@example.test"));
    message.To.Add(new MailboxAddress("Recovery", "recovery@example.test"));
    message.Subject = subject;
    var body = new BodyBuilder { TextBody = "attachment smoke" };
    body.Attachments.Add(attachmentPath);
    message.Body = body.ToMessageBody();
    using var stream = new MemoryStream();
    message.WriteTo(stream);
    return stream.ToArray();
}

session.AddEml("Inbox", MakeMessage("OSTOM attachment smoke", attachmentPath));
session.AddEml("Sent Items", MakeMessage("OSTOM small attachment smoke", attachmentPath));

session.Complete();
"@
    Set-Content -LiteralPath $programPath -Value $program -Encoding UTF8

    $payloadSize = [int64]$AttachmentSizeMb * 1MB
    $payload = New-Object byte[] (1MB)
    for ($index = 0; $index -lt $payload.Length; $index++) { $payload[$index] = [byte]($index % 251) }
    $file = [IO.File]::Open($payloadPath, [IO.FileMode]::Create, [IO.FileAccess]::Write, [IO.FileShare]::None)
    try {
        for ($written = 0L; $written -lt $payloadSize; $written += $payload.Length) {
            $count = [int][Math]::Min($payload.Length, $payloadSize - $written)
            $file.Write($payload, 0, $count)
        }
    } finally { $file.Close() }

    Write-Host "Generating attachment-bearing PST ($AttachmentSizeMb MiB payload)..."
    dotnet run --project $projectPath --configuration Release -- $OutputPath $payloadPath
    if ($LASTEXITCODE -ne 0) { throw "attachment PST builder exited with code $LASTEXITCODE" }

    Write-Host "Running OSTOM export (indexed-recovery=$IndexedRecovery, native-only=$NativeOnly)..."
    New-Item -ItemType Directory -Force -Path $exportPath | Out-Null
    if ($NativeOnly) {
        go run -trimpath ./cmd/ostom -quiet -recovery-scan -native-only -eml -export-dir $exportPath $OutputPath
    } elseif ($IndexedRecovery) {
        go run -trimpath ./cmd/ostom -indexed-worker -indexed-recovery -eml -export-dir $exportPath $OutputPath
    } else {
        go run -trimpath ./cmd/ostom -quiet -recovery-scan -eml -export-dir $exportPath $OutputPath
    }
    $ostomExit = $LASTEXITCODE
    if ($ostomExit -ne 0 -and $ostomExit -ne 3) { throw "OSTOM attachment smoke exited with code $ostomExit" }

    $emlFiles = @(Get-ChildItem -LiteralPath $exportPath -Recurse -Filter '*.eml' -File)
    if ($emlFiles.Count -lt 1) { throw "OSTOM produced no EML files" }
    $attachmentHeaders = @($emlFiles | Select-String -Pattern 'large-attachment.bin' -SimpleMatch)
    if ($attachmentHeaders.Count -lt 1) { throw "attachment filename was not preserved in exported EML" }
    if ($NativeOnly -and (Test-Path -LiteralPath (Join-Path $exportPath 'Indexed'))) {
        throw "native-only export unexpectedly created Indexed output"
    }

    $size = (Get-Item -LiteralPath $OutputPath).Length
    Write-Host "Attachment PST smoke passed: $OutputPath ($size bytes), EML=$($emlFiles.Count), attachment-matches=$($attachmentHeaders.Count)"
} finally {
    if (Test-Path -LiteralPath $workRoot) { Remove-Item -LiteralPath $workRoot -Recurse -Force }
}
