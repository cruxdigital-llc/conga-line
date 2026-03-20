# Upload behavior files to S3 for deployment to agent workspaces.
# These are composed (base + type-specific) and deployed to each agent's
# workspace directory during bootstrap and on every container restart.

locals {
  behavior_files = fileset("${path.module}/../behavior", "**/*")
}

resource "aws_s3_object" "behavior" {
  for_each = local.behavior_files
  bucket   = local.state_bucket
  key      = "openclaw/behavior/${each.value}"
  content  = file("${path.module}/../behavior/${each.value}")
  etag     = md5(file("${path.module}/../behavior/${each.value}"))
}
