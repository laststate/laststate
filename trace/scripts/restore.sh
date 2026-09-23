#!/usr/bin/env bash
set -euo pipefail
IN=${1:?"usage: restore.sh ./backups/trace-STAMP"}
if [[ -z "${TRACE_DATABASE_URL:-}" ]]; then
  echo "TRACE_DATABASE_URL required" >&2
  exit 1
fi
echo "Restoring postgres from $IN/db.dump"
pg_restore --clean --if-exists -d "$TRACE_DATABASE_URL" "$IN/db.dump"
OBJ=${TRACE_OBJECT_DIR:-./data/objects}
if [[ -f "$IN/objects.tgz" ]]; then
  mkdir -p "$OBJ"
  tar xzf "$IN/objects.tgz" -C "$OBJ"
fi
echo "Restore complete. Start Trace with TRACE_BOOTSTRAP=false"
