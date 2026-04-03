output "state_bucket" {
  description = "S3 bucket name for Terraform state"
  value       = local.state_bucket
}

output "lock_table" {
  description = "DynamoDB table name for state locking"
  value       = local.lock_table
}

output "vpc_id" {
  value = aws_vpc.main.id
}

output "private_subnet_id" {
  value = aws_subnet.private.id
}

output "conga_host_sg_id" {
  value = aws_security_group.conga_host.id
}

output "instance_profile_arn" {
  value = aws_iam_instance_profile.conga_host.arn
}

output "instance_id" {
  value      = aws_instance.conga.id
  depends_on = [terraform_data.bootstrap_ready]
}

output "ssm_connect_command" {
  value = "aws ssm start-session --target ${aws_instance.conga.id} --region ${var.aws_region}"
}

output "kms_key_arn" {
  value = aws_kms_key.ebs.arn
}

output "sns_topic_arn" {
  value = aws_sns_topic.alerts.arn
}

output "data_volume_id" {
  value = aws_ebs_volume.data.id
}

output "ecr_repository_url" {
  value = aws_ecr_repository.conga.repository_url
}

output "aws_region" {
  value = var.aws_region
}

output "aws_profile" {
  value = var.aws_profile
}
