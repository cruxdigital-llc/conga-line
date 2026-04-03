terraform {
  required_version = ">= 1.5"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
    conga = {
      source  = "registry.terraform.io/cruxdigital-llc/conga"
      version = "0.1.4"
    }
  }

  # Uncomment and configure for remote state:
  # backend "s3" {
  #   bucket         = "conga-line-terraform-state-ACCOUNT_ID"
  #   key            = "conga-line/terraform.tfstate"
  #   region         = "us-east-2"
  #   dynamodb_table = "conga-line-terraform-locks"
  #   encrypt        = true
  #   profile        = "openclaw"
  # }
}

provider "aws" {
  region  = var.aws_region
  profile = var.aws_profile
}

provider "conga" {
  provider_type = "aws"
  region        = var.aws_region
  profile       = var.aws_profile
}

# Layer 1: AWS Infrastructure
module "infrastructure" {
  source = "../../modules/infrastructure"

  aws_region   = var.aws_region
  aws_profile  = var.aws_profile
  project_name = var.project_name
  repo_root    = var.repo_root

  instance_type                 = var.instance_type
  config_check_interval_minutes = var.config_check_interval_minutes
  alert_email                   = var.alert_email
}

# Layer 2: CongaLine Agent Lifecycle
module "congaline" {
  source     = "../../modules/congaline"
  depends_on = [module.infrastructure]

  image = var.image

  agents                 = var.agents
  global_secrets         = var.global_secrets
  channels               = var.channels
  egress_mode            = var.egress_mode
  egress_allowed_domains = var.egress_allowed_domains
}

# Outputs
output "ssm_connect_command" {
  value = module.infrastructure.ssm_connect_command
}

output "instance_id" {
  value = module.infrastructure.instance_id
}

output "agent_ports" {
  value = module.congaline.agent_ports
}

output "ecr_repository_url" {
  value = module.infrastructure.ecr_repository_url
}
