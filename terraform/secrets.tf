# All secrets (shared and per-agent) are managed by the CLI:
#   cruxclaw admin setup              — creates shared secrets from manifest
#   cruxclaw secrets set <name>       — creates per-agent secrets
#   cruxclaw admin remove-agent --delete-secrets — cleans up on removal

# State migration: these resources were previously managed by Terraform.
# The `removed` blocks tell Terraform to forget them without destroying
# the actual secrets in AWS, so the CLI can take over management.

removed {
  from = aws_secretsmanager_secret.shared
  lifecycle {
    destroy = false
  }
}

removed {
  from = aws_secretsmanager_secret_version.shared
  lifecycle {
    destroy = false
  }
}

removed {
  from = terraform_data.user_secrets_cleanup
  lifecycle {
    destroy = false
  }
}
