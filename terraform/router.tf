# Upload router source + bootstrap script to S3
# (user-data has a 16KB limit, so the real bootstrap is in S3)

locals {
  bootstrap_content = templatefile("${path.module}/user-data.sh.tftpl", {
    aws_region                    = var.aws_region
    project_name                  = var.project_name
    config_check_interval_minutes = var.config_check_interval_minutes
    state_bucket                  = local.state_bucket
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
