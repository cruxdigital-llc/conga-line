# Upload behavior files to S3 for deployment to agent workspaces.
# These are composed (base + type-specific) and deployed to each agent's
# workspace directory during bootstrap and on every container restart.

locals {
  behavior_files = {
    for f in fileset("${var.repo_root}/behavior", "**/*") : f => f
    if !endswith(f, ".gitkeep")
  }
}

resource "aws_s3_object" "behavior" {
  for_each = local.behavior_files
  bucket   = local.state_bucket
  key      = "conga/behavior/${each.value}"
  content  = file("${var.repo_root}/behavior/${each.value}")
  etag     = md5(file("${var.repo_root}/behavior/${each.value}"))
}

# Upload deploy helper to S3 — single source of truth for bootstrap and provisioning
resource "aws_s3_object" "deploy_behavior_helper" {
  bucket  = local.state_bucket
  key     = "conga/scripts/deploy-behavior.sh"
  content = file("${var.repo_root}/scripts/deploy-behavior.sh.tmpl")
  etag    = md5(file("${var.repo_root}/scripts/deploy-behavior.sh.tmpl"))
}
