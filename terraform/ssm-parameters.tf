# --- SSM Parameter Store for CLI Discovery ---
# These parameters allow the CruxClaw CLI to discover user config
# without needing Terraform state access.

resource "aws_ssm_parameter" "user_config" {
  for_each = var.users
  name     = "/openclaw/users/${each.key}"
  type     = "String"
  value = jsonencode({
    slack_channel = each.value.slack_channel
    gateway_port  = each.value.gateway_port
  })
  tags = {
    Project = var.project_name
  }
}

resource "aws_ssm_parameter" "user_iam_mapping" {
  for_each = { for k, v in var.users : k => v if v.iam_identity != "" }
  name     = "/openclaw/users/by-iam/${each.value.iam_identity}"
  type     = "String"
  value    = each.key
  tags = {
    Project = var.project_name
  }
}
