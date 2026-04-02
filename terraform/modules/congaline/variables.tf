variable "image" {
  description = "Docker image for OpenClaw containers"
  type        = string
  default     = "ghcr.io/openclaw/openclaw:2026.3.11"
}

variable "agents" {
  description = "Map of agents to provision. Key is agent name. Ports auto-assigned alphabetically from 18789 if omitted."
  type = map(object({
    type         = string           # "user" or "team"
    gateway_port = optional(number) # auto-assigned if null
    binding_id   = string           # Slack member ID (user) or channel ID (team)
  }))
}

variable "global_secrets" {
  description = "Secrets applied to every agent (e.g. anthropic-api-key)"
  type        = map(string)
  sensitive   = true
  default     = {}
}

variable "channel_secrets" {
  description = "Shared secrets for the messaging channel (e.g. slack-bot-token, slack-signing-secret, slack-app-token)"
  type        = map(string)
  sensitive   = true
  default     = {}
}

variable "agent_secrets" {
  description = "Per-agent secrets. Map of agent_name => map of secret_name => value."
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
