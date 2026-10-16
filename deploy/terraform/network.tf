resource "aws_security_group" "data" {
  name        = "${var.name}-data"
  description = "Pulse data services"
  vpc_id      = var.vpc_id

  dynamic "ingress" {
    for_each = { postgres = 5432, redis = 6379, kafka_scram = 9096 }
    content {
      description = ingress.key
      from_port   = ingress.value
      to_port     = ingress.value
      protocol    = "tcp"
      cidr_blocks = var.allowed_cidr_blocks
    }
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}
