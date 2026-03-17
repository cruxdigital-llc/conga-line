locals {
  shared_secrets = {
    "openclaw/shared/slack-bot-token"      = "Slack bot token (xoxb-)"
    "openclaw/shared/slack-app-token"      = "Slack app token (xapp-)"
    "openclaw/shared/slack-signing-secret" = "Slack signing secret"
  }
}

resource "aws_secretsmanager_secret" "shared" {
  for_each    = local.shared_secrets
  name        = each.key
  description = each.value

  tags = {
    Name = each.key
  }
}

resource "aws_secretsmanager_secret_version" "shared" {
  for_each      = local.shared_secrets
  secret_id     = aws_secretsmanager_secret.shared[each.key].id
  secret_string = "REPLACE_ME"

  lifecycle {
    ignore_changes = [secret_string]
  }
}
