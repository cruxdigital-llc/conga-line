# Clean up per-user secrets when a user agent is removed from var.agents.
# These secrets are created out-of-band by the CLI (cruxclaw secrets set),
# so Terraform doesn't track them as resources — only handles deletion.
resource "terraform_data" "user_secrets_cleanup" {
  for_each = local.user_agents

  input = {
    member_id = each.value.member_id
    region    = var.aws_region
    profile   = var.aws_profile
  }

  provisioner "local-exec" {
    when    = destroy
    command = <<-EOT
      echo "Cleaning up secrets for user ${self.input.member_id}..."
      for secret_arn in $(aws secretsmanager list-secrets \
        --filter Key=name,Values="openclaw/${self.input.member_id}/" \
        --query 'SecretList[].ARN' --output text \
        --region "${self.input.region}" --profile "${self.input.profile}"); do
        echo "Deleting secret: $secret_arn"
        aws secretsmanager delete-secret \
          --secret-id "$secret_arn" \
          --force-delete-without-recovery \
          --region "${self.input.region}" --profile "${self.input.profile}"
      done
      echo "Secret cleanup complete for ${self.input.member_id}"
    EOT
  }
}

resource "aws_secretsmanager_secret" "shared" {
  for_each    = var.shared_secrets
  name        = each.key
  description = each.value

  tags = {
    Name = each.key
  }
}

resource "aws_secretsmanager_secret_version" "shared" {
  for_each      = var.shared_secrets
  secret_id     = aws_secretsmanager_secret.shared[each.key].id
  secret_string = "REPLACE_ME"

  lifecycle {
    ignore_changes = [secret_string]
  }
}
