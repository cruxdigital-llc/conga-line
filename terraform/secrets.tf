# All secrets (shared and per-agent) are managed by the CLI:
#   conga admin setup              — creates shared secrets from manifest
#   conga secrets set <name>       — creates per-agent secrets
#   conga admin remove-agent --delete-secrets — cleans up on removal
#
# STATE MIGRATION (run once before first `terraform apply` on this branch):
#   These resources were previously managed by Terraform. Remove them from
#   state so Terraform doesn't destroy the live secrets:
#
#   terraform state rm 'aws_secretsmanager_secret.shared'
#   terraform state rm 'aws_secretsmanager_secret_version.shared'
#   terraform state rm 'terraform_data.user_secrets_cleanup'
#
#   If any of these return "No matching objects found", that's fine — it
#   means the resource was never in state (e.g., fresh deployment).
