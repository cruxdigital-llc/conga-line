resource "conga_secret" "api_key" {
  agent = "aaron"
  name  = "anthropic-api-key"
  value = var.anthropic_api_key
}
