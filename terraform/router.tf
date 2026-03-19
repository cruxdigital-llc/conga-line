# Upload router source + bootstrap script to S3
# (user-data has a 16KB limit, so the real bootstrap is in S3)

locals {
  bootstrap_content = templatefile("${path.module}/user-data.sh.tftpl", {
    aws_region                    = var.aws_region
    project_name                  = var.project_name
    agents                        = var.agents
    agent_container_id            = local.agent_container_id
    config_check_interval_minutes = var.config_check_interval_minutes
    openclaw_image                = var.openclaw_image
    state_bucket                  = local.state_bucket
    routing_json = jsonencode({
      channels = { for k, v in local.team_agents : v.slack_channel => "http://openclaw-${k}:18789/slack/events" }
      members  = { for k, v in local.user_agents : v.member_id => "http://openclaw-${v.member_id}:18789/slack/events" }
    })
  })
}

resource "aws_s3_object" "router_package_json" {
  bucket  = local.state_bucket
  key     = "openclaw/router/package.json"
  content = file("${path.module}/../router/package.json")
  etag    = md5(file("${path.module}/../router/package.json"))
}

resource "aws_s3_object" "router_index_js" {
  bucket  = local.state_bucket
  key     = "openclaw/router/src/index.js"
  content = file("${path.module}/../router/src/index.js")
  etag    = md5(file("${path.module}/../router/src/index.js"))
}

resource "aws_s3_object" "bootstrap_script" {
  bucket  = local.state_bucket
  key     = "openclaw/bootstrap/bootstrap.sh"
  content = local.bootstrap_content
  etag    = md5(local.bootstrap_content)
}
