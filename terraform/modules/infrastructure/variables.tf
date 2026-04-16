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
  description = "Describes the config values and shared secrets required for the deployment."
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

variable "repo_root" {
  description = "Absolute path to the congaline repository root. Used to locate behavior files, router source, and CLI scripts."
  type        = string
}

variable "egress_mode" {
  description = "Global egress enforcement mode. Seeded into S3 so fresh instances bootstrap with correct egress policy."
  type        = string
  default     = "enforce"

  validation {
    condition     = contains(["enforce", "validate"], var.egress_mode)
    error_message = "egress_mode must be \"enforce\" or \"validate\"."
  }
}

variable "egress_allowed_domains" {
  description = "Global egress allowed domains. Seeded into S3 so fresh instances bootstrap with correct egress policy."
  type        = list(string)
  default     = []
}

variable "egress_ports" {
  description = "Egress ports to open on the host security group and NACLs. Use cidr=\"vpc\" for VPC-scoped rules."
  type = list(object({
    protocol    = string
    port        = number
    cidr        = optional(string, "0.0.0.0/0")
    description = optional(string, "")
  }))
  default = [
    { protocol = "tcp", port = 443, description = "HTTPS (Slack WSS, LLM APIs, Docker Hub, SSM)" },
    { protocol = "tcp", port = 53, cidr = "vpc", description = "DNS TCP (VPC resolver)" },
    { protocol = "udp", port = 53, cidr = "vpc", description = "DNS UDP (VPC resolver)" },
  ]

  validation {
    condition     = alltrue([for p in var.egress_ports : contains(["tcp", "udp"], p.protocol)])
    error_message = "Each egress port protocol must be \"tcp\" or \"udp\"."
  }

  validation {
    condition     = alltrue([for p in var.egress_ports : p.port >= 1 && p.port <= 65535])
    error_message = "Each egress port must be between 1 and 65535."
  }

  validation {
    condition = length(var.egress_ports) == length(distinct([
      for p in var.egress_ports : "${p.protocol}-${p.port}"
    ]))
    error_message = "Duplicate protocol-port combinations are not allowed in egress_ports."
  }

  validation {
    condition     = length(var.egress_ports) <= 90
    error_message = "Maximum 90 egress port entries (NACL rule_number budget)."
  }
}
