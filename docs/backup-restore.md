# Backup and restore

Pulse keeps state in three places:

| Store | What | How it is backed up |
| --- | --- | --- |
| Postgres | Users, orgs, projects, keys, issues, alerts, dashboards, releases, audit log, data settings and deletion requests | `pg_dump --format=custom` |
| ClickHouse | Events, sessions, person aliases, spans, errors, logs, metrics and the replay index | Each table's `CREATE` statement and its rows in Native format (gzip) |
| Object storage | Replay chunks (`pulse-replays`) and source maps (`pulse-artifacts`) | `aws s3 sync` |

Redis holds only sessions, rate limits and caches, and is not backed up.
Kafka holds data in transit only. Back up after the processor has caught
up, or accept losing whatever was in flight.

## Scripts

`tools/backup/backup.sh` and `tools/backup/restore.sh` cover all three
stores.

- With `MODE=compose` (the default), they run the clients in containers on
  the local stack's network.
- With `MODE=direct`, they use `pg_dump`/`pg_restore`, `clickhouse-client`
  and the `aws` CLI from `PATH`. Use this against managed services, setting
  the connection variables listed in `tools/backup/common.sh`:
  `POSTGRES_URL`, `CLICKHOUSE_HOST`, `CLICKHOUSE_PORT`, `CLICKHOUSE_USER`,
  `CLICKHOUSE_PASSWORD`, `CLICKHOUSE_SECURE`, `S3_URL`, `S3_ACCESS_KEY`,
  `S3_SECRET_KEY` and `S3_BUCKETS`.

```bash
# Back up (default directory backups/<UTC time>, ignored by git)
tools/backup/backup.sh
tools/backup/backup.sh /mnt/backups/pulse-2026-10-16

# Restore: stop the Pulse services first so nothing writes meanwhile
docker compose -f deploy/compose/docker-compose.yml stop ingest-gateway stream-processor query-api alerting-worker governance-worker web
tools/backup/restore.sh /mnt/backups/pulse-2026-10-16
docker compose -f deploy/compose/docker-compose.yml up -d
```

Restore does three things:

- **Postgres:** `pg_restore --clean`. Objects are dropped and recreated.
- **ClickHouse:** each table is created if it is missing, then truncated and
  reloaded. The row count is checked against the count saved at backup
  time.
- **Object storage:** objects are synced back. Objects that were not in the
  backup are kept.

The target can be an empty, freshly migrated installation or the same one.

### Verified on the development box

1. Backed up the local stack (8 ClickHouse tables, about 72 KB of Postgres,
   one replay object).
2. Simulated data loss:
   - truncated `events` and `person_aliases`;
   - deleted every dashboard and an ingest key in Postgres;
   - removed the replay object.
3. Restored. It took about 20 seconds.

Afterwards:

- every ClickHouse row count matched the backup;
- the dashboards and keys were back;
- the object was back;
- login worked after the services restarted.

## Production

The scripts suit small and medium installs and drills. At scale, prefer each
service's native backups:

- **Postgres:** managed snapshots with point-in-time recovery. The Terraform
  skeleton keeps 14 days on RDS.
- **ClickHouse:** native `BACKUP TABLE ... TO S3(...)`, which is incremental
  with `base_backup`, or ClickHouse Cloud's automatic backups. Restore with
  `RESTORE`.
- **Object storage:** bucket versioning or cross-region replication.
  Replays already expire through lifecycle rules (30 days) and the
  governance worker.

Retention also applies to backups. Data deleted by a deletion request or by
retention still exists in older backups until they expire. The Terraform
backup bucket expires backups after 35 days. Document this in your privacy
policy, or restore and re-run deletion requests after any restore.

Run a restore drill regularly. A backup that was never restored is not a
backup.
