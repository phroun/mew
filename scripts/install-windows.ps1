# install-windows.ps1 — install mew on Windows and add it to the Start Menu.
#
#   powershell -ExecutionPolicy Bypass -File scripts\install-windows.ps1
#
# Copies the built binaries (bin\mew.exe + bin\mew-sdl.exe) into a stable
# location, creates a Start Menu shortcut that launches the graphical host
# (mew-sdl.exe, which carries the embedded app icon), and — unless told not to —
# adds the install directory to the user PATH so `mew` works from any console.
#
# Build the binaries first (on Windows, with a cgo toolchain + SDL2 for the GUI):
#   make windows        # console mew.exe
#   make windows-sdl    # GUI mew-sdl.exe (with icon)
#
# Parameters:
#   -InstallDir <path>   where the binaries go   (default: %LOCALAPPDATA%\Programs\mew)
#   -AllUsers            install for all users    (needs an elevated shell)
#   -NoPath              don't touch PATH
#   -Uninstall           remove the install dir, the Start Menu shortcut, and
#                        the PATH entry this script added
[CmdletBinding()]
param(
    [string]$InstallDir,
    [switch]$AllUsers,
    [switch]$NoPath,
    [switch]$Uninstall
)

$ErrorActionPreference = 'Stop'
$AppName = 'mew'

# Default install location: per-user Programs dir, or Program Files for -AllUsers.
if (-not $InstallDir) {
    if ($AllUsers) {
        $InstallDir = Join-Path $env:ProgramFiles $AppName
    } else {
        $InstallDir = Join-Path $env:LOCALAPPDATA "Programs\$AppName"
    }
}

# Start Menu Programs dir (all-users vs per-user) and the shortcut path.
if ($AllUsers) {
    $startMenu = Join-Path $env:ProgramData 'Microsoft\Windows\Start Menu\Programs'
    $pathScope = 'Machine'
} else {
    $startMenu = Join-Path $env:AppData 'Microsoft\Windows\Start Menu\Programs'
    $pathScope = 'User'
}
$shortcut = Join-Path $startMenu "$AppName.lnk"

function Remove-FromPath {
    param([string]$Dir, [string]$Scope)
    $cur = [Environment]::GetEnvironmentVariable('Path', $Scope)
    if (-not $cur) { return }
    $kept = $cur.Split(';') | Where-Object { $_ -and ($_.TrimEnd('\') -ne $Dir.TrimEnd('\')) }
    [Environment]::SetEnvironmentVariable('Path', ($kept -join ';'), $Scope)
}

if ($Uninstall) {
    if (Test-Path $shortcut) { Remove-Item $shortcut -Force; Write-Host "removed Start Menu shortcut" }
    if (Test-Path $InstallDir) { Remove-Item $InstallDir -Recurse -Force; Write-Host "removed $InstallDir" }
    Remove-FromPath -Dir $InstallDir -Scope $pathScope
    Write-Host "uninstalled $AppName"
    return
}

# Locate the built binaries (bin\ next to the repo root, i.e. beside scripts\).
$repoRoot = Split-Path -Parent $PSScriptRoot
$binDir   = Join-Path $repoRoot 'bin'
$mewExe   = Join-Path $binDir 'mew.exe'
$sdlExe   = Join-Path $binDir 'mew-sdl.exe'

if (-not (Test-Path $mewExe)) { throw "not found: $mewExe (run 'make windows' first)" }
if (-not (Test-Path $sdlExe)) { throw "not found: $sdlExe (run 'make windows-sdl' on Windows first)" }

# Copy the binaries into place, side by side (the --window/--detach handoff from
# mew.exe expects mew-sdl.exe beside it).
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
Copy-Item $mewExe -Destination $InstallDir -Force
Copy-Item $sdlExe -Destination $InstallDir -Force
Write-Host "installed binaries to $InstallDir"

# Start Menu shortcut — points at the GUI host (mew-sdl.exe), which carries the
# embedded app icon, so the Start Menu entry shows it. Launching from the Start
# Menu opens the window (the console mew.exe stays the terminal entry point).
$installedSdl = Join-Path $InstallDir 'mew-sdl.exe'
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $shortcut) | Out-Null
$shell = New-Object -ComObject WScript.Shell
$lnk = $shell.CreateShortcut($shortcut)
$lnk.TargetPath       = $installedSdl
$lnk.IconLocation     = $installedSdl
$lnk.WorkingDirectory = $InstallDir
$lnk.Description      = 'mew editor'
$lnk.Save()
Write-Host "created Start Menu shortcut: $shortcut"

# Add the install dir to PATH (idempotent) so `mew` runs from any console.
if (-not $NoPath) {
    $cur = [Environment]::GetEnvironmentVariable('Path', $pathScope)
    $has = $cur -and ($cur.Split(';') | Where-Object { $_.TrimEnd('\') -eq $InstallDir.TrimEnd('\') })
    if (-not $has) {
        $new = if ($cur) { "$cur;$InstallDir" } else { $InstallDir }
        [Environment]::SetEnvironmentVariable('Path', $new, $pathScope)
        Write-Host "added $InstallDir to $pathScope PATH (open a new console to pick it up)"
    } else {
        Write-Host "$InstallDir already on $pathScope PATH"
    }
}

Write-Host "done — find '$AppName' in the Start Menu, or run 'mew' / 'mew --window' from a console"
