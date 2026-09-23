#!/bin/bash
# Start the Trace web UI in mock/preview mode with fake data.
# No database, S3, or queue required — perfect for UI development and demos.
#
# Usage:
#   ./scripts/mock-preview.sh
#   TRACE_WEB_DIR=web/dist ./scripts/mock-preview.sh

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

cd "$PROJECT_DIR"

echo "Starting Trace in MOCK mode..."
echo "  - No database required"
echo "  - No S3/object store required"
echo "  - No queue/worker required"
echo "  - UI preview with realistic fake data"
echo ""

# Build web UI if dist doesn't exist
if [ ! -d "$PROJECT_DIR/web/dist" ]; then
    echo "Building web UI..."
    cd "$PROJECT_DIR/web"
    npm run build
    cd "$PROJECT_DIR"
    echo "Web UI built successfully."
    echo ""
fi

echo "  Open: http://localhost:8080"
echo "  Press Ctrl+C to stop"
echo ""

TRACE_MOCK=true TRACE_OPEN_UI=true TRACE_WEB_DIR="$PROJECT_DIR/web/dist" \
  go run ./cmd/trace
