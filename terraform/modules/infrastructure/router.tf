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
  key     = "conga/router/slack/package.json"
  content = file("${var.repo_root}/router/slack/package.json")
  etag    = md5(file("${var.repo_root}/router/slack/package.json"))
}

resource "aws_s3_object" "router_index_js" {
  bucket  = local.state_bucket
  key     = "conga/router/slack/src/index.js"
  content = file("${var.repo_root}/router/slack/src/index.js")
  etag    = md5(file("${var.repo_root}/router/slack/src/index.js"))
}

resource "aws_s3_object" "bootstrap_script" {
  bucket  = local.state_bucket
  key     = "conga/bootstrap/bootstrap.sh"
  content = local.bootstrap_content
  etag    = md5(local.bootstrap_content)
}

# Seed the egress policy to S3 so fresh instances bootstrap with correct
# Envoy configs instead of deny-all. The conga_policy terraform resource
# handles ongoing updates; this just ensures first boot works.
resource "aws_s3_object" "egress_policy" {
  count  = length(var.egress_allowed_domains) > 0 ? 1 : 0
  bucket = local.state_bucket
  key    = "conga/conga-policy.yaml"
  content = yamlencode({
    apiVersion = "conga.dev/v1alpha1"
    egress = {
      mode            = var.egress_mode
      allowed_domains = var.egress_allowed_domains
    }
  })
  etag = md5(yamlencode({
    apiVersion = "conga.dev/v1alpha1"
    egress = {
      mode            = var.egress_mode
      allowed_domains = var.egress_allowed_domains
    }
  }))
}
