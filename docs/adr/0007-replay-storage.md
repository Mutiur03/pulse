# 7. Session replay in object storage, indexed in ClickHouse

Date: 2026-10-16. Status: accepted.

## Context

rrweb recordings are large (DOM snapshots plus mutation streams) and read
rarely and sequentially, unlike analytical data. Storing them in ClickHouse
rows would bloat parts and merges.

## Decision

The browser SDK sends gzip-compressed rrweb chunks to `/v1/replay`. The
ingest gateway scrubs text, stores each chunk as an object in an
S3-compatible bucket (`replays/<project>/<session>/<start>-<nonce>.json.gz`), and writes a
small index row (session, sequence, time range, size, object key, markers)
to the `replay_chunks` table. The player lists chunks from ClickHouse and
streams objects through the query API. Locally a versitygw container serves
S3; in production any S3 API works (AWS S3, MinIO, R2).

## Consequences

- Cheap storage and lifecycle rules for the bulk data; ClickHouse stays
  small and fast for replay search and linking from issues and traces.
- Two stores to keep consistent: retention and deletion requests remove
  both the index rows and the objects (governance worker), and an S3
  lifecycle rule is a backstop (deploy/terraform).
- Backups must include the bucket (tools/backup).
