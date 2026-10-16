# Terraform skeleton

Creates the managed services Pulse needs on AWS and prints the Helm values
that connect the chart to them. It is a starting point, not a turnkey
production setup: review sizes, networking, backups and access before use.

| Need | Resource here | Helm values |
| --- | --- | --- |
| Postgres 16 | RDS (`aws_db_instance.postgres`) | `secrets.POSTGRES_URL` |
| Redis 7 | ElastiCache (`aws_elasticache_replication_group.redis`) | `config.redisAddr`, `secrets.REDIS_PASSWORD` |
| Kafka | MSK with SASL/SCRAM (`aws_msk_cluster.kafka`) | `config.kafkaBootstrapServers`, `config.kafkaTLS`, `secrets.KAFKA_SASL_*` |
| Object storage | S3 buckets for replays and source maps (`aws_s3_bucket.*`) | `config.s3*`, `secrets.S3_*` |
| ClickHouse | Placeholder: ClickHouse Cloud or a self-managed cluster (`clickhouse.tf`) | `config.clickhouseAddr`, `config.clickhouseSecure`, `secrets.CLICKHOUSE_PASSWORD` |

The Kubernetes cluster itself (EKS or any other) is out of scope; pass its
VPC and private subnets in `vpc_id` and `subnet_ids`.

```bash
cd deploy/terraform
cp terraform.tfvars.example terraform.tfvars   # fill in
terraform init
terraform plan
terraform apply
terraform output -json helm_values             # merge into your values file
```

Secrets (database password, SCRAM credentials) are generated with
`random_password` and stored in Terraform state, so keep the state in an
encrypted remote backend (see `backend.tf`).

Validate without credentials:

```bash
terraform init -backend=false && terraform validate
```
