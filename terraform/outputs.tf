output "state_bucket" {
  description = "S3 bucket name for Terraform state"
  value       = "openclaw-terraform-state-167595588574"
}

output "lock_table" {
  description = "DynamoDB table name for state locking"
  value       = "openclaw-terraform-locks"
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
