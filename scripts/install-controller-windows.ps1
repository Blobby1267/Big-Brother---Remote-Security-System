param(
    # Source controller executable path.
    [string]$ControllerExe = "controller.exe",
    # Target install directory under Program Files.
    [string]$InstallDir = "$env:ProgramFiles\BigBrother",
    # Desktop shortcut location for quick operator launch.
    [string]$ShortcutPath = "$env:USERPROFILE\Desktop\Big Brother Controller.lnk"
)

# Stop immediately on errors to avoid partial installs.
$ErrorActionPreference = "Stop"

if (-not (Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
}

# Resolve and copy executable into install folder.
$sourceExe = Resolve-Path $ControllerExe -ErrorAction SilentlyContinue
if (-not $sourceExe) {
    throw "Controller executable not found: $ControllerExe"
}

$targetExe = Join-Path $InstallDir "controller.exe"
Copy-Item -Path $sourceExe.Path -Destination $targetExe -Force

# Create a Windows shortcut for the installed controller binary.
$WshShell = New-Object -ComObject WScript.Shell
$shortcut = $WshShell.CreateShortcut($ShortcutPath)
$shortcut.TargetPath = $targetExe
$shortcut.WorkingDirectory = $InstallDir
$shortcut.IconLocation = $targetExe
$shortcut.Description = "Big Brother Controller"
$shortcut.Save()

Write-Host "Installed Big Brother Controller to $InstallDir"
Write-Host "Desktop shortcut created at $ShortcutPath"
