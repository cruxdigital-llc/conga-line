output "state_bucket" {
  description = "S3 bucket name for Terraform state"
  value       = local.state_bucket
}

output "lock_table" {
  description = "DynamoDB table name for state locking"
  value       = local.lock_table
}

output "vpc_id" {
  description = "VPC ID"
  value       = aws_vpc.main.id
}

output "private_subnet_id" {
  description = "Private subnet ID for OpenClaw host"
  value       = aws_subnet.private.id
}

output "openclaw_host_sg_id" {
  description = "Security group ID for OpenClaw host"
  value       = aws_security_group.openclaw_host.id
}

output "instance_profile_arn" {
  description = "IAM instance profile ARN for OpenClaw host"
  value       = aws_iam_instance_profile.openclaw_host.arn
}

output "instance_profile_name" {
  description = "IAM instance profile name for OpenClaw host"
  value       = aws_iam_instance_profile.openclaw_host.name
}

output "kms_key_arn" {
  description = "KMS key ARN for EBS encryption"
  value       = aws_kms_key.ebs.arn
}

output "gateway_log_group" {
  description = "CloudWatch log group for gateway logs"
  value       = aws_cloudwatch_log_group.gateway.name
}

output "instance_id" {
  description = "OpenClaw host EC2 instance ID"
  value       = aws_instance.openclaw.id
}

output "ssm_connect_command" {
  description = "Command to connect via SSM"
  value       = "aws ssm start-session --target ${aws_instance.openclaw.id} --region ${var.aws_region}"
}

output "sns_topic_arn" {
  description = "SNS topic ARN for alerts"
  value       = aws_sns_topic.alerts.arn
}

output "config_check_interval" {
  description = "Config integrity check interval in minutes"
  value       = var.config_check_interval_minutes
}

output "data_volume_id" {
  description = "Persistent EBS data volume ID"
  value       = aws_ebs_volume.data.id
}

output "ecr_repository_url" {
  description = "ECR repository URL for custom OpenClaw image"
  value       = aws_ecr_repository.openclaw.repository_url
}

output "ssm_port_forward_commands" {
  description = "SSM port forwarding commands per agent (local port always 18789 for OAuth redirects)"
  value = {
    for name, cfg in var.agents : name => join(" ", [
      "aws ssm start-session",
      "--target ${aws_instance.openclaw.id}",
      "--region ${var.aws_region}",
      "--document-name AWS-StartPortForwardingSession",
      "--parameters '{\"portNumber\":[\"${cfg.gateway_port}\"],\"localPortNumber\":[\"18789\"]}'"
    ])
  }
}
