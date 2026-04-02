data "conga_policy" "current" {}

output "egress_mode" {
  value = data.conga_policy.current.egress_mode
}
