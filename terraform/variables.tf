variable "aws_region" {
  description = "AWS region for all resources"
  type        = string
  default     = "us-east-2"
}

variable "aws_profile" {
  description = "AWS CLI profile name"
  type        = string
  default     = "openclaw"
}

variable "project_name" {
  description = "Project name used for resource naming"
  type        = string
  default     = "openclaw"
}

variable "config_check_interval_minutes" {
  description = "Interval in minutes for config integrity hash checks"
  type        = number
  default     = 5
}

variable "alert_email" {
  description = "Email address for alert notifications (empty = no subscriber)"
  type        = string
  default     = ""
}

variable "setup_manifest" {
  description = "Describes the config values and shared secrets required for the deployment. The CLI reads this manifest during `cruxclaw admin setup` and prompts for missing values."
  type = object({
    config   = map(string)
    defaults = optional(map(string), {})
    secrets  = map(string)
  })
  default = {
    config = {
      "openclaw-image" = "Docker image for OpenClaw (ECR, GHCR, or Docker Hub)"
    }
    defaults = {
      "openclaw-image" = "ghcr.io/openclaw/openclaw:2026.3.11"
    }
    secrets = {
      "openclaw/shared/slack-bot-token"      = "Slack bot token (xoxb-)"
      "openclaw/shared/slack-app-token"      = "Slack app token (xapp-)"
      "openclaw/shared/slack-signing-secret" = "Slack signing secret"
      "openclaw/shared/google-client-id"     = "Google OAuth client ID"
      "openclaw/shared/google-client-secret" = "Google OAuth client secret"
    }
  }
}

# Agents are managed entirely via the CLI (cruxclaw admin add-user / add-team).
# Agent config lives in SSM Parameter Store at /openclaw/agents/<name>.
# The bootstrap script discovers agents from SSM at boot time.
