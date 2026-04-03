variable "image" {
  description = "Docker image for OpenClaw containers"
  type        = string
  default     = "ghcr.io/openclaw/openclaw:2026.3.11"
}

variable "agents" {
  description = "Map of agents to provision. Key is agent name. Ports auto-assigned alphabetically from 18789 if omitted."
  type = map(object({
    type                   = string                    # "user" or "team"
    gateway_port           = optional(number)          # auto-assigned if null
    egress_mode            = optional(string)          # per-agent override; null = inherit global ("enforce" or "validate")
    egress_allowed_domains = optional(list(string))    # per-agent override; null = inherit global
    secrets                = optional(map(string), {}) # per-agent secrets
  }))
}

variable "global_secrets" {
  description = "Secrets applied to every agent (e.g. anthropic-api-key)"
  type        = map(string)
  sensitive   = true
  default     = {}
}

variable "channels" {
  description = "Messaging channels. Map of platform => { secrets, bindings }. Bindings map agent name => config with required `id` key."
  type = map(object({
    secrets  = map(string)
    bindings = optional(map(map(string)), {})
  }))
  default = {}
}


variable "egress_mode" {
  description = "Global egress enforcement mode"
  type        = string
  default     = "enforce"
}

variable "egress_allowed_domains" {
  description = "Global egress allowed domains"
  type        = list(string)
  default     = ["api.anthropic.com", "*.slack.com", "*.slack-edge.com"]
}
