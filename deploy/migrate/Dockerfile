# Schema migrations for Kubernetes (the Helm chart runs this as a Job before
# each install or upgrade). Compose applies the same files with its
# clickhouse-migrate and postgres-migrate services.
FROM clickhouse/clickhouse-server:24.10
RUN apt-get update && apt-get install -y --no-install-recommends postgresql-client && rm -rf /var/lib/apt/lists/*
COPY schemas/postgres /migrations/postgres
COPY schemas/clickhouse /migrations/clickhouse
COPY deploy/migrate/migrate.sh /usr/local/bin/pulse-migrate
ENTRYPOINT ["/usr/local/bin/pulse-migrate"]
