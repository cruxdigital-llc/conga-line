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

variable "setup_manifest" {
  type = object({
    config   = map(string)
    defaults = optional(map(string), {})
    secrets  = map(string)
  })
  default = {
    config = {
      "image" = "Docker image for OpenClaw (ECR, GHCR, or Docker Hub)"
    }
    defaults = {
      "image" = "ghcr.io/openclaw/openclaw:2026.3.11"
    }
    secrets = {
      "conga/shared/slack-bot-token"      = "Slack bot token (xoxb-)"
      "conga/shared/slack-app-token"      = "Slack app token (xapp-)"
      "conga/shared/slack-signing-secret" = "Slack signing secret"
      "conga/shared/google-client-id"     = "Google OAuth client ID"
      "conga/shared/google-client-secret" = "Google OAuth client secret"
    }
  }
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

variable "anthropic_api_key" {
  type      = string
  sensitive = true
}

variable "slack_bot_token" {
  type      = string
  sensitive = true
}

variable "slack_signing_secret" {
  type      = string
  sensitive = true
}

variable "slack_app_token" {
  type      = string
  sensitive = true
}

variable "extra_secrets" {
  type      = map(map(string))
  sensitive = true
  default   = {}
}

variable "egress_allowed_domains" {
  type    = list(string)
  default = ["api.anthropic.com", "*.slack.com", "*.slack-edge.com"]
}

variable "agent_egress_overrides" {
  type    = map(list(string))
  default = {}
}
