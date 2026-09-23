#!/usr/bin/env bash
# Trace backup: Postgres + object dir/S3 mirror
set -euo pipefail
STAMP=$(date +%Y%m%d-%H%M%S)
OUT=${1:-"./backups/trace-$STAMP"}
mkdir -p "$OUT"

if [[ -z "${TRACE_DATABASE_URL:-}" ]]; then
  echo "TRACE_DATABASE_URL required" >&2
  exit 1
fi

echo "Dumping postgres..."
pg_dump "$TRACE_DATABASE_URL" -Fc -f "$OUT/db.dump"

OBJ=${TRACE_OBJECT_DIR:-./data/objects}
if [[ -d "$OBJ" ]]; then
  echo "Archiving objects from $OBJ..."
  tar czf "$OUT/objects.tgz" -C "$OBJ" .
fi

if [[ -n "${TRACE_S3_ENDPOINT:-}" && -n "${TRACE_S3_BUCKET:-}" ]]; then
  if command -v mc >/dev/null 2>&1; then
    echo "Mirroring S3 bucket..."
    mc mirror "local/${TRACE_S3_BUCKET}" "$OUT/s3/" || true
  fi
fi

echo "OK: $OUT"
