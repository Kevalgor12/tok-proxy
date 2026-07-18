# tok installer for Windows (PowerShell).
#   iwr -useb https://raw.githubusercontent.com/Kevalgor12/tok-proxy/main/scripts/install.ps1 | iex
# Downloads the standalone tok.exe (no Node required), puts it on PATH, installs hooks.

$ErrorActionPreference = 'Stop'

# Override with $env:TOK_REPO = 'owner/repo' to install from a fork.
$Repo = if ($env:TOK_REPO) { $env:TOK_REPO } else { 'Kevalgor12/tok-proxy' }

$Asset = 'tok-windows-x64.exe'
$Url = "https://github.com/$Repo/releases/latest/download/$Asset"
$InstallDir = Join-Path $HOME '.local\bin'
$Dest = Join-Path $InstallDir 'tok.exe'

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
Write-Host "Downloading $Url ..."
Invoke-WebRequest -Uri $Url -OutFile $Dest

# Add ~/.local/bin to the user PATH if it isn't already there.
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if ($userPath -notlike "*$InstallDir*") {
  [Environment]::SetEnvironmentVariable('Path', "$InstallDir;$userPath", 'User')
  $env:Path = "$InstallDir;$env:Path"
  Write-Host "Added $InstallDir to your PATH (open a new terminal for it to persist)."
}

# Detect AI tools and install hooks.
& $Dest init

Write-Host ""
Write-Host "tok installed to $Dest"
Write-Host "Restart Claude Code, then run:  tok doctor"
