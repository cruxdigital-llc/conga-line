terraform {
  required_providers {
    conga = {
      source = "registry.terraform.io/cruxdigital-llc/conga"
    }
  }
}

provider "conga" {
  provider_type = "remote"
  ssh_host      = var.ssh_host
  ssh_user      = var.ssh_user
  ssh_key_path  = var.ssh_key_path
}

# Environment setup
resource "conga_environment" "demo" {
  image         = "ghcr.io/openclaw/openclaw:2026.3.11"
  install_docker = true
}

# User agent
resource "conga_agent" "aaron" {
  name         = "aaron"
  type         = "user"
  gateway_port = 18789
  depends_on   = [conga_environment.demo]
}

# Team agent
resource "conga_agent" "team" {
  name         = "team"
  type         = "team"
  gateway_port = 18790
  depends_on   = [conga_environment.demo]
}

# Per-agent API keys
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
  depends_on     = [conga_environment.demo]
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
  binding_id = "C0AQG67NPG9"
}

# Egress policy
resource "conga_policy" "demo" {
  egress_mode            = "enforce"
  egress_allowed_domains = ["api.anthropic.com", "*.slack.com", "*.slack-edge.com"]
  depends_on             = [conga_environment.demo]
}

# Outputs
output "aaron_gateway_port" {
  value = conga_agent.aaron.gateway_port
}

output "team_gateway_port" {
  value = conga_agent.team.gateway_port
}
