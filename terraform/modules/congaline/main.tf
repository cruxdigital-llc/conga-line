terraform {
  required_providers {
    conga = {
      source = "registry.terraform.io/cruxdigital-llc/conga"
    }
  }
}

# Compute deterministic port assignments from sorted agent names.
# Base port is 18789 (common.BaseGatewayPort). Each agent gets the next port
# in alphabetical order. Explicit gateway_port in the agent map overrides this.
locals {
  sorted_agents = sort(keys(var.agents))
  agent_ports = {
    for i, name in local.sorted_agents :
    name => var.agents[name].gateway_port != null ? var.agents[name].gateway_port : 18789 + i
  }
}

# Environment setup
resource "conga_environment" "this" {
  image = var.image
}

# Agents — one per entry in var.agents
resource "conga_agent" "this" {
  for_each     = var.agents
  name         = each.key
  type         = each.value.type
  gateway_port = local.agent_ports[each.key]
  depends_on   = [conga_environment.this]
}

# Per-agent API key
resource "conga_secret" "anthropic_api_key" {
  for_each = var.agents
  agent    = each.key
  name     = "anthropic-api-key"
  value    = var.anthropic_api_key
  depends_on = [conga_agent.this]
}

# Extra per-agent secrets
resource "conga_secret" "extra" {
  for_each = merge([
    for agent, secrets in var.extra_secrets : {
      for name, value in secrets : "${agent}/${name}" => {
        agent = agent
        name  = name
        value = value
      }
    }
  ]...)

  agent      = each.value.agent
  name       = each.value.name
  value      = each.value.value
  depends_on = [conga_agent.this]
}

# Slack channel
resource "conga_channel" "slack" {
  platform       = "slack"
  bot_token      = var.slack_bot_token
  signing_secret = var.slack_signing_secret
  app_token      = var.slack_app_token
  depends_on     = [conga_environment.this]
}

# Channel bindings — one per agent that has a binding_id
resource "conga_channel_binding" "slack" {
  for_each   = { for k, v in var.agents : k => v if v.binding_id != "" }
  agent      = each.key
  platform   = conga_channel.slack.platform
  binding_id = each.value.binding_id
  depends_on = [conga_agent.this]
}

# Egress policy with optional per-agent overrides
resource "conga_policy" "this" {
  egress_mode            = "enforce"
  egress_allowed_domains = var.egress_allowed_domains

  dynamic "agent_override" {
    for_each = var.agent_egress_overrides
    content {
      name                   = agent_override.key
      egress_allowed_domains = agent_override.value
    }
  }

  depends_on = [conga_environment.this]
}
