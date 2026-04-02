# --- AWS Configuration ---

variable "aws_region" {
  type    = string
  default = "us-east-2"
}

variable "aws_profile" {
  type    = string
  default = "openclaw"
}

variable "project_name" {
  type    = string
  default = "conga-line"
}

variable "repo_root" {
  description = "Absolute path to the congaline repository root"
  type        = string
}

# --- Infrastructure ---

variable "instance_type" {
  type    = string
  default = "r6g.medium"
}

variable "config_check_interval_minutes" {
  type    = number
  default = 5
}

variable "alert_email" {
  type    = string
  default = ""
}

# --- CongaLine ---

variable "image" {
  description = "Docker image for OpenClaw containers"
  type        = string
  default     = "ghcr.io/openclaw/openclaw:2026.3.11"
}

variable "agents" {
  description = "Map of agents to provision. Ports auto-assigned alphabetically from 18789 if omitted."
  type = map(object({
    type         = string
    gateway_port = optional(number)
    binding_id   = string
  }))
}

variable "global_secrets" {
  description = "Secrets applied to every agent (e.g. anthropic-api-key)"
  type      = map(string)
  sensitive = true
  default   = {}
}

variable "channel_secrets" {
  description = "Shared secrets for the messaging channel (e.g. slack-bot-token, slack-signing-secret, slack-app-token)"
  type      = map(string)
  sensitive = true
  default   = {}
}

variable "agent_secrets" {
  description = "Per-agent secrets. Map of agent_name => map of secret_name => value."
  type      = map(map(string))
  sensitive = true
  default   = {}
}

variable "egress_allowed_domains" {
  description = "Global egress allowed domains. Supports wildcards (e.g. *.slack.com)."
  type        = list(string)
  default     = ["api.anthropic.com", "*.slack.com", "*.slack-edge.com"]
}

variable "agent_egress_overrides" {
  description = "Per-agent egress domain overrides. Replaces the global allowlist for that agent."
  type        = map(list(string))
  default     = {}
}
