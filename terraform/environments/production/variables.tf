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

variable "egress_ports" {
  description = "Egress ports for the host security group. Use cidr=\"vpc\" for VPC-scoped rules."
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
    condition = length(var.egress_ports) == length(distinct([
      for p in var.egress_ports : "${p.protocol}-${p.port}"
    ]))
    error_message = "Duplicate protocol-port combinations are not allowed in egress_ports."
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
    type                   = string
    gateway_port           = optional(number)
    egress_allowed_domains = optional(list(string))
    secrets                = optional(map(string), {})
  }))
}

variable "global_secrets" {
  description = "Secrets applied to every agent (e.g. anthropic-api-key)"
  type        = map(string)
  sensitive   = true
  default     = {}
}

variable "channels" {
  description = "Messaging channels. Map of platform => { secrets, bindings }."
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
  description = "Global egress allowed domains. Supports wildcards (e.g. *.slack.com)."
  type        = list(string)
  default     = ["api.anthropic.com", "*.slack.com", "*.slack-edge.com"]
}
