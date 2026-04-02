variable "anthropic_api_key" {
  type      = string
  sensitive = true
  default   = "sk-test-placeholder"
}

variable "slack_bot_token" {
  type      = string
  sensitive = true
  default   = "xoxb-test-placeholder"
}

variable "slack_signing_secret" {
  type      = string
  sensitive = true
  default   = "test-signing-secret"
}

variable "slack_app_token" {
  type      = string
  sensitive = true
  default   = "xapp-test-placeholder"
}
