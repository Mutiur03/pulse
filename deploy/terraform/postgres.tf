resource "random_password" "postgres" {
  length  = 32
  special = false
}

resource "aws_db_subnet_group" "postgres" {
  name       = "${var.name}-postgres"
  subnet_ids = var.subnet_ids
}

resource "aws_db_instance" "postgres" {
  identifier                   = "${var.name}-postgres"
  engine                       = "postgres"
  engine_version               = "16"
  instance_class               = var.postgres_instance_class
  allocated_storage            = var.postgres_storage_gb
  max_allocated_storage        = var.postgres_storage_gb * 4
  storage_encrypted            = true
  db_name                      = "pulse"
  username                     = "pulse"
  password                     = random_password.postgres.result
  db_subnet_group_name         = aws_db_subnet_group.postgres.name
  vpc_security_group_ids       = [aws_security_group.data.id]
  multi_az                     = var.environment == "production"
  backup_retention_period      = 14
  deletion_protection          = true
  skip_final_snapshot          = false
  final_snapshot_identifier    = "${var.name}-postgres-final"
  performance_insights_enabled = true
}
