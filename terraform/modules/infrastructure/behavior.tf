# Upload behavior files to S3 for deployment to agent workspaces.
# Files under default/ provide shared defaults; files under agents/<name>/
# override the defaults for specific agents. Deployed during bootstrap
# and on every container restart via deploy-behavior.sh.

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

# Restart all agents when behavior files or the deploy helper change.
# The ExecStartPre in each agent's systemd unit syncs from S3 and runs
# deploy-behavior.sh, so a restart is sufficient to pick up changes.
locals {
  behavior_content_hash = md5(join("", [
    for f in sort(keys(local.behavior_files)) :
    md5(file("${var.repo_root}/behavior/${f}"))
  ]))
  deploy_helper_hash = md5(file("${var.repo_root}/scripts/deploy-behavior.sh.tmpl"))
}

resource "terraform_data" "behavior_refresh" {
  depends_on = [
    aws_s3_object.behavior,
    aws_s3_object.deploy_behavior_helper,
    terraform_data.bootstrap_ready,
  ]

  triggers_replace = "${local.behavior_content_hash}-${local.deploy_helper_hash}"

  provisioner "local-exec" {
    interpreter = ["bash", "-c"]
    command     = <<-EOT
      echo "Behavior files changed — restarting all agents..."
      RESULT=$(aws ssm send-command \
        --instance-ids "${aws_instance.conga.id}" \
        --document-name "AWS-RunShellScript" \
        --parameters '{"commands":["for svc in $(systemctl list-units --type=service --state=running --no-legend conga-*.service | awk \x27{print $1}\x27 | grep -v router); do echo \"Restarting $svc...\"; systemctl restart $svc; sleep 2; done; echo DONE"]}' \
        --timeout-seconds 120 \
        --region "${var.aws_region}" \
        --profile "${var.aws_profile}" \
        --output text --query "Command.CommandId" 2>/dev/null)
      if [ -z "$RESULT" ]; then
        echo "WARNING: Failed to send restart command — agents will pick up changes on next manual restart"
        exit 0
      fi
      sleep 15
      OUTPUT=$(aws ssm get-command-invocation \
        --command-id "$RESULT" \
        --instance-id "${aws_instance.conga.id}" \
        --region "${var.aws_region}" \
        --profile "${var.aws_profile}" \
        --output text --query "[Status, StandardOutputContent]" 2>/dev/null)
      echo "$OUTPUT"
      if echo "$OUTPUT" | grep -q "DONE"; then
        echo "All agents restarted with updated behavior files."
      else
        echo "WARNING: Agent restart may not have completed — check 'conga status' manually"
      fi
    EOT
  }
}
