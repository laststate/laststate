param(
  [string]$ServiceName = "LastStateRelay"
)

$ErrorActionPreference = "Stop"
$existing = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
if (-not $existing) {
  Write-Host "Service $ServiceName is not installed."
  exit 0
}
if ($existing.Status -eq "Running") {
  Stop-Service -Name $ServiceName -Force
}
sc.exe delete $ServiceName | Out-Null
Write-Host "Service $ServiceName removed."
