terraform {
  required_providers {
    conga = {
      source = "registry.terraform.io/cruxdigital-llc/conga"
    }
  }
}

provider "conga" {
  provider_type = "local"
}

# Environment setup
resource "conga_environment" "main" {
  image = "ghcr.io/openclaw/openclaw:2026.3.11"
}

# User agent
resource "conga_agent" "aaron" {
  name       = "aaron"
  type       = "user"
  depends_on = [conga_environment.main]
}

# Team agent
resource "conga_agent" "team" {
  name       = "team"
  type       = "team"
  depends_on = [conga_environment.main]
}

# Per-agent secret
resource "conga_secret" "aaron_api_key" {
  agent = conga_agent.aaron.name
  name  = "anthropic-api-key"
  value = var.anthropic_api_key
}

resource "conga_secret" "team_api_key" {
  agent = conga_agent.team.name
  name  = "anthropic-api-key"
  value = var.anthropic_api_key
}

# Slack channel
resource "conga_channel" "slack" {
  platform       = "slack"
  bot_token      = var.slack_bot_token
  signing_secret = var.slack_signing_secret
  app_token      = var.slack_app_token
  depends_on     = [conga_environment.main]
}

# Channel bindings
resource "conga_channel_binding" "aaron_slack" {
  agent      = conga_agent.aaron.name
  platform   = conga_channel.slack.platform
  binding_id = "U0ANSPZPG9X"
}

resource "conga_channel_binding" "team_slack" {
  agent      = conga_agent.team.name
  platform   = conga_channel.slack.platform
  binding_id = "C0TEAMCHANNEL"
}

# Policy
resource "conga_policy" "main" {
  egress_mode            = "enforce"
  egress_allowed_domains = ["api.anthropic.com", "*.slack.com", "*.slack-edge.com"]
  depends_on             = [conga_environment.main]
}

# Data sources
data "conga_agent_status" "aaron" {
  name       = conga_agent.aaron.name
  depends_on = [conga_agent.aaron]
}

data "conga_policy" "current" {
  depends_on = [conga_policy.main]
}

data "conga_channels" "all" {
  depends_on = [conga_channel.slack]
}

# Outputs
output "aaron_gateway_port" {
  value = conga_agent.aaron.gateway_port
}

output "aaron_status" {
  value = data.conga_agent_status.aaron.service_state
}

output "policy_mode" {
  value = data.conga_policy.current.egress_mode
}
