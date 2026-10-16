# 12. Managed data services; Helm deploys only Pulse

Date: 2026-10-16. Status: accepted.

## Context

Running Kafka, ClickHouse and Postgres well on Kubernetes needs operators,
storage classes, backups and upgrade procedures. Most teams would rather buy
them.

## Decision

The Helm chart deploys only Pulse's services, a migration Job and optional
ingress, autoscalers, disruption budgets and ServiceMonitors. Data services
are external and configured through values and a Secret. A Terraform
skeleton creates them on AWS (RDS, ElastiCache, MSK with SCRAM, S3) with a
placeholder for ClickHouse Cloud or a self-managed cluster, and outputs the
matching Helm values. The services support TLS and credentials for each
(`KAFKA_TLS`, `KAFKA_SASL_*`, `CLICKHOUSE_SECURE`, `REDIS_URL`).

## Consequences

- The chart stays small and portable across clouds.
- A fully self-hosted Kubernetes install needs operators chosen by the user
  (Strimzi, Altinity, CloudNativePG); Compose remains the one-command option.
