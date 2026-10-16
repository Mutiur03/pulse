output "helm_values" {
  description = "Merge into the Helm chart's values (contains secrets)."
  sensitive   = true
  value = {
    config = {
      kafkaBootstrapServers = aws_msk_cluster.kafka.bootstrap_brokers_sasl_scram
      kafkaTLS              = true
      kafkaSaslMechanism    = "SCRAM-SHA-512"
      clickhouseAddr        = "${var.clickhouse_host}:${var.clickhouse_port}"
      clickhouseSecure      = var.clickhouse_port == 9440
      redisAddr             = "${aws_elasticache_replication_group.redis.primary_endpoint_address}:6379"
      s3Endpoint            = "s3.${var.region}.amazonaws.com"
      s3Region              = var.region
      s3UseSSL              = true
      s3ReplayBucket        = aws_s3_bucket.replays.id
      s3ArtifactBucket      = aws_s3_bucket.artifacts.id
    }
    secrets = {
      POSTGRES_URL        = "postgres://pulse:${random_password.postgres.result}@${aws_db_instance.postgres.endpoint}/pulse?sslmode=require"
      REDIS_URL           = "rediss://:${random_password.redis.result}@${aws_elasticache_replication_group.redis.primary_endpoint_address}:6379"
      KAFKA_SASL_USERNAME = "pulse"
      KAFKA_SASL_PASSWORD = random_password.kafka.result
      CLICKHOUSE_PASSWORD = random_password.clickhouse.result
      S3_ACCESS_KEY       = aws_iam_access_key.pulse.id
      S3_SECRET_KEY       = aws_iam_access_key.pulse.secret
    }
  }
}

output "backup_bucket" {
  value = aws_s3_bucket.backups.id
}
