variable "aws_region" {
  description = "AWS region for all resources"
  type        = string
  default     = "us-east-2"
}

variable "aws_profile" {
  description = "AWS CLI profile name"
  type        = string
  default     = "conga-line"
}

variable "project_name" {
  description = "Project name used for resource naming"
  type        = string
  default     = "conga-line"
}

variable "instance_type" {
  description = "EC2 instance type for the Conga Line host. Size at ~2GB per agent (e.g. r6g.medium for 3 agents)"
  type        = string
  default     = "r6g.medium"
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
  description = "Describes the config values and shared secrets required for the deployment. The CLI reads this manifest during `conga admin setup` and prompts for missing values."
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

# Agents are managed entirely via the CLI (conga admin add-user / add-team).
# Agent config lives in SSM Parameter Store at /conga/agents/<name>.
# The bootstrap script discovers agents from SSM at boot time.
