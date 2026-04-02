terraform {
  required_providers {
    conga = {
      source = "registry.terraform.io/cruxdigital-llc/conga"
    }
  }
}

provider "conga" {
  provider_type = "aws"
  region        = var.aws_region
  profile       = var.aws_profile
}

# Environment — import existing: terraform import conga_environment.prod aws
resource "conga_environment" "prod" {
  image = "ghcr.io/openclaw/openclaw:2026.3.11"
}

# User agents
resource "conga_agent" "aaron" {
  name         = "aaron"
  type         = "user"
  gateway_port = 18789
  depends_on   = [conga_environment.prod]
}

resource "conga_agent" "zach" {
  name         = "zach"
  type         = "user"
  gateway_port = 18791
  depends_on   = [conga_environment.prod]
}

resource "conga_agent" "nathan" {
  name         = "nathan"
  type         = "user"
  gateway_port = 18793
  depends_on   = [conga_environment.prod]
}

# Team agent
resource "conga_agent" "nextgen_delivery" {
  name         = "nextgen-delivery"
  type         = "team"
  gateway_port = 18792
  depends_on   = [conga_environment.prod]
}

# Per-agent API keys
resource "conga_secret" "aaron_api_key" {
  agent = conga_agent.aaron.name
  name  = "anthropic-api-key"
  value = var.anthropic_api_key
}

resource "conga_secret" "zach_api_key" {
  agent = conga_agent.zach.name
  name  = "anthropic-api-key"
  value = var.anthropic_api_key
}

resource "conga_secret" "nathan_api_key" {
  agent = conga_agent.nathan.name
  name  = "anthropic-api-key"
  value = var.anthropic_api_key
}

resource "conga_secret" "nextgen_delivery_api_key" {
  agent = conga_agent.nextgen_delivery.name
  name  = "anthropic-api-key"
  value = var.anthropic_api_key
}

# Aaron's Trello secrets are managed via CLI (already in Secrets Manager).
# Add them here when ready to fully manage via Terraform.

# Slack channels and bindings are managed via `conga admin setup` on AWS.
# The AWS provider handles Slack config through the bootstrap script and
# shared secrets in Secrets Manager — not via AddChannel/BindChannel.
# TODO: Implement AddChannel/BindChannel for AWS provider to enable
# channel management here.

# Egress policy
resource "conga_policy" "prod" {
  egress_mode            = "enforce"
  egress_allowed_domains = ["api.anthropic.com", "*.slack.com", "*.slack-edge.com"]
  depends_on             = [conga_environment.prod]
}

# Outputs
output "agent_ports" {
  value = {
    aaron            = conga_agent.aaron.gateway_port
    zach             = conga_agent.zach.gateway_port
    nathan           = conga_agent.nathan.gateway_port
    nextgen_delivery = conga_agent.nextgen_delivery.gateway_port
  }
}
