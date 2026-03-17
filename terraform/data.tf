data "aws_caller_identity" "current" {}

locals {
  state_bucket = "${var.project_name}-terraform-state-${data.aws_caller_identity.current.account_id}"
}
