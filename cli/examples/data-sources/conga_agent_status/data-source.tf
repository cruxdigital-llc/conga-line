data "conga_agent_status" "aaron" {
  name = "aaron"
}

output "aaron_state" {
  value = data.conga_agent_status.aaron.container_state
}
