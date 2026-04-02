variable "aws_region" {
  type    = string
  default = "us-east-2"
}

variable "aws_profile" {
  type    = string
  default = "openclaw"
}

variable "anthropic_api_key" {
  type      = string
  sensitive = true
}
