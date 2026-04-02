data "conga_channels" "all" {}

output "slack_running" {
  value = [for ch in data.conga_channels.all.channels : ch.router_running if ch.platform == "slack"]
}
