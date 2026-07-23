param(
    # Output folder containing USB-ready deployment files.
    [string]$OutputDir = "./dist/deploy-bundle"
)

# Stop on any missing artifact or copy error.
$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $PSScriptRoot
$outputPath = [System.IO.Path]::GetFullPath((Join-Path $projectRoot $OutputDir))

# Clean output folder to prevent stale artifacts from previous runs.
if (Test-Path $outputPath) {
    Get-ChildItem -Path $outputPath -Force | Remove-Item -Recurse -Force
}
New-Item -ItemType Directory -Path $outputPath -Force | Out-Null

# Copy only files required for manual USB config workflow.
$filesToCopy = @(
    @{ Source = Join-Path $projectRoot "dist/agent.exe"; Destination = Join-Path $outputPath "agent.exe" },
    @{ Source = Join-Path $projectRoot "scripts/config-template.json"; Destination = Join-Path $outputPath "config.json" }
)

# Validate source files and copy them into bundle.
foreach ($file in $filesToCopy) {
    if (-not (Test-Path $file.Source)) {
        throw "Required file not found: $($file.Source)"
    }
    Copy-Item -Path $file.Source -Destination $file.Destination -Force
}

# Bundle README explains manual config editing and install flow.
$readme = @"
Big Brother deploy bundle
=========================

This folder contains the portable files needed for the USB deployment flow:

- agent.exe: installs the agent onto a target Windows device
- config.json: a ready-to-edit template configuration file

Quick start:
1. Edit config.json with the target device_id, setup_token, relay_api_address, and relay_address.
2. Copy agent.exe and config.json to a USB drive.
3. On the target Windows machine, run:
   .\agent.exe --install
"@
Set-Content -Path (Join-Path $outputPath "README.txt") -Value $readme -Encoding utf8

Write-Host "Created deploy bundle at $outputPath"
