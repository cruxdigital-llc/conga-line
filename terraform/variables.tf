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

variable "users" {
  description = "Map of user IDs to their config. Admin adds entries, users self-serve secrets."
  type = map(object({
    slack_channel = string
  }))
  default = {
    UEXAMPLE01 = {
      slack_channel = "CEXAMPLE01"
    }
    UEXAMPLE02 = {
      slack_channel = "CEXAMPLE02"
    }
  }
}
