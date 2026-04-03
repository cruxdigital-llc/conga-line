terraform {
  required_providers {
    conga = {
      source  = "registry.terraform.io/cruxdigital-llc/conga"
      version = "0.1.4"
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

locals {
  global_secret_keys = merge([
    for agent in keys(var.agents) : {
      for name in nonsensitive(keys(var.global_secrets)) : "${agent}/${name}" => {
        agent = agent
        name  = name
      }
    }
  ]...)

  agent_secret_keys = merge(flatten([
    for agent, cfg in var.agents : [
      for name in keys(cfg.secrets) : {
        "${agent}/${name}" = {
          agent = agent
          name  = name
        }
      }
    ]
  ])...)

  channel_bindings = merge(flatten([
    for platform, ch in var.channels : [
      for agent, config in ch.bindings : {
        "${agent}/${platform}" = {
          agent      = agent
          platform   = platform
          binding_id = config.id
        }
      }
    ]
  ])...)
}

# Global secrets applied to every agent (e.g. anthropic-api-key)
resource "conga_secret" "global" {
  for_each   = local.global_secret_keys
  agent      = each.value.agent
  name       = each.value.name
  value      = var.global_secrets[each.value.name]
  depends_on = [conga_agent.this]
}

# Per-agent secrets (e.g. aaron's trello keys)
resource "conga_secret" "agent" {
  for_each   = local.agent_secret_keys
  agent      = each.value.agent
  name       = each.value.name
  value      = var.agents[each.value.agent].secrets[each.value.name]
  depends_on = [conga_agent.this]
}

# Messaging channels — one per platform
resource "conga_channel" "this" {
  for_each   = var.channels
  platform   = each.key
  secrets    = each.value.secrets
  depends_on = [conga_environment.this]
}

# Channel bindings — agent × platform
resource "conga_channel_binding" "this" {
  for_each   = local.channel_bindings
  agent      = each.value.agent
  platform   = each.value.platform
  binding_id = each.value.binding_id
  depends_on = [conga_agent.this, conga_channel.this]
}

# Egress policy — per-agent overrides derived from agent definitions
resource "conga_policy" "this" {
  egress_mode            = var.egress_mode
  egress_allowed_domains = var.egress_allowed_domains

  dynamic "agent_override" {
    for_each = { for name, cfg in var.agents : name => cfg if cfg.egress_allowed_domains != null }
    content {
      name                   = agent_override.key
      egress_allowed_domains = agent_override.value.egress_allowed_domains
    }
  }

  depends_on = [conga_environment.this]
}
