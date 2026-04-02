resource "conga_channel" "slack" {
  platform = "slack"
  secrets = {
    "slack-bot-token"      = var.slack_bot_token
    "slack-signing-secret" = var.slack_signing_secret
    "slack-app-token"      = var.slack_app_token
  }
}
