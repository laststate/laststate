# Move each submodule to the tip of its tracked branch (main),
# so COMPONENTS.md can be refreshed after a test boot.
# Usage: pwsh -File scripts/update.ps1
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root
git submodule update --remote --merge trace relay billing-service simulator
git submodule status
Write-Output "Boot the stack, run scripts/smoke.ps1, then commit the new pins."
