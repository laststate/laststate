# Backup and restore

## PostgreSQL

```bash
# Backup
pg_dump "$TRACE_DATABASE_URL" -Fc -f trace-$(date +%Y%m%d).dump

# Restore
pg_restore --clean --if-exists -d "$TRACE_DATABASE_URL" trace-YYYYMMDD.dump
```

## Object storage

### Local filesystem

```bash
tar czf objects-$(date +%Y%m%d).tgz -C "$TRACE_OBJECT_DIR" .
# restore
mkdir -p "$TRACE_OBJECT_DIR" && tar xzf objects-YYYYMMDD.tgz -C "$TRACE_OBJECT_DIR"
```

### S3 / MinIO

```bash
mc mirror local/trace s3backup/trace-$(date +%Y%m%d)/
```

## Secrets

Bootstrap credentials are written once to:

- `$TRACE_OBJECT_DIR/../bootstrap-token.txt` (ingest token)
- `$TRACE_OBJECT_DIR/../bootstrap-admin.txt` (admin email + password)

Never commit these files. Rotate tokens after restore if the backup may have leaked.

## Disaster recovery checklist

1. Restore Postgres dump.
2. Restore object storage prefix / directory.
3. Start Trace with `TRACE_BOOTSTRAP=false` (schema already present).
4. Run migrations (automatic on boot; advisory-locked).
5. Verify `/health/ready` and ingest a smoke event.
