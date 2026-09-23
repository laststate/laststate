param(
  [string]$Binary = "$PSScriptRoot\..\..\bin\laststate-relay.exe",
  [string]$Config = "$env:ProgramData\LastState\relay.yaml",
  [string]$DataDir = "$env:ProgramData\LastState\data",
  [string]$ServiceName = "LastStateRelay"
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path $Binary)) {
  throw "Binary not found: $Binary (build with: go build -o bin/laststate-relay.exe ./cmd/laststate-relay)"
}

New-Item -ItemType Directory -Force -Path (Split-Path $Config) | Out-Null
New-Item -ItemType Directory -Force -Path $DataDir | Out-Null

if (-not (Test-Path $Config)) {
  Copy-Item "$PSScriptRoot\..\..\relay.yaml.example" $Config
  Write-Host "Installed default config at $Config — edit before production use."
}

$existing = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
if ($existing) {
  if ($existing.Status -eq "Running") {
    Stop-Service -Name $ServiceName -Force
  }
  sc.exe delete $ServiceName | Out-Null
  Start-Sleep -Seconds 1
}

$binPath = "`"$Binary`" run --config `"$Config`""
sc.exe create $ServiceName binPath= $binPath start= auto DisplayName= "Last State Relay" | Out-Null
sc.exe description $ServiceName "Last State Relay offline-first embedded diagnostics gateway" | Out-Null
# Restart on failure: reset period 86400s, restart after 5s/15s/30s
sc.exe failure $ServiceName reset= 86400 actions= restart/5000/restart/15000/restart/30000 | Out-Null
sc.exe failureflag $ServiceName 1 | Out-Null
sc.exe start $ServiceName | Out-Null
Write-Host "Service $ServiceName installed and started."
