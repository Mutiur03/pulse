variable "region" {
  type    = string
  default = "us-east-1"
}

variable "environment" {
  type    = string
  default = "production"
}

variable "name" {
  description = "Prefix for resource names."
  type        = string
  default     = "pulse"
}

variable "vpc_id" {
  description = "VPC of the Kubernetes cluster."
  type        = string
}

variable "subnet_ids" {
  description = "Private subnets (at least two, in different zones) for the data services."
  type        = list(string)
}

variable "allowed_cidr_blocks" {
  description = "CIDRs allowed to reach the data services (the cluster's pod/node ranges)."
  type        = list(string)
}

variable "postgres_instance_class" {
  type    = string
  default = "db.t4g.medium"
}

variable "postgres_storage_gb" {
  type    = number
  default = 50
}

variable "redis_node_type" {
  type    = string
  default = "cache.t4g.small"
}

variable "kafka_instance_type" {
  type    = string
  default = "kafka.m7g.large"
}

variable "kafka_broker_count" {
  description = "A multiple of the number of subnets."
  type        = number
  default     = 3
}

variable "kafka_volume_gb" {
  type    = number
  default = 200
}

variable "replay_retention_days" {
  description = "S3 lifecycle expiry for replay objects; match the replay TTL (30 days)."
  type        = number
  default     = 30
}

variable "clickhouse_host" {
  description = "Host of your ClickHouse Cloud service or cluster (see clickhouse.tf)."
  type        = string
  default     = "CHANGE-ME.clickhouse.cloud"
}

variable "clickhouse_port" {
  description = "Native protocol port: 9440 with TLS (ClickHouse Cloud), 9000 without."
  type        = number
  default     = 9440
}
