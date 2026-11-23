#!/usr/bin/env pwsh
#requires -Version 7.0

# Verified Windows installation requires PowerShell 7+ and the GitHub CLI.
# Authenticate gh before running this script so release attestations can be
# checked: pwsh -File scripts/install.ps1 -AddToPath.

[CmdletBinding()]
param(
    [Parameter()]
    [ValidatePattern('^v?[0-9]+\.[0-9]+\.[0-9]+$')]
    [string]$Version,

    [Parameter()]
    [ValidatePattern('^[^/\s]+/[^/\s]+$')]
    [string]$Repository = 'naterator/qbitctl',

    [Parameter()]
    [string]$InstallDir,

    [Parameter()]
    [switch]$AddToPath,

    [Parameter()]
    [string]$GhPath = 'gh'
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

function Get-NormalizedPath {
    param([Parameter(Mandatory)][string]$Path)

    $full = [System.IO.Path]::GetFullPath($Path)
    if ($full.Length -gt 3) {
        $full = $full.TrimEnd('\')
    }
    return $full
}

function Invoke-GitHubCli {
    param([Parameter(Mandatory)][string[]]$Arguments)

    $output = & $script:GitHubCli @Arguments 2>&1
    if ($LASTEXITCODE -ne 0) {
        $message = ($output | ForEach-Object { $_.ToString() }) -join [Environment]::NewLine
        if ([string]::IsNullOrWhiteSpace($message)) {
            $message = "exit code $LASTEXITCODE"
        }
        throw "GitHub CLI failed: gh $($Arguments -join ' '): $message"
    }
    return $output
}

function Get-ArchitectureName {
    $architecture = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
    switch ($architecture) {
        'X64' { return 'amd64' }
        'Arm64' { return 'arm64' }
        default { throw "Unsupported Windows architecture '$architecture'. qbitctl publishes amd64 and arm64 assets." }
    }
}

function Get-ReleaseTag {
    if (-not [string]::IsNullOrWhiteSpace($Version)) {
        return $Version
    }

    $json = (Invoke-GitHubCli @('release', 'view', '--repo', $Repository, '--json', 'tagName')) -join ''
    $release = $json | ConvertFrom-Json
    if ([string]::IsNullOrWhiteSpace($release.tagName)) {
        throw 'GitHub CLI did not return a latest release tag.'
    }
    $tag = [string]$release.tagName
    if ($tag -notmatch '^v?[0-9]+\.[0-9]+\.[0-9]+$') {
        throw "Latest release tag '$tag' is not a semantic version."
    }
    return $tag
}

function Get-ImmutableRelease {
    param([Parameter(Mandatory)][string]$Tag)

    $json = (Invoke-GitHubCli @('release', 'view', $Tag, '--repo', $Repository, '--json', 'tagName,isDraft,isImmutable,isPrerelease')) -join ''
    $release = $json | ConvertFrom-Json
    if ([string]$release.tagName -ne $Tag) {
        throw "GitHub returned release '$($release.tagName)' instead of '$Tag'."
    }
    if ([bool]$release.isDraft) {
        throw "Release '$Tag' is a draft and cannot be installed."
    }
    if ([bool]$release.isPrerelease) {
        throw "Release '$Tag' is a prerelease and cannot be installed by the stable installer."
    }
    if (-not [bool]$release.isImmutable) {
        throw "Release '$Tag' is not immutable; refusing to install an unprotected release."
    }
    return $release
}

function Read-Checksum {
    param(
        [Parameter(Mandatory)][string]$Path,
        [Parameter(Mandatory)][string]$AssetName
    )

    $content = Get-Content -LiteralPath $Path -Raw
    $trimmed = $content.Trim()
    $match = [regex]::Match($trimmed, '^(?<hash>[0-9a-fA-F]{64})\s+\*?(?<name>[^\s]+)$')
    if (-not $match.Success -or $match.Groups['name'].Value -ne $AssetName) {
        throw "Checksum file does not contain exactly one SHA-256 record for $AssetName."
    }
    return $match.Groups['hash'].Value.ToLowerInvariant()
}

function Verify-Sha256 {
    param(
        [Parameter(Mandatory)][string]$Path,
        [Parameter(Mandatory)][string]$ChecksumPath,
        [Parameter(Mandatory)][string]$AssetName
    )

    $expected = Read-Checksum -Path $ChecksumPath -AssetName $AssetName
    $actual = (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -ne $expected) {
        throw "SHA-256 mismatch for $AssetName."
    }
}

function Extract-VerifiedBinary {
    param(
        [Parameter(Mandatory)][string]$ArchivePath,
        [Parameter(Mandatory)][string]$DestinationPath
    )

    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $archive = $null
    $input = $null
    $output = $null
    try {
        $archive = [System.IO.Compression.ZipFile]::OpenRead($ArchivePath)
        $entries = @($archive.Entries)
        if ($entries.Count -ne 1 -or $entries[0].FullName -ne 'qbitctl.exe' -or $entries[0].Name -ne 'qbitctl.exe') {
            throw 'Windows release ZIP must contain exactly one regular qbitctl.exe entry.'
        }
        $entry = $entries[0]
        if ($entry.Length -le 0 -or $entry.Length -gt (256MB)) {
            throw 'Windows release ZIP contains an empty or oversized executable.'
        }

        $input = $entry.Open()
        $output = [System.IO.File]::Open($DestinationPath, [System.IO.FileMode]::CreateNew, [System.IO.FileAccess]::Write, [System.IO.FileShare]::None)
        $input.CopyTo($output)
        $output.Flush()
        $output.Close()
        $output = $null
        $input.Close()
        $input = $null
        if ((Get-Item -LiteralPath $DestinationPath).Length -le 0) {
            throw 'Windows release ZIP extracted an empty executable.'
        }
    } catch {
        Remove-Item -LiteralPath $DestinationPath -Force -ErrorAction SilentlyContinue
        throw "Could not safely extract qbitctl.exe: $($_.Exception.Message)"
    } finally {
        if ($null -ne $output) { $output.Dispose() }
        if ($null -ne $input) { $input.Dispose() }
        if ($null -ne $archive) { $archive.Dispose() }
    }
}

function Replace-InstalledBinary {
    param(
        [Parameter(Mandatory)][string]$SourcePath,
        [Parameter(Mandatory)][string]$TargetPath
    )

    $targetDir = Split-Path -Parent $TargetPath
    New-Item -ItemType Directory -Path $targetDir -Force | Out-Null
    $staged = Join-Path $targetDir ('.qbitctl-install-' + [guid]::NewGuid().ToString('N') + '.exe')
    try {
        Copy-Item -LiteralPath $SourcePath -Destination $staged -Force
        if (Test-Path -LiteralPath $TargetPath -PathType Leaf) {
            # File.Replace is atomic on Windows and leaves the destination
            # untouched when the replacement fails (for example, while a
            # running qbitctl process still holds the file open).
            [System.IO.File]::Replace($staged, $TargetPath, [NullString]::Value)
        } else {
            [System.IO.File]::Move($staged, $TargetPath)
        }
    } catch {
        Remove-Item -LiteralPath $staged -Force -ErrorAction SilentlyContinue
        throw "Could not replace ${TargetPath}: $($_.Exception.Message)"
    } finally {
        Remove-Item -LiteralPath $staged -Force -ErrorAction SilentlyContinue
    }
}

function Add-UserPathEntry {
    param([Parameter(Mandatory)][string]$Directory)

    $normalized = Get-NormalizedPath $Directory
    $current = [Environment]::GetEnvironmentVariable('Path', 'User')
    $parts = @()
    if (-not [string]::IsNullOrWhiteSpace($current)) {
        $parts = @($current -split ';' | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
    }
    foreach ($part in $parts) {
        try {
            if ([StringComparer]::OrdinalIgnoreCase.Equals((Get-NormalizedPath $part.Trim()), $normalized)) {
                Write-Host "PATH already contains $normalized."
                return
            }
        } catch {
            # Preserve an unrelated malformed PATH entry; it is not ours to
            # rewrite or interpret.
        }
    }
    [Environment]::SetEnvironmentVariable('Path', (($parts + $normalized) -join ';'), 'User')
    $env:Path = "$normalized;$env:Path"
    Write-Host "Added $normalized to the user PATH. Open a new terminal to use it."
}

function Main {
    $script:GitHubCli = (Get-Command $GhPath -ErrorAction Stop).Source
    $tag = Get-ReleaseTag
    Get-ImmutableRelease -Tag $tag | Out-Null
    $architecture = Get-ArchitectureName
    $asset = "qbitctl-windows-$architecture.zip"

    if ([string]::IsNullOrWhiteSpace($InstallDir)) {
        if ([string]::IsNullOrWhiteSpace($env:LOCALAPPDATA)) {
            throw 'LOCALAPPDATA is not set; pass -InstallDir explicitly.'
        }
        $InstallDir = Join-Path $env:LOCALAPPDATA 'qbitctl'
    }
    $InstallDir = Get-NormalizedPath $InstallDir

    $temporary = Join-Path ([IO.Path]::GetTempPath()) ('qbitctl-install-' + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $temporary -Force | Out-Null
    try {
        Invoke-GitHubCli @('release', 'download', $tag, '--repo', $Repository, '--pattern', $asset, '--pattern', "$asset.sha256", '--dir', $temporary, '--clobber') | Out-Null
        $archivePath = Join-Path $temporary $asset
        $checksumPath = "$archivePath.sha256"
        if (-not (Test-Path -LiteralPath $archivePath -PathType Leaf) -or -not (Test-Path -LiteralPath $checksumPath -PathType Leaf)) {
            throw "Release '$tag' did not provide both $asset and $asset.sha256."
        }

        Verify-Sha256 -Path $archivePath -ChecksumPath $checksumPath -AssetName $asset
        Invoke-GitHubCli @('release', 'verify-asset', $tag, $archivePath, '--repo', $Repository) | Out-Null

        $extractedPath = Join-Path $temporary 'qbitctl.exe'
        Extract-VerifiedBinary -ArchivePath $archivePath -DestinationPath $extractedPath
        $targetPath = Join-Path $InstallDir 'qbitctl.exe'
        Replace-InstalledBinary -SourcePath $extractedPath -TargetPath $targetPath

        if ($AddToPath) {
            Add-UserPathEntry -Directory $InstallDir
        } else {
            Write-Host "Installed qbitctl $tag to $targetPath."
            Write-Host "PATH was not changed. Re-run with -AddToPath to add $InstallDir to the user PATH."
        }
    } finally {
        Remove-Item -LiteralPath $temporary -Recurse -Force -ErrorAction SilentlyContinue
    }
}

try {
    Main
} catch {
    Write-Error $_.Exception.Message
    exit 1
}
