# Re-vendor upstream component snapshots at new pins.
# Usage: pwsh -File scripts/update.ps1 [-TraceRef main] [-RelayRef main] [-BillingRef main] [-SimRef main]
param([string]$TraceRef = "main", [string]$RelayRef = "main", [string]$BillingRef = "main", [string]$SimRef = "main")
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root
function Clone-Vendor($dir, $url, $ref) {
  if (Test-Path "$dir.tmp") { Remove-Item -Recurse -Force "$dir.tmp" }
  git clone --depth 1 --branch $ref $url "$dir.tmp"
  Remove-Item -Recurse -Force "$dir.tmp/.git"
  if (Test-Path $dir) { Remove-Item -Recurse -Force $dir }
  Rename-Item "$dir.tmp" $dir
}
Clone-Vendor "trace" "https://github.com/laststate/trace.git" $TraceRef
Clone-Vendor "relay" "https://github.com/laststate/relay.git" $RelayRef
Clone-Vendor "billing-service" "https://github.com/laststate/billing-service.git" $BillingRef
Clone-Vendor "simulator" "https://github.com/laststate/simulator.git" $SimRef
Write-Output "Vendored. Record SHAs in COMPONENTS.md and commit."
