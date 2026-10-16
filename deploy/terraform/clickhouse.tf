# ClickHouse placeholder.
#
# Pulse needs ClickHouse 24.x reachable over the native protocol. Options:
#
# 1. ClickHouse Cloud with the official provider (ClickHouse/clickhouse):
#
#    resource "clickhouse_service" "pulse" {
#      name           = "${var.name}"
#      cloud_provider = "aws"
#      region         = var.region
#      idle_scaling   = false
#      password_hash  = base64encode(sha256(random_password.clickhouse.result))
#      ip_access      = [{ source = "<cluster egress IP>/32", description = "k8s" }]
#      min_replica_memory_gb = 16
#      max_replica_memory_gb = 64
#    }
#
#    then set clickhouse_host to clickhouse_service.pulse.endpoints[...] and
#    keep port 9440 (TLS).
#
# 2. A self-managed cluster on Kubernetes (the Altinity operator) or EC2.
#    Use port 9000 and clickhouse_secure = false only inside a private network.
#
# Either way the migration Job of the Helm chart creates the tables.

resource "random_password" "clickhouse" {
  length  = 32
  special = false
}
