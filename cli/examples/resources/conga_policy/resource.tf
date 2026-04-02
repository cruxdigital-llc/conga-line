resource "conga_policy" "this" {
  egress_mode            = "enforce"
  egress_allowed_domains = ["api.anthropic.com", "*.slack.com"]

  agent_override {
    name                   = "aaron"
    egress_allowed_domains = ["api.anthropic.com", "*.slack.com", "*.trello.com"]
  }
}
