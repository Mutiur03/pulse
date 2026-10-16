#!/bin/bash
# Creates a staging Pulse on the current kube context: random secrets, data
# services, then the Helm chart. Re-running keeps existing secrets.
set -euo pipefail
cd "$(dirname "$0")"
NS=${NS:-pulse}
kubectl create namespace "$NS" --dry-run=client -o yaml | kubectl apply -f -
rand() { head -c 32 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 32; }
if ! kubectl -n "$NS" get secret pulse-data >/dev/null 2>&1; then
  PG=$(rand); CH=$(rand); RD=$(rand); S3K=$(rand | head -c 20); S3S=$(rand)
  kubectl -n "$NS" create secret generic pulse-data \
    --from-literal=postgres-password="$PG" --from-literal=clickhouse-password="$CH" \
    --from-literal=redis-password="$RD" --from-literal=s3-access-key="$S3K" \
    --from-literal=s3-secret-key="$S3S"
  kubectl -n "$NS" create secret generic pulse-secrets \
    --from-literal=POSTGRES_URL="postgres://pulse:$PG@postgres:5432/pulse?sslmode=disable" \
    --from-literal=CLICKHOUSE_PASSWORD="$CH" --from-literal=REDIS_PASSWORD="$RD" \
    --from-literal=S3_ACCESS_KEY="$S3K" --from-literal=S3_SECRET_KEY="$S3S"
fi
kubectl -n "$NS" apply -f data.yaml
for s in postgres clickhouse redis kafka objectstore; do
  kubectl -n "$NS" rollout status statefulset/$s --timeout=300s
done
helm upgrade --install pulse ../helm -n "$NS" -f values-staging.yaml "$@" --wait --timeout 10m
