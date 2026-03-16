output "state_bucket" {
  description = "S3 bucket name for Terraform state"
  value       = "openclaw-terraform-state-123456789012"
}

output "lock_table" {
  description = "DynamoDB table name for state locking"
  value       = "openclaw-terraform-locks"
}
