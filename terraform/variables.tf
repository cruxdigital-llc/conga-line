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

variable "openclaw_image" {
  description = "Docker image for OpenClaw containers (ECR, GHCR, or Docker Hub). Required — upstream image needs PR #49514 fix."
  type        = string

  validation {
    condition     = length(trimspace(var.openclaw_image)) > 0
    error_message = "openclaw_image must be set. See terraform.tfvars.example for details."
  }
}

variable "shared_secrets" {
  description = "Map of shared secret paths to descriptions. Created in Secrets Manager with placeholder values."
  type        = map(string)
  default = {
    "openclaw/shared/slack-bot-token"      = "Slack bot token (xoxb-)"
    "openclaw/shared/slack-app-token"      = "Slack app token (xapp-)"
    "openclaw/shared/slack-signing-secret" = "Slack signing secret"
    "openclaw/shared/google-client-id"     = "Google OAuth client ID"
    "openclaw/shared/google-client-secret" = "Google OAuth client secret"
  }
}

variable "agents" {
  description = "Map of agent names to config. type=user for DM-only individual agents, type=team for channel-based team agents."
  type = map(object({
    type          = string
    member_id     = optional(string, "")
    slack_channel = optional(string, "")
    gateway_port  = number
    iam_identity  = optional(string, "")
  }))
  default = {}

  validation {
    condition = alltrue([
      for name, cfg in var.agents : contains(["user", "team"], cfg.type)
    ])
    error_message = "Agent type must be \"user\" or \"team\"."
  }

  validation {
    condition = alltrue([
      for name, cfg in var.agents : cfg.type != "user" || (cfg.member_id != "" && startswith(cfg.member_id, "U"))
    ])
    error_message = "Agents with type=\"user\" must have a member_id starting with 'U'."
  }

  validation {
    condition = alltrue([
      for name, cfg in var.agents : cfg.type != "user" || cfg.slack_channel == ""
    ])
    error_message = "Agents with type=\"user\" must not have a slack_channel (DM-only)."
  }

  validation {
    condition = alltrue([
      for name, cfg in var.agents : cfg.type != "team" || (cfg.slack_channel != "" && startswith(cfg.slack_channel, "C"))
    ])
    error_message = "Agents with type=\"team\" must have a slack_channel starting with 'C'."
  }

  validation {
    condition = alltrue([
      for name, cfg in var.agents : cfg.gateway_port >= 18789 && cfg.gateway_port <= 18889
    ])
    error_message = "All gateway_port values must be in range 18789-18889."
  }

  validation {
    condition     = length(values(var.agents)[*].gateway_port) == length(distinct(values(var.agents)[*].gateway_port))
    error_message = "All gateway_port values must be unique."
  }

  validation {
    condition     = length([for k, v in var.agents : v.member_id if v.type == "user"]) == length(distinct([for k, v in var.agents : v.member_id if v.type == "user"]))
    error_message = "All user agent member_id values must be unique."
  }
}

locals {
  user_agents = { for k, v in var.agents : k => v if v.type == "user" }
  team_agents = { for k, v in var.agents : k => v if v.type == "team" }
  # Container ID: member_id for user agents, agent name for team agents
  agent_container_id = { for k, v in var.agents : k => v.type == "user" ? v.member_id : k }
}
