#!/usr/bin/env bash
# Re-vendor upstream component snapshots at new pins.
# Usage: bash scripts/update.sh [trace_sha] [relay_sha] [billing_sha] [sim_sha]
# Defaults to origin/main HEAD of each repo.
set -euo pipefail
cd "$(dirname "$0")/.."
clone() { # $1 dir $2 url $3 ref
  rm -rf "$1.tmp" && git clone --depth 1 ${3:+--branch "$3"} "$2" "$1.tmp"
  rm -rf "$1.tmp/.git"
  rm -rf "$1" && mv "$1.tmp" "$1"
}
clone trace https://github.com/laststate/trace.git "${1:-main}"
clone relay https://github.com/laststate/relay.git "${2:-main}"
clone billing-service https://github.com/laststate/billing-service.git "${3:-main}"
clone simulator https://github.com/laststate/simulator.git "${4:-main}"
echo "Vendored. Record SHAs in COMPONENTS.md and commit."
