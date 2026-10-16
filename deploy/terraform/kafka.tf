resource "random_password" "kafka" {
  length  = 32
  special = false
}

resource "aws_kms_key" "kafka" {
  description = "${var.name} MSK SCRAM secret"
}

# MSK SCRAM secrets must be named AmazonMSK_* and use a customer managed key.
resource "aws_secretsmanager_secret" "kafka" {
  name       = "AmazonMSK_${var.name}"
  kms_key_id = aws_kms_key.kafka.key_id
}

resource "aws_secretsmanager_secret_version" "kafka" {
  secret_id     = aws_secretsmanager_secret.kafka.id
  secret_string = jsonencode({ username = "pulse", password = random_password.kafka.result })
}

resource "aws_msk_configuration" "kafka" {
  name           = "${var.name}-kafka"
  kafka_versions = ["3.9.x"]
  # The services create raw.* and issue topics on first write.
  server_properties = <<-EOT
    auto.create.topics.enable=true
    num.partitions=6
    default.replication.factor=3
    min.insync.replicas=2
    log.retention.hours=72
  EOT
}

resource "aws_msk_cluster" "kafka" {
  cluster_name           = "${var.name}-kafka"
  kafka_version          = "3.9.x"
  number_of_broker_nodes = var.kafka_broker_count

  broker_node_group_info {
    instance_type   = var.kafka_instance_type
    client_subnets  = var.subnet_ids
    security_groups = [aws_security_group.data.id]
    storage_info {
      ebs_storage_info {
        volume_size = var.kafka_volume_gb
      }
    }
  }

  configuration_info {
    arn      = aws_msk_configuration.kafka.arn
    revision = aws_msk_configuration.kafka.latest_revision
  }

  client_authentication {
    sasl {
      scram = true
    }
  }

  encryption_info {
    encryption_in_transit {
      client_broker = "TLS"
    }
  }
}

resource "aws_msk_scram_secret_association" "kafka" {
  cluster_arn     = aws_msk_cluster.kafka.arn
  secret_arn_list = [aws_secretsmanager_secret.kafka.arn]
  depends_on      = [aws_secretsmanager_secret_version.kafka]
}
