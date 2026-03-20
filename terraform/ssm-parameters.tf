# --- SSM Parameter Store ---
# The setup manifest tells the CLI what config values and shared secrets
# are required. `cruxclaw admin setup` reads this and prompts for values.
# Config values are stored in SSM at /openclaw/config/<key>.
# Shared secrets are stored in Secrets Manager at the specified path.

resource "aws_ssm_parameter" "setup_manifest" {
  name  = "/openclaw/config/setup-manifest"
  type  = "String"
  value = jsonencode(var.setup_manifest)
  tags = {
    Project = var.project_name
  }
}

resource "aws_ssm_parameter" "state_bucket" {
  name  = "/openclaw/config/state-bucket"
  type  = "String"
  value = local.state_bucket
  tags = {
    Project = var.project_name
  }
}
