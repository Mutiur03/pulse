# Staging on a local cluster

A staging Pulse runs the production Helm chart on a single-node k3s cluster
(k3d, so k3s runs inside Docker). Kafka, ClickHouse, Postgres, Redis and an
S3-compatible store run in the cluster with persistent volumes, so the whole
thing fits on one 8 vCPU / 16 GB machine. Production uses managed data
services instead; see [deployment](deployment.md).

Files are in `deploy/staging/`:

| File | What it is |
| --- | --- |
| `up.sh` | Creates the namespace, random secrets, data services, then `helm upgrade --install` |
| `data.yaml` | Single-replica StatefulSets for Postgres, ClickHouse, Redis, Kafka (KRaft) and versitygw |
| `values-staging.yaml` | Chart values: local images, Traefik ingress, trusted proxies |
| `go-demo-api.yaml` | The example Go backend, sending OTLP/gRPC to the ingest gateway |
| `prometheus.yaml` | A small Prometheus that scrapes pods by their `prometheus.io/*` annotations |

## Create the cluster

```bash
k3d cluster create staging --agents 0 \
  --k3s-arg "--flannel-backend=host-gw@server:0" \
  --k3s-arg "--disable-network-policy@server:0" \
  --k3s-arg "--kube-proxy-arg=proxy-mode=nftables@server:0" --wait
```

The extra flags are only needed when k3s itself runs in a container whose
kernel lacks VXLAN or the legacy iptables NAT table (flannel then fails with
`failed to create vxlan device`, or Service addresses time out). On a normal
VM, `k3d cluster create staging` or a plain k3s install is enough.

## Build and load the images

```bash
R=ghcr.io/mutiur03/pulse-
for t in ingest-gateway stream-processor query-api alerting-worker governance-worker; do
  docker build -f deploy/compose/Dockerfile.go --target $t -t ${R}$t:staging .
done
docker build -f deploy/compose/Dockerfile.web -t ${R}web:staging .
docker build -f deploy/migrate/Dockerfile -t ${R}migrate:staging .
docker build -t ${R}go-demo-api:staging examples/go-demo-api
k3d image import -c staging ${R}{ingest-gateway,stream-processor,query-api,alerting-worker,governance-worker,web,migrate,go-demo-api}:staging
```

`values-staging.yaml` uses the `staging` tag with `pullPolicy: Never`. To use
a registry instead, push the images and override `image.tag` and
`image.pullPolicy`.

## Install

```bash
deploy/staging/up.sh
```

On the first run it generates random passwords into two Secrets (never into
files): `pulse-data` for the data services and `pulse-secrets`, which the chart
uses through `existingSecret`. Re-running keeps them and upgrades the release;
extra arguments go to `helm`, for example
`deploy/staging/up.sh --set-string config.extraEnv.ROLL_ID=$(date +%s)` to
force a rolling restart. The migration Job runs as a pre-install and
pre-upgrade hook.

## Reach it

Traefik serves `pulse.staging.localhost` (web) and `ingest.staging.localhost`
(ingest). Forward it to the host and use those names, which browsers resolve
to 127.0.0.1:

```bash
kubectl -n kube-system port-forward svc/traefik 8089:80
# web:    http://pulse.staging.localhost:8089
# ingest: http://ingest.staging.localhost:8089
```

`appUrl` in the values must match the address you open, or the CSRF origin
check rejects logins. Chrome treats `*.localhost` as a secure origin, so the
`Secure` session cookie works over plain HTTP. For anything else, put TLS in
front and set `appUrl` to the HTTPS address. `cloudflared tunnel --url
http://localhost:8089 --http-host-header pulse.staging.localhost` works where
outbound port 7844 is open.

## Send data

Create a project in the web app and copy its key, then:

```bash
kubectl -n pulse create secret generic pulse-demo --from-literal=key=pk_...
kubectl -n pulse apply -f deploy/staging/go-demo-api.yaml
kubectl -n pulse port-forward svc/go-demo-api 8090:8090
pnpm --filter @pulse/browser-demo start
# open http://localhost:5173/?ingest=http://ingest.staging.localhost:8089&key=pk_...&api=http://localhost:8090
```

## Checks

- Metrics: `kubectl -n pulse apply -f deploy/staging/prometheus.yaml`, then
  port-forward `svc/prometheus` and open Status > Targets. Every Go pod
  should be up.
- Load: `INGEST_URL=http://ingest.staging.localhost:8089 PULSE_KEY=pk_... k6 run load/ingest.js`
  (add `127.0.0.1 ingest.staging.localhost` to `/etc/hosts` for k6).
- Backup and restore: port-forward Postgres, ClickHouse and the object store,
  then run `tools/backup/backup.sh` and `restore.sh` with `MODE=compose
  COMPOSE_NETWORK=host` and the addresses and passwords from the `pulse-data`
  Secret (see [backup and restore](backup-restore.md)). Scale the Pulse
  Deployments to 0 during a restore and run `up.sh` afterwards.

## Results (October 2026)

Measured on the 8 vCPU box this was built on:

| Check | Result |
| --- | --- |
| Install from scratch | Data services ready and chart installed in under a minute |
| Browser demo and Go demo API (gRPC) | Events, traces, errors, logs, metrics and replays all stored and visible |
| Alert rule on error count | Fired within a minute of the demo's errors |
| k6 ingest, 200 req/s x 50 records for 60s | 0 failed of 12,001 requests, p95 33 ms, all 600k records stored |
| Prometheus | 8 of 8 Go pods scraped |
| Rolling update under 70 req/s | Without a drain delay: 42 failed of 9,057 (30 s timeouts). With `shutdownDelaySeconds: 10`: 5 of 10,502, then 0, then 0 |
| Force-deleting ingest, query and processor pods and restarting Kafka | 0 failed requests, no events lost; Kafka back in 30 s |
| Backup, wipe, restore | Every Postgres and ClickHouse row count matched; restore took 15 s |

## Tear down

```bash
k3d cluster delete staging
```

This removes the volumes too.
