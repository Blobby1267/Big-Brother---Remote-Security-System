param(
    # Agent executable file to install.
    [string]$AgentExe = "agent.exe",
    # Service config file path consumed by installed agent.
    [string]$ConfigPath = "$env:ProgramData\\BigBrother\\config.json",
    # Windows Service name used for registration and startup.
    [string]$ServiceName = "BigBrotherAgent"
)

# Fail on first error to keep service state consistent.
$ErrorActionPreference = "Stop"
$installDir = "$env:ProgramFiles\\BigBrother"
if (-not (Test-Path $installDir)) {
    New-Item -ItemType Directory -Path $installDir -Force | Out-Null
}

# Install agent binary in Program Files.
$targetAgent = Join-Path $installDir $AgentExe
Copy-Item -Path $AgentExe -Destination $targetAgent -Force

# Ensure config directory exists before service starts.
if (-not (Test-Path (Split-Path $ConfigPath))) {
    New-Item -ItemType Directory -Path (Split-Path $ConfigPath) -Force | Out-Null
}

if (-not (Test-Path $ConfigPath)) {
    Write-Host "Please place your config.json at $ConfigPath before starting the service."
}

# Create or update service commandline.
$exePath = "\"$targetAgent\" --config \"$ConfigPath\""
if (Get-Service -Name $ServiceName -ErrorAction SilentlyContinue) {
    Write-Host "Service $ServiceName already exists. Updating executable path."
    sc.exe config $ServiceName binPath= "$exePath" start= auto
} else {
    sc.exe create $ServiceName binPath= "$exePath" start= auto obj= LocalSystem type= own
}

sc.exe config $ServiceName start= auto

# Restart service so latest binary/args are active now.
Stop-Service -Name $ServiceName -ErrorAction SilentlyContinue
Start-Sleep -Seconds 2
Start-Service -Name $ServiceName
Write-Host "Installed and started service $ServiceName"
Write-Host "The agent will now start automatically after Windows boots."
