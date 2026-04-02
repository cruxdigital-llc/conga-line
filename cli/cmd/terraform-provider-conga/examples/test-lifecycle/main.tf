terraform {
  required_providers {
    conga = {
      source = "registry.terraform.io/cruxdigital-llc/conga"
    }
  }
}

provider "conga" {
  provider_type = "local"
}

# Step 1: Environment
resource "conga_environment" "test" {
  image = "ghcr.io/openclaw/openclaw:2026.3.11"
}

# Step 2: Agent
resource "conga_agent" "tftest" {
  name       = "tftest"
  type       = "user"
  depends_on = [conga_environment.test]
}

# Step 3: Secret
resource "conga_secret" "tftest_api_key" {
  agent = conga_agent.tftest.name
  name  = "anthropic-api-key"
  value = "sk-ant-test-placeholder-for-terraform-validation"
}

# Step 4: Policy
resource "conga_policy" "test" {
  egress_mode            = "validate"
  egress_allowed_domains = ["api.anthropic.com", "*.slack.com"]
  depends_on             = [conga_environment.test]
}
