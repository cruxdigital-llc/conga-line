variable "ssh_host" {
  type        = string
  description = "Remote host IP or hostname."
}

variable "ssh_user" {
  type        = string
  description = "SSH user on the remote host."
  default     = "root"
}

variable "ssh_key_path" {
  type        = string
  description = "Path to SSH private key."
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
