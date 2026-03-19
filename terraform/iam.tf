# --- Instance Role ---

resource "aws_iam_role" "openclaw_host" {
  name_prefix = "${var.project_name}-host-"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = {
        Service = "ec2.amazonaws.com"
      }
    }]
  })

  tags = {
    Name = "${var.project_name}-host-role"
  }
}

resource "aws_iam_instance_profile" "openclaw_host" {
  name_prefix = "${var.project_name}-host-"
  role        = aws_iam_role.openclaw_host.name
}

# --- SSM Access (AWS managed policy) ---

resource "aws_iam_role_policy_attachment" "ssm" {
  role       = aws_iam_role.openclaw_host.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

# --- Secrets Manager Read ---

resource "aws_iam_role_policy" "secrets_read" {
  name_prefix = "${var.project_name}-secrets-"
  role        = aws_iam_role.openclaw_host.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "secretsmanager:GetSecretValue"
        ]
        Resource = [
          "arn:aws:secretsmanager:${var.aws_region}:${data.aws_caller_identity.current.account_id}:secret:openclaw/shared/*",
          "arn:aws:secretsmanager:${var.aws_region}:${data.aws_caller_identity.current.account_id}:secret:openclaw/agents/*"
        ]
      },
      {
        Effect   = "Allow"
        Action   = ["secretsmanager:ListSecrets"]
        Resource = "*"
      },
      {
        Effect = "Allow"
        Action = [
          "ecr:GetAuthorizationToken"
        ]
        Resource = "*"
      },
      {
        Effect = "Allow"
        Action = [
          "ecr:GetDownloadUrlForLayer",
          "ecr:BatchGetImage",
          "ecr:BatchCheckLayerAvailability"
        ]
        Resource = aws_ecr_repository.openclaw.arn
      },
      {
        Effect   = "Allow"
        Action   = ["cloudwatch:PutMetricData"]
        Resource = "*"
        Condition = {
          StringEquals = {
            "cloudwatch:namespace" = "OpenClaw"
          }
        }
      },
      {
        Effect = "Allow"
        Action = ["cloudwatch:PutDashboard"]
        Resource = [
          "arn:aws:cloudwatch::${data.aws_caller_identity.current.account_id}:dashboard/${var.project_name}"
        ]
      }
    ]
  })
}

# --- S3 Read (bootstrap + router artifacts) ---

resource "aws_iam_role_policy" "s3_read" {
  name_prefix = "${var.project_name}-s3-"
  role        = aws_iam_role.openclaw_host.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = ["s3:GetObject"]
      Resource = [
        "arn:aws:s3:::${local.state_bucket}/openclaw/router/*",
        "arn:aws:s3:::${local.state_bucket}/openclaw/bootstrap/*"
      ]
    }]
  })
}

# --- CloudWatch Logs ---

resource "aws_cloudwatch_log_group" "gateway" {
  name              = "/${var.project_name}/gateway"
  retention_in_days = 30

  tags = {
    Name = "${var.project_name}-gateway-logs"
  }
}

resource "aws_iam_role_policy" "cloudwatch_logs" {
  name_prefix = "${var.project_name}-logs-"
  role        = aws_iam_role.openclaw_host.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = [
        "logs:CreateLogGroup",
        "logs:CreateLogStream",
        "logs:PutLogEvents",
        "logs:DescribeLogGroups",
        "logs:DescribeLogStreams",
        "logs:PutRetentionPolicy"
      ]
      Resource = [
        "${aws_cloudwatch_log_group.gateway.arn}",
        "${aws_cloudwatch_log_group.gateway.arn}:*"
      ]
    }]
  })
}

# --- Deny Dangerous Actions ---

resource "aws_iam_role_policy" "deny_dangerous" {
  name_prefix = "${var.project_name}-deny-"
  role        = aws_iam_role.openclaw_host.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Deny"
      Action = [
        "iam:*",
        "organizations:*",
        "sts:AssumeRole",
        "ec2:RunInstances",
        "ec2:CreateVpc",
        "ec2:CreateSecurityGroup",
        "ec2:AuthorizeSecurityGroupIngress",
        "lambda:*",
        "s3:DeleteBucket",
        "s3:PutBucketPolicy"
      ]
      Resource = "*"
    }]
  })
}
