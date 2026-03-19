# --- SSM Parameter Store for CLI Discovery ---
# These parameters allow the CruxClaw CLI to discover agent config
# without needing Terraform state access.

# User agents: keyed by member_id (existing path pattern)
resource "aws_ssm_parameter" "user_config" {
  for_each = local.user_agents
  name     = "/openclaw/users/${each.value.member_id}"
  type     = "String"
  value = jsonencode({
    agent_name   = each.key
    gateway_port = each.value.gateway_port
  })
  tags = {
    Project = var.project_name
  }
}

# Team agents: keyed by team name
resource "aws_ssm_parameter" "team_config" {
  for_each = local.team_agents
  name     = "/openclaw/teams/${each.key}"
  type     = "String"
  value = jsonencode({
    slack_channel = each.value.slack_channel
    gateway_port  = each.value.gateway_port
  })
  tags = {
    Project = var.project_name
  }
}

# IAM mapping: only for user agents with an iam_identity
resource "aws_ssm_parameter" "user_iam_mapping" {
  for_each = { for k, v in local.user_agents : k => v if v.iam_identity != "" }
  name     = "/openclaw/users/by-iam/${each.value.iam_identity}"
  type     = "String"
  value    = each.value.member_id
  tags = {
    Project = var.project_name
  }
}
