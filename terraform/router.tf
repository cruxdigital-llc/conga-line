# Upload router source + bootstrap script to S3
# (user-data has a 16KB limit, so the real bootstrap is in S3)

resource "aws_s3_object" "router_package_json" {
  bucket  = "openclaw-terraform-state-123456789012"
  key     = "openclaw/router/package.json"
  content = file("${path.module}/../router/package.json")
  etag    = md5(file("${path.module}/../router/package.json"))
}

resource "aws_s3_object" "router_index_js" {
  bucket  = "openclaw-terraform-state-123456789012"
  key     = "openclaw/router/src/index.js"
  content = file("${path.module}/../router/src/index.js")
  etag    = md5(file("${path.module}/../router/src/index.js"))
}

resource "aws_s3_object" "bootstrap_script" {
  bucket = "openclaw-terraform-state-123456789012"
  key    = "openclaw/bootstrap/bootstrap.sh"
  content = templatefile("${path.module}/user-data.sh.tftpl", {
    aws_region                    = var.aws_region
    project_name                  = var.project_name
    users                         = var.users
    config_check_interval_minutes = var.config_check_interval_minutes
    routing_json = jsonencode({
      channels = { for uid, cfg in var.users : cfg.slack_channel => "http://openclaw-${uid}:18789/slack/events" }
      members  = { for uid, cfg in var.users : uid => "http://openclaw-${uid}:18789/slack/events" }
    })
  })
  etag = md5(templatefile("${path.module}/user-data.sh.tftpl", {
    aws_region                    = var.aws_region
    project_name                  = var.project_name
    users                         = var.users
    config_check_interval_minutes = var.config_check_interval_minutes
    routing_json = jsonencode({
      channels = { for uid, cfg in var.users : cfg.slack_channel => "http://openclaw-${uid}:18789/slack/events" }
      members  = { for uid, cfg in var.users : uid => "http://openclaw-${uid}:18789/slack/events" }
    })
  }))
}
