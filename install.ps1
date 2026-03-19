#Requires -Version 5.1
[CmdletBinding()]
param(
  [switch]$Uninstall
)

$ErrorActionPreference = 'Stop'

$Repo   = 'ntancardoso/gb'
$Binary = 'gb.exe'

function Write-Info    { Write-Host "==> $args" -ForegroundColor Cyan }
function Write-Success { Write-Host "[ok] $args" -ForegroundColor Green }
function Write-Warn    { Write-Host "warning: $args" -ForegroundColor Yellow }
function Write-Fatal   { Write-Host "error: $args" -ForegroundColor Red; exit 1 }

function Get-Arch {
  switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { return 'amd64' }
    'ARM64' { return 'arm64' }
    default { Write-Fatal "Unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
  }
}

function Get-LatestVersion {
  $response = Invoke-RestMethod "https://api.github.com/repos/$Repo/releases/latest"
  return $response.tag_name
}

function Get-InstalledVersion {
  # Check PATH first, then fall back to the known install dir
  $cmd = Get-Command gb -ErrorAction SilentlyContinue
  if (-not $cmd) {
    $knownBin = Join-Path (Get-InstallDir) $Binary
    if (Test-Path $knownBin) {
      $cmd = $knownBin
    } else {
      return $null
    }
  }
  try {
    $bin = if ($cmd -is [string]) { $cmd } else { $cmd.Source }
    $out = & $bin --version 2>&1
    if ($out -match '(\d+\.\d+\.\d+)') { return "v$($Matches[1])" }
  } catch {}
  return $null
}

function Get-InstallDir {
  # Prefer user-local dir to avoid requiring admin
  $localBin = "$env:LOCALAPPDATA\Programs\gb"
  return $localBin
}

function Add-ToUserPath {
  param([string]$Dir)
  $current = [Environment]::GetEnvironmentVariable('Path', 'User')
  if ($current -split ';' -contains $Dir) { return }
  [Environment]::SetEnvironmentVariable('Path', "$current;$Dir", 'User')
  $env:PATH = "$env:PATH;$Dir"
  Write-Warn "$Dir added to your user PATH (restart terminal to take effect)"
}

function Get-FileHash256 {
  param([string]$File)
  return (Get-FileHash -Algorithm SHA256 $File).Hash.ToLower()
}

function Confirm-Checksum {
  param([string]$Archive, [string]$ChecksumsFile)
  $name = Split-Path $Archive -Leaf
  $lines = Get-Content $ChecksumsFile
  $entry = $lines | Where-Object { $_ -match "\s$([regex]::Escape($name))$" } | Select-Object -First 1
  if (-not $entry) {
    Write-Warn "No checksum entry for $name, skipping verification"
    return
  }
  $expected = ($entry -split '\s+')[0]
  $actual   = Get-FileHash256 $Archive
  if ($actual -ne $expected) {
    Write-Fatal "Checksum mismatch!`n  expected: $expected`n  got:      $actual"
  }
  Write-Success "Checksum verified"
}

function Invoke-Uninstall {
  $cmd = Get-Command gb -ErrorAction SilentlyContinue
  $knownBin = Join-Path (Get-InstallDir) $Binary
  if (-not $cmd -and -not (Test-Path $knownBin)) {
    Write-Warn "gb is not installed"
    exit 0
  }
  $path = if ($cmd) { $cmd.Source } else { $knownBin }
  Write-Info "Removing $path..."
  Remove-Item $path -Force
  # Remove install dir if empty
  $dir = Split-Path $path
  if ((Get-ChildItem $dir -ErrorAction SilentlyContinue).Count -eq 0) {
    Remove-Item $dir -Force -ErrorAction SilentlyContinue
  }
  Write-Success "gb uninstalled"
}

function Main {
  if ($Uninstall) { Invoke-Uninstall; return }

  $arch = Get-Arch

  Write-Info "Fetching latest release..."
  $version = Get-LatestVersion
  if (-not $version) { Write-Fatal "Could not determine latest version" }

  $installedVersion = Get-InstalledVersion
  if ($installedVersion) {
    $cmd = Get-Command gb -ErrorAction SilentlyContinue
    $installedPath = if ($cmd) { $cmd.Source } else { Join-Path (Get-InstallDir) $Binary }
    if ($installedVersion -eq $version) {
      Write-Success "gb $version is already installed at $installedPath - nothing to do"
      exit 0
    }
    Write-Info "Updating gb $installedVersion -> $version  (at $installedPath)"
  } else {
    Write-Info "Installing gb $version"
  }

  $ver     = $version.TrimStart('v')
  $asset   = "gb_${ver}_windows_${arch}.zip"
  $baseUrl = "https://github.com/$Repo/releases/download/$version"

  $tmp          = [System.IO.Path]::GetTempPath() + [System.IO.Path]::GetRandomFileName()
  $tmpDir       = (New-Item -ItemType Directory -Path $tmp -Force).FullName
  $archivePath  = Join-Path $tmpDir $asset
  $checksumPath = Join-Path $tmpDir 'checksums.txt'

  try {
    Write-Info "Downloading $asset..."
    Invoke-WebRequest "$baseUrl/$asset"       -OutFile $archivePath  -UseBasicParsing
    Invoke-WebRequest "$baseUrl/checksums.txt" -OutFile $checksumPath -UseBasicParsing

    Write-Info "Verifying checksum..."
    Confirm-Checksum $archivePath $checksumPath

    Write-Info "Extracting..."
    Expand-Archive $archivePath -DestinationPath $tmpDir -Force

    $extractedBin = Join-Path $tmpDir $Binary
    if (-not (Test-Path $extractedBin)) {
      Write-Fatal "Binary '$Binary' not found in archive"
    }

    $installDir = Get-InstallDir
    New-Item -ItemType Directory -Path $installDir -Force | Out-Null

    Write-Info "Installing to $installDir..."
    $destBin = Join-Path $installDir $Binary
    $backupBin = "$destBin.old"
    $hasBackup = $false

    if (Test-Path $destBin) {
      Remove-Item $backupBin -Force -ErrorAction SilentlyContinue
      try {
        Rename-Item $destBin $backupBin -Force
        $hasBackup = $true
      } catch {
        Write-Fatal "Cannot move existing binary - is gb currently running? Close it and try again."
      }
    }

    try {
      Copy-Item $extractedBin $destBin -Force
      # Install succeeded - remove backup
      if ($hasBackup) { Remove-Item $backupBin -Force -ErrorAction SilentlyContinue }
    } catch {
      # Restore backup on failure
      if ($hasBackup) {
        Write-Warn "Install failed, restoring previous version..."
        Rename-Item $backupBin $destBin -Force -ErrorAction SilentlyContinue
      }
      Write-Fatal "Installation failed: $_"
    }

    Add-ToUserPath $installDir

    if ($installedVersion) {
      Write-Success "gb updated $installedVersion -> $version"
    } else {
      Write-Success "gb $version installed successfully"
    }

    Write-Host ""
    & (Join-Path $installDir $Binary) --version

  } finally {
    Remove-Item $tmpDir -Recurse -Force -ErrorAction SilentlyContinue
  }
}

Main
