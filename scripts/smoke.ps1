# Smoke-test the one-command stack. Usage: pwsh -File scripts/smoke.ps1
$checks = @(
  @{ Name = "trace-ui";          Url = "http://localhost:8080/" },
  @{ Name = "relay-admin";       Url = "http://localhost:8383/" },
  @{ Name = "grafana";           Url = "http://localhost:3001/api/health" },
  @{ Name = "prometheus";        Url = "http://localhost:9090/-/healthy" },
  @{ Name = "relay-metrics";     Url = "http://localhost:9467/metrics" },
  @{ Name = "simulator-metrics"; Url = "http://localhost:9468/metrics" }
)
$pass = 0; $fail = 0
foreach ($c in $checks) {
  try {
    $r = Invoke-WebRequest -Uri $c.Url -TimeoutSec 5 -UseBasicParsing
    Write-Output "OK   $($c.Name) ($($r.StatusCode))"
    $pass++
  } catch {
    Write-Output "FAIL $($c.Name) ($($c.Url)): $($_.Exception.Message)"
    $fail++
  }
}
Write-Output "--- $pass ok, $fail failed ---"
if ($fail -gt 0) { exit 1 }
