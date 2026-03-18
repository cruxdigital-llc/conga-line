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
    condition     = length(var.openclaw_image) > 0
    error_message = "openclaw_image must be set. See terraform.tfvars.example for details."
  }
}

variable "users" {
  description = "Map of user IDs to their config. Admin adds entries, users self-serve secrets."
  type = map(object({
    slack_channel = string
    gateway_port  = number
    iam_identity  = optional(string, "")
  }))
  default = {}

  validation {
    condition = alltrue([
      for uid, cfg in var.users : cfg.gateway_port >= 18789 && cfg.gateway_port <= 18889
    ])
    error_message = "All gateway_port values must be in range 18789-18889."
  }

  validation {
    condition     = length(values(var.users)[*].gateway_port) == length(distinct(values(var.users)[*].gateway_port))
    error_message = "All gateway_port values must be unique."
  }
}
