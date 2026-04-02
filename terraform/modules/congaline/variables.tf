variable "image" {
  description = "Docker image for OpenClaw containers"
  type        = string
  default     = "ghcr.io/openclaw/openclaw:2026.3.11"
}

variable "agents" {
  description = "Map of agents to provision. Key is agent name. Ports auto-assigned alphabetically from 18789 if omitted."
  type = map(object({
    type         = string         # "user" or "team"
    gateway_port = optional(number) # auto-assigned if null
    binding_id   = string         # Slack member ID (user) or channel ID (team)
  }))
}

variable "anthropic_api_key" {
  description = "Anthropic API key (shared across all agents)"
  type        = string
  sensitive   = true
}

variable "slack_bot_token" {
  description = "Slack bot token"
  type        = string
  sensitive   = true
}

variable "slack_signing_secret" {
  description = "Slack signing secret"
  type        = string
  sensitive   = true
}

variable "slack_app_token" {
  description = "Slack app token for Socket Mode"
  type        = string
  sensitive   = true
}

variable "extra_secrets" {
  description = "Additional per-agent secrets. Map of agent_name => map of secret_name => value."
  type        = map(map(string))
  sensitive   = true
  default     = {}
}

variable "egress_allowed_domains" {
  description = "Global egress allowed domains"
  type        = list(string)
  default     = ["api.anthropic.com", "*.slack.com", "*.slack-edge.com"]
}

variable "agent_egress_overrides" {
  description = "Per-agent egress domain overrides. Map of agent_name => list of allowed domains."
  type        = map(list(string))
  default     = {}
}
