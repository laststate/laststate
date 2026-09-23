#!/usr/bin/env bash
# Move each submodule to the tip of its tracked branch (main),
# so COMPONENTS.md can be refreshed after a test boot.
# Usage: bash scripts/update.sh
set -euo pipefail
cd "$(dirname "$0")/.."
git submodule update --remote --merge trace relay billing-service simulator
git submodule status
echo "Boot the stack, run scripts/smoke.sh, then commit the new pins."
