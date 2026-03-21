# --- SNS Topic ---

resource "aws_sns_topic" "alerts" {
  name = "${var.project_name}-alerts"

  tags = {
    Name = "${var.project_name}-alerts"
  }
}

resource "aws_sns_topic_subscription" "email" {
  count     = var.alert_email != "" ? 1 : 0
  topic_arn = aws_sns_topic.alerts.arn
  protocol  = "email"
  endpoint  = var.alert_email
}

# --- CloudWatch Metric Filter ---

resource "aws_cloudwatch_log_metric_filter" "config_violation" {
  name           = "${var.project_name}-config-integrity-violation"
  pattern        = "CONFIG_INTEGRITY_VIOLATION"
  log_group_name = aws_cloudwatch_log_group.gateway.name

  metric_transformation {
    name          = "ConfigIntegrityViolation"
    namespace     = "CongaLine"
    value         = "1"
    default_value = "0"
  }
}

# --- CloudWatch Alarm ---

resource "aws_cloudwatch_metric_alarm" "config_violation" {
  alarm_name          = "${var.project_name}-config-integrity-violation"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "ConfigIntegrityViolation"
  namespace           = "CongaLine"
  period              = 300
  statistic           = "Sum"
  threshold           = 0
  treat_missing_data  = "notBreaching"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]
  alarm_description   = "Conga Line config file integrity violation detected"

  tags = {
    Name = "${var.project_name}-config-integrity-alarm"
  }
}

# CloudWatch dashboard is created by the bootstrap script at boot time,
# using the agents discovered from SSM. This ensures it always reflects
# the actual agent list without requiring Terraform changes.
