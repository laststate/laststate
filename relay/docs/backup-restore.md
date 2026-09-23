# Backup and restore

## Backup

Stop Relay first (or pause all sources) so SQLite and the object store are quiet.

```bash
laststate-relay backup --data-dir ./data --output ./backups/relay-$(date +%Y%m%d).zip
```

Or copy the data directory after stopping the service:

```bash
cp -a /var/lib/laststate /var/backups/laststate-$(date +%Y%m%d)
```

Also keep `relay.yaml` and any secrets referenced via `env:` / `file:`.

## Restore

```bash
# service stopped
laststate-relay restore --input ./backups/relay-20260715.zip --data-dir ./data
laststate-relay spool reconcile --data-dir ./data
laststate-relay doctor --config relay.yaml
```

## Notes

- Event IDs are content hashes. Restoring an old spool and re-ingesting the same
  bytes shows up as duplicates (`ACK_DUPLICATE`).
- Delivery state comes back with SQLite; open circuit breakers clear after
  successful posts.
- Use `spool.fsync: full` in production so a clean shutdown is easy to back up.
