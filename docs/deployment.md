# Deploying Pulse

Pulse runs as six stateless services plus a schema migration step, against
Postgres, Redis, ClickHouse, Kafka and S3-compatible storage.

| Service | Ports | Scales on |
| --- | --- | --- |
| ingest-gateway | 8080 HTTP, 4317 OTLP/gRPC, 9101 metrics | request rate (CPU) |
| stream-processor | 8082 health and metrics | Kafka partitions / lag |
| query-api | 8081 HTTP, 9102 metrics | read QPS (CPU) |
| alerting-worker | 8083 health and metrics | rule count (one replica is plenty for thousands) |
| governance-worker | 8084 health and metrics | one replica |
| web | 3000 | users |

- **Local or single host:** Docker Compose (`deploy/compose`, see the README).
- **Kubernetes:** the Helm chart in `deploy/helm` with managed data services.
  The Terraform skeleton in `deploy/terraform` can create those on AWS.

## Kubernetes with Helm

1. **Build and push the images.** Each Go service is a target of
   `deploy/compose/Dockerfile.go`. The web app is `deploy/compose/Dockerfile.web`
   and the migration image is `deploy/migrate/Dockerfile`. All images are built
   from the repository root:

   ```bash
   REG=ghcr.io/you TAG=0.2.0
   for s in ingest-gateway stream-processor query-api alerting-worker governance-worker; do
     docker build -f deploy/compose/Dockerfile.go --target $s -t $REG/pulse-$s:$TAG . && docker push $REG/pulse-$s:$TAG
   done
   docker build -f deploy/compose/Dockerfile.web -t $REG/pulse-web:$TAG . && docker push $REG/pulse-web:$TAG
   docker build -f deploy/migrate/Dockerfile -t $REG/pulse-migrate:$TAG . && docker push $REG/pulse-migrate:$TAG
   ```

2. **Provide the data services** (managed services or your own operators) and
   their settings in a values file. The Terraform output `helm_values` has
   this shape:

   ```yaml
   image: {registry: ghcr.io/you, tag: "0.2.0"}
   appUrl: https://pulse.example.com
   config:
     kafkaBootstrapServers: b-1.kafka.example:9096,b-2.kafka.example:9096
     kafkaTLS: true
     clickhouseAddr: abc.clickhouse.cloud:9440
     clickhouseSecure: true
     redisAddr: redis.example:6379
     s3Endpoint: s3.us-east-1.amazonaws.com
     s3ReplayBucket: pulse-replays-prod
     s3ArtifactBucket: pulse-artifacts-prod
     smtpAddr: smtp.example:587
   existingSecret: pulse-secrets   # or fill `secrets:` and let the chart create it
   ingress: {enabled: true, className: nginx, webHost: pulse.example.com, ingestHost: ingest.pulse.example.com}
   ```

   The Secret holds `POSTGRES_URL`, `CLICKHOUSE_PASSWORD`, `REDIS_URL` or
   `REDIS_PASSWORD`, `KAFKA_SASL_USERNAME`, `KAFKA_SASL_PASSWORD`,
   `S3_ACCESS_KEY`, `S3_SECRET_KEY`, `SMTP_USER` and `SMTP_PASSWORD`.

3. **Install:**

   ```bash
   helm upgrade --install pulse deploy/helm -n pulse --create-namespace -f values-prod.yaml
   ```

   Before every install and upgrade, a Job runs `pulse-migrate`. It applies
   every file in `schemas/postgres` and `schemas/clickhouse`. All files are
   idempotent. The services start once the Job succeeds.

4. **Create the first user.** Open the web host and sign up. Seed data is
   for local development only and is not part of the chart.

### What the chart creates

- One Deployment and Service per component.
- HorizontalPodAutoscalers on CPU for the ingest gateway and query API, and
  PodDisruptionBudgets.
- Pods run as non-root on read-only root filesystems.
- A ConfigMap with connection settings and a Secret (unless `existingSecret`
  is set).
- An optional Ingress with separate web and ingest hosts. The ingest host
  routes only `/v1` and `/health` to the gateway. Expose OTLP/gRPC (4317)
  with a gRPC-capable load balancer if you need it.
- `prometheus.io/*` pod annotations, or ServiceMonitors with
  `metrics.serviceMonitor.enabled=true` (see [monitoring.md](monitoring.md)).

Validate changes to the chart with
`helm lint deploy/helm && helm template t deploy/helm | kubeconform -strict -summary`.
CI runs the same checks.

### Settings for managed services

| Variable | Purpose |
| --- | --- |
| `KAFKA_BOOTSTRAP_SERVERS` | Comma-separated brokers |
| `KAFKA_TLS=true` | TLS to the brokers |
| `KAFKA_SASL_MECHANISM` | `SCRAM-SHA-512` (default), `SCRAM-SHA-256` or `PLAIN` |
| `KAFKA_SASL_USERNAME`, `KAFKA_SASL_PASSWORD` | SASL credentials |
| `CLICKHOUSE_ADDR` | `host:port` of the native protocol (9440 with TLS) |
| `CLICKHOUSE_USER`, `CLICKHOUSE_PASSWORD`, `CLICKHOUSE_DB` | Credentials and database |
| `CLICKHOUSE_SECURE=true` | TLS |
| `POSTGRES_URL` | libpq URL, for example with `sslmode=require` |
| `REDIS_URL` | `rediss://:password@host:6379` (takes precedence) |
| `REDIS_ADDR`, `REDIS_PASSWORD` | Plain address and password |
| `S3_ENDPOINT`, `S3_REGION`, `S3_USE_SSL` | Object storage endpoint |
| `S3_BUCKET`, `S3_ARTIFACT_BUCKET` | Buckets for replays and source maps |
| `S3_ACCESS_KEY`, `S3_SECRET_KEY` | Object storage credentials |
| `METRICS_ADDR` | Metrics listener of the ingest gateway (`:9101`) and query API (`:9102`) |

### Topics and partitions

The services create their Kafka topics on first write. With managed Kafka,
either allow auto-creation (the Terraform skeleton sets 6 partitions and
replication factor 3) or create these topics yourself: `raw.events`,
`raw.spans`, `raw.errors`, `raw.logs`, `raw.metrics` and `issue.events`.
Stream processor replicas beyond the partition count stay idle.

## Terraform skeleton

`deploy/terraform` creates the following on AWS:

- RDS Postgres 16 (multi-AZ, encrypted, 14-day backups).
- ElastiCache Redis with TLS and an auth token.
- MSK with SASL/SCRAM over TLS.
- S3 buckets for replays (with a lifecycle rule), artifacts and backups.
- A service user for the buckets.

ClickHouse is a documented placeholder: use ClickHouse Cloud or a
self-managed cluster. See [its README](../deploy/terraform/README.md). It
is a starting point to review, not a hardened production setup.

## Upgrades

`helm upgrade` runs the migration Job first, then rolls the Deployments.
Schema files only add tables and columns, so old pods keep working during
the rollout. Take a backup before upgrading
([backup-restore.md](backup-restore.md)).
