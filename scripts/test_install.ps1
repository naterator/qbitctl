#!/usr/bin/env pwsh
#requires -Version 7.0

[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$installer = Join-Path $scriptDir 'install.ps1'
$pwsh = (Get-Command pwsh -ErrorAction SilentlyContinue)
if ($null -eq $pwsh) {
    $pwsh = (Get-Command powershell -ErrorAction Stop)
}

function Assert-True {
    param([Parameter(Mandatory)][bool]$Condition, [Parameter(Mandatory)][string]$Message)
    if (-not $Condition) { throw "installer test: $Message" }
}

function Assert-Equal {
    param([Parameter(Mandatory)]$Actual, [Parameter(Mandatory)]$Expected, [Parameter(Mandatory)][string]$Message)
    if ($Actual -ne $Expected) { throw "installer test: $Message (actual '$Actual', expected '$Expected')" }
}

function New-ZipFixture {
    param(
        [Parameter(Mandatory)][string]$Destination,
        [Parameter(Mandatory)][hashtable]$Entries
    )

    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $stream = [System.IO.File]::Open($Destination, [IO.FileMode]::Create, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)
    $archive = [System.IO.Compression.ZipArchive]::new($stream, [IO.Compression.ZipArchiveMode]::Create, $false)
    try {
        foreach ($name in $Entries.Keys) {
            $entry = $archive.CreateEntry($name)
            $writer = [IO.StreamWriter]::new($entry.Open())
            try { $writer.Write([string]$Entries[$name]) } finally { $writer.Dispose() }
        }
    } finally {
        $archive.Dispose()
        $stream.Dispose()
    }
}

function Write-Checksum {
    param([Parameter(Mandatory)][string]$Path)
    $name = Split-Path -Leaf $Path
    $hash = (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
    Set-Content -LiteralPath "$Path.sha256" -Value "$hash  $name" -NoNewline
}

function Invoke-Installer {
    param([Parameter(Mandatory)][string[]]$Arguments)
    $output = & $pwsh.Source -NoLogo -NoProfile -File $installer @Arguments 2>&1
    [pscustomobject]@{
        ExitCode = $LASTEXITCODE
        Output   = (($output | ForEach-Object { $_.ToString() }) -join [Environment]::NewLine)
    }
}

$root = Join-Path ([IO.Path]::GetTempPath()) ('qbitctl-install-test-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $root -Force | Out-Null
$oldUserPath = $null
$hadUserPath = $false
$pathCaptured = $false
try {
    $architecture = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
    $assetArchitecture = switch ($architecture) {
        'X64' { 'amd64' }
        'Arm64' { 'arm64' }
        default { throw "unsupported test architecture '$architecture'" }
    }
    $asset = "qbitctl-windows-$assetArchitecture.zip"

    $releaseDir = Join-Path $root 'release'
    New-Item -ItemType Directory -Path $releaseDir -Force | Out-Null
    $payload = Join-Path $root 'payload'
    New-Item -ItemType Directory -Path $payload -Force | Out-Null
    Set-Content -LiteralPath (Join-Path $payload 'qbitctl.exe') -Value 'fixture-v1' -NoNewline
    $archivePath = Join-Path $releaseDir $asset
    [System.IO.Compression.ZipFile]::CreateFromDirectory($payload, $archivePath)
    Write-Checksum -Path $archivePath

    $fakeGh = Join-Path $root 'gh.ps1'
    @'
param([Parameter(ValueFromRemainingArguments = $true)][string[]]$Arguments)
$releaseDir = $env:QBITCTL_TEST_RELEASE_DIR
$command = $Arguments -join ' '
if ($env:QBITCTL_TEST_GH_LOG) { Add-Content -LiteralPath $env:QBITCTL_TEST_GH_LOG -Value $command }
if ($Arguments -contains 'verify-asset') {
    if ($env:QBITCTL_TEST_VERIFY -ne 'true') { [Console]::Error.WriteLine('fixture attestation failure'); exit 1 }
    exit 0
}
if ($Arguments -contains 'download') {
    $dir = $Arguments[([Array]::IndexOf($Arguments, '--dir') + 1)]
    Copy-Item -LiteralPath (Join-Path $releaseDir $env:QBITCTL_TEST_ASSET) -Destination (Join-Path $dir $env:QBITCTL_TEST_ASSET) -Force
    Copy-Item -LiteralPath (Join-Path $releaseDir ($env:QBITCTL_TEST_ASSET + '.sha256')) -Destination (Join-Path $dir ($env:QBITCTL_TEST_ASSET + '.sha256')) -Force
    exit 0
}
if ($Arguments -contains 'view') {
    if (($Arguments -join ',') -match 'isImmutable') {
        $immutable = if ($env:QBITCTL_TEST_IMMUTABLE -eq 'true') { 'true' } else { 'false' }
        Write-Output ('{"tagName":"1.3.2","isDraft":false,"isImmutable":' + $immutable + ',"isPrerelease":false}')
    } else {
        Write-Output '{"tagName":"1.3.2"}'
    }
    exit 0
}
exit 1
'@ | Set-Content -LiteralPath $fakeGh -NoNewline

    $env:QBITCTL_TEST_RELEASE_DIR = $releaseDir
    $env:QBITCTL_TEST_ASSET = $asset
    $env:QBITCTL_TEST_VERIFY = 'true'
    $env:QBITCTL_TEST_IMMUTABLE = 'true'
    $env:QBITCTL_TEST_GH_LOG = Join-Path $root 'gh.log'

    $installDir = Join-Path $root 'bin'
    $result = Invoke-Installer @('-GhPath', $fakeGh, '-InstallDir', $installDir)
    Assert-Equal $result.ExitCode 0 'verified installation failed'
    $target = Join-Path $installDir 'qbitctl.exe'
    Assert-True (Test-Path -LiteralPath $target -PathType Leaf) 'installer did not create qbitctl.exe'
    Assert-Equal (Get-Content -LiteralPath $target -Raw) 'fixture-v1' 'installed binary content is wrong'
    Assert-True ($result.Output -match 'PATH was not changed') 'installer changed PATH without opt-in'
    Assert-True ((Get-Content -LiteralPath $env:QBITCTL_TEST_GH_LOG -Raw) -match 'verify-asset') 'installer did not invoke gh release verify-asset'

    $verified = Get-Content -LiteralPath $target -Raw

    $env:QBITCTL_TEST_VERIFY = 'false'
    $result = Invoke-Installer @('-GhPath', $fakeGh, '-Version', '1.3.2', '-InstallDir', $installDir)
    Assert-True ($result.ExitCode -ne 0) 'attestation failure was accepted'
    Assert-Equal (Get-Content -LiteralPath $target -Raw) $verified 'attestation failure replaced the binary'

    $env:QBITCTL_TEST_VERIFY = 'true'
    Set-Content -LiteralPath $archivePath -Value 'not a zip' -NoNewline
    $result = Invoke-Installer @('-GhPath', $fakeGh, '-Version', '1.3.2', '-InstallDir', $installDir)
    Assert-True ($result.ExitCode -ne 0) 'SHA mismatch was accepted'
    Assert-Equal (Get-Content -LiteralPath $target -Raw) $verified 'SHA mismatch replaced the binary'

    Write-Checksum -Path $archivePath
    $result = Invoke-Installer @('-GhPath', $fakeGh, '-Version', '1.3.2', '-InstallDir', $installDir)
    Assert-True ($result.ExitCode -ne 0) 'corrupt ZIP was accepted'
    Assert-Equal (Get-Content -LiteralPath $target -Raw) $verified 'corrupt ZIP replaced the binary'

    New-ZipFixture -Destination $archivePath -Entries @{ 'qbitctl.exe' = 'unsafe'; 'notes.txt' = 'unexpected' }
    Write-Checksum -Path $archivePath
    $result = Invoke-Installer @('-GhPath', $fakeGh, '-Version', '1.3.2', '-InstallDir', $installDir)
    Assert-True ($result.ExitCode -ne 0) 'unsafe ZIP was accepted'
    Assert-Equal (Get-Content -LiteralPath $target -Raw) $verified 'unsafe ZIP replaced the binary'

    New-ZipFixture -Destination $archivePath -Entries @{ '../qbitctl.exe' = 'traversal' }
    Write-Checksum -Path $archivePath
    $result = Invoke-Installer @('-GhPath', $fakeGh, '-Version', '1.3.2', '-InstallDir', $installDir)
    Assert-True ($result.ExitCode -ne 0) 'path-traversal ZIP was accepted'
    Assert-Equal (Get-Content -LiteralPath $target -Raw) $verified 'path-traversal ZIP replaced the binary'

    Set-Content -LiteralPath (Join-Path $payload 'qbitctl.exe') -Value 'fixture-v2' -NoNewline
    Remove-Item -LiteralPath $archivePath -Force
    [System.IO.Compression.ZipFile]::CreateFromDirectory($payload, $archivePath)
    Write-Checksum -Path $archivePath
    $result = Invoke-Installer @('-GhPath', $fakeGh, '-Version', '1.3.2', '-InstallDir', $installDir)
    Assert-Equal $result.ExitCode 0 'verified replacement installation failed'
    Assert-Equal (Get-Content -LiteralPath $target -Raw) 'fixture-v2' 'verified replacement did not update the binary'
    $verified = Get-Content -LiteralPath $target -Raw

    $env:QBITCTL_TEST_IMMUTABLE = 'false'
    $result = Invoke-Installer @('-GhPath', $fakeGh, '-Version', '1.3.2', '-InstallDir', $installDir)
    Assert-True ($result.ExitCode -ne 0) 'mutable release was accepted'
    Assert-Equal (Get-Content -LiteralPath $target -Raw) $verified 'mutable release replaced the binary'
    $env:QBITCTL_TEST_IMMUTABLE = 'true'

    if ($IsWindows) {
        $oldUserPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        $hadUserPath = $null -ne $oldUserPath
        $pathCaptured = $true
        [Environment]::SetEnvironmentVariable('Path', '', 'User')
        $result = Invoke-Installer @('-GhPath', $fakeGh, '-Version', '1.3.2', '-InstallDir', $installDir, '-AddToPath')
        Assert-Equal $result.ExitCode 0 'PATH opt-in installation failed'
        $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        Assert-True (($userPath -split ';') -contains $installDir) 'PATH opt-in did not add the install directory'
        $result = Invoke-Installer @('-GhPath', $fakeGh, '-Version', '1.3.2', '-InstallDir', $installDir, '-AddToPath')
        Assert-Equal $result.ExitCode 0 'idempotent PATH installation failed'
        $count = @(([Environment]::GetEnvironmentVariable('Path', 'User') -split ';' | Where-Object { $_ -ieq $installDir })).Count
        Assert-Equal $count 1 'PATH opt-in duplicated the install directory'
    } else {
        Write-Host 'Skipping user PATH registry test outside Windows.'
    }

    Write-Host "installer tests passed for $architecture ($asset)"
} finally {
    if ($pathCaptured -and $IsWindows -and $hadUserPath) {
        [Environment]::SetEnvironmentVariable('Path', $oldUserPath, 'User')
    } elseif ($pathCaptured -and $IsWindows) {
        [Environment]::SetEnvironmentVariable('Path', $null, 'User')
    }
    foreach ($name in 'QBITCTL_TEST_RELEASE_DIR', 'QBITCTL_TEST_ASSET', 'QBITCTL_TEST_VERIFY', 'QBITCTL_TEST_IMMUTABLE', 'QBITCTL_TEST_GH_LOG') {
        Remove-Item -Path "Env:$name" -ErrorAction SilentlyContinue
    }
    Remove-Item -LiteralPath $root -Recurse -Force -ErrorAction SilentlyContinue
}
