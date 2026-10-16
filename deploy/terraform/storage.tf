resource "aws_s3_bucket" "replays" {
  bucket = "${var.name}-replays-${var.environment}"
}

resource "aws_s3_bucket" "artifacts" {
  bucket = "${var.name}-artifacts-${var.environment}"
}

resource "aws_s3_bucket" "backups" {
  bucket = "${var.name}-backups-${var.environment}"
}

locals {
  buckets = {
    replays   = aws_s3_bucket.replays.id
    artifacts = aws_s3_bucket.artifacts.id
    backups   = aws_s3_bucket.backups.id
  }
}

resource "aws_s3_bucket_public_access_block" "all" {
  for_each                = local.buckets
  bucket                  = each.value
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_server_side_encryption_configuration" "all" {
  for_each = local.buckets
  bucket   = each.value
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

# Backstop for replay objects; the governance worker applies each project's
# shorter period.
resource "aws_s3_bucket_lifecycle_configuration" "replays" {
  bucket = aws_s3_bucket.replays.id
  rule {
    id     = "expire-replays"
    status = "Enabled"
    filter {}
    expiration {
      days = var.replay_retention_days
    }
  }
}

resource "aws_s3_bucket_versioning" "backups" {
  bucket = aws_s3_bucket.backups.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "backups" {
  bucket = aws_s3_bucket.backups.id
  rule {
    id     = "expire-old-backups"
    status = "Enabled"
    filter {}
    expiration {
      days = 35
    }
    noncurrent_version_expiration {
      noncurrent_days = 7
    }
  }
}

# Service user for the Pulse pods. Prefer IRSA/pod identity in production
# and drop the access key.
resource "aws_iam_user" "pulse" {
  name = "${var.name}-services"
}

resource "aws_iam_access_key" "pulse" {
  user = aws_iam_user.pulse.name
}

resource "aws_iam_user_policy" "pulse" {
  name = "${var.name}-buckets"
  user = aws_iam_user.pulse.name
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["s3:GetObject", "s3:PutObject", "s3:DeleteObject", "s3:ListBucket"]
      Resource = flatten([for b in [aws_s3_bucket.replays, aws_s3_bucket.artifacts, aws_s3_bucket.backups] : [b.arn, "${b.arn}/*"]])
    }]
  })
}
