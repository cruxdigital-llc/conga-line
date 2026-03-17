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
    namespace     = "OpenClaw"
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
  namespace           = "OpenClaw"
  period              = 300
  statistic           = "Sum"
  threshold           = 0
  treat_missing_data  = "notBreaching"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]
  alarm_description   = "OpenClaw config file integrity violation detected"

  tags = {
    Name = "${var.project_name}-config-integrity-alarm"
  }
}

# --- CloudWatch Dashboard ---

resource "aws_cloudwatch_dashboard" "openclaw" {
  dashboard_name = var.project_name

  dashboard_body = jsonencode({
    widgets = [
      {
        type   = "metric"
        x      = 0
        y      = 0
        width  = 12
        height = 6
        properties = {
          title   = "Session Context Size (KB)"
          view    = "timeSeries"
          stacked = false
          region  = var.aws_region
          metrics = [
            for uid in keys(var.users) : [
              "OpenClaw", "SessionSizeKB", "UserId", uid,
              { label = uid, stat = "Maximum", period = 300 }
            ]
          ]
          yAxis = {
            left = { label = "KB", showUnits = false }
          }
        }
      },
      {
        type   = "metric"
        x      = 12
        y      = 0
        width  = 12
        height = 6
        properties = {
          title   = "Session Message Count"
          view    = "timeSeries"
          stacked = false
          region  = var.aws_region
          metrics = [
            for uid in keys(var.users) : [
              "OpenClaw", "SessionMessageCount", "UserId", uid,
              { label = uid, stat = "Maximum", period = 300 }
            ]
          ]
          yAxis = {
            left = { label = "Messages", showUnits = false }
          }
        }
      }
    ]
  })
}
