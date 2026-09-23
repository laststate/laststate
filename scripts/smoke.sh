#!/usr/bin/env bash
# Smoke-test the one-command stack. Usage: bash scripts/smoke.sh
set -u
pass=0; fail=0
check() { # $1 name $2 url $3 expected-substring(optional)
  out=$(curl -sk --max-time 5 "$2" || true)
  if [ -z "$out" ]; then echo "FAIL $1 ($2 unreachable)"; fail=$((fail+1)); return; fi
  if [ -n "${3:-}" ] && ! printf '%s' "$out" | grep -q "$3"; then
    echo "FAIL $1 (unexpected body)"; fail=$((fail+1)); return
  fi
  echo "OK   $1"; pass=$((pass+1))
}
check "trace-ui"        "http://localhost:8080/" ""
check "relay-admin"     "http://localhost:8383/" ""
check "grafana"         "http://localhost:3001/api/health" "ok"
check "prometheus"      "http://localhost:9090/-/healthy" "Prometheus"
check "relay-metrics"   "http://localhost:9467/metrics" ""
check "simulator-metrics" "http://localhost:9468/metrics" ""
echo "--- $pass ok, $fail failed ---"
[ "$fail" -eq 0 ]
