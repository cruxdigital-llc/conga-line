# Spec: Config Integrity + Monitoring

## Overview
Add CloudWatch agent for log shipping from systemd journal, config hash-check with systemd timer, CloudWatch metric filter + alarm + SNS topic. All configurable via Terraform variables.

## Deliverables

### 1. New Variables in `terraform/variables.tf`

```hcl
variable "config_check_interval_minutes" {
  description = "Interval in minutes for config integrity hash checks"
  type        = number
  default     = 5
}

variable "alert_email" {
  description = "Email address for alert notifications (empty = no subscriber)"
  type        = string
  default     = ""
}
```

### 2. `terraform/monitoring.tf`

```hcl
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
    namespace     = "Conga Line"
    value         = "1"
    default_value = "0"
  }
}

# --- CloudWatch Alarm ---

resource "aws_cloudwatch_alarm" "config_violation" {
  alarm_name          = "${var.project_name}-config-integrity-violation"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "ConfigIntegrityViolation"
  namespace           = "Conga Line"
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
```

### 3. User-Data Additions (append to `user-data.sh.tftpl`)

New template variable needed: `config_check_interval_minutes`

Add these sections between the existing section 8 (CREATE SYSTEMD SERVICE) and section 9 (START SERVICE):

```bash
# ============================================================
# 9. CONFIG INTEGRITY MONITORING
# ============================================================

# Store known-good config hash (root-owned, read-only)
sha256sum /opt/conga/config/openclaw.json | cut -d' ' -f1 > /opt/conga/config/openclaw.json.sha256
chmod 0444 /opt/conga/config/openclaw.json.sha256

# Create integrity check script
mkdir -p /opt/conga/scripts
cat > /opt/conga/scripts/check-config-integrity.sh << 'CHECKSCRIPT'
#!/bin/bash
EXPECTED_HASH=$(cat /opt/conga/config/openclaw.json.sha256)
CURRENT_HASH=$(sha256sum /opt/conga/data/myagent/openclaw.json | cut -d' ' -f1)
if [ "$EXPECTED_HASH" != "$CURRENT_HASH" ]; then
  echo "CONFIG_INTEGRITY_VIOLATION user=myagent expected=$EXPECTED_HASH actual=$CURRENT_HASH" | systemd-cat -t conga-integrity -p warning
else
  echo "Config integrity OK user=myagent hash=$CURRENT_HASH" | systemd-cat -t conga-integrity
fi
CHECKSCRIPT
chmod 0755 /opt/conga/scripts/check-config-integrity.sh

# Create systemd service for the check
cat > /etc/systemd/system/conga-config-check.service << 'CHECKSVC'
[Unit]
Description=Conga Line Config Integrity Check

[Service]
Type=oneshot
ExecStart=/opt/conga/scripts/check-config-integrity.sh
CHECKSVC

# Create systemd timer
cat > /etc/systemd/system/conga-config-check.timer << CHECKTIMER
[Unit]
Description=Conga Line Config Integrity Check Timer

[Timer]
OnBootSec=2min
OnUnitActiveSec=${config_check_interval_minutes}min
Persistent=true

[Install]
WantedBy=timers.target
CHECKTIMER

systemctl daemon-reload
systemctl enable --now conga-config-check.timer

echo "Config integrity monitoring enabled (every ${config_check_interval_minutes} minutes)"

# ============================================================
# 10. CLOUDWATCH AGENT
# ============================================================

# Install CloudWatch agent
dnf install -y amazon-cloudwatch-agent

# Configure agent to ship journal logs
cat > /opt/aws/amazon-cloudwatch-agent/etc/amazon-cloudwatch-agent.json << CWCONFIG
{
  "agent": {
    "run_as_user": "root"
  },
  "logs": {
    "logs_collected": {
      "journald": [
        {
          "log_group_name": "/${project_name}/gateway",
          "log_stream_name": "{instance_id}",
          "retention_in_days": 30,
          "filters": [
            { "_SYSTEMD_UNIT": "conga-*.service" },
            { "SYSLOG_IDENTIFIER": "conga-integrity" }
          ]
        }
      ]
    }
  }
}
CWCONFIG

# Start CloudWatch agent
systemctl enable --now amazon-cloudwatch-agent

echo "CloudWatch agent started, shipping journal logs"
```

### 4. Update `terraform/compute.tf` — templatefile call

Add the new variable to the templatefile parameters:

```hcl
  user_data = base64encode(templatefile("${path.module}/user-data.sh.tftpl", {
    aws_region                     = var.aws_region
    project_name                   = var.project_name
    user_id                        = "myagent"
    slack_channel                  = "CEXAMPLE01"
    config_check_interval_minutes  = var.config_check_interval_minutes
  }))
```

### 5. IAM Update in `terraform/iam.tf`

The existing CloudWatch Logs policy scopes to the gateway log group ARN. The CloudWatch agent also needs `logs:CreateLogGroup` in case the group doesn't exist yet (defensive), and `logs:PutRetentionPolicy`. Update the policy:

```hcl
resource "aws_iam_role_policy" "cloudwatch_logs" {
  name_prefix = "${var.project_name}-logs-"
  role        = aws_iam_role.conga_host.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "logs:CreateLogGroup",
          "logs:CreateLogStream",
          "logs:PutLogEvents",
          "logs:DescribeLogGroups",
          "logs:DescribeLogStreams",
          "logs:PutRetentionPolicy"
        ]
        Resource = [
          "${aws_cloudwatch_log_group.gateway.arn}",
          "${aws_cloudwatch_log_group.gateway.arn}:*"
        ]
      }
    ]
  })
}
```

### 6. Updated Outputs in `terraform/outputs.tf`

```hcl
output "sns_topic_arn" {
  description = "SNS topic ARN for alerts"
  value       = aws_sns_topic.alerts.arn
}

output "config_check_interval" {
  description = "Config integrity check interval in minutes"
  value       = var.config_check_interval_minutes
}
```

## Edge Cases

| Scenario | Handling |
|---|---|
| Config changes legitimately (redeployment) | New instance gets fresh hash at bootstrap; no false alarm |
| Conga Line hot-reload modifies config | Hash mismatch triggers alarm — this is the intended behavior (detects tampering AND hot-reload writes) |
| CloudWatch agent fails to start | Container still runs; logs just aren't shipped. Check agent status via SSM. |
| Timer fires before container is running | Check script runs but file exists from bootstrap; will report OK |
| SNS topic with no subscribers | Alarm state changes visible in CloudWatch console; no email sent |
| Instance replacement | Fresh bootstrap creates new hash baseline; alarm auto-resolves |

## Validation Steps

1. `terraform plan` — should show monitoring.tf resources (SNS topic, metric filter, alarm) + updated launch template
2. `terraform apply` — creates resources, replaces instance
3. Wait for bootstrap, then via SSM:
   - `systemctl status conga-config-check.timer` — timer active
   - `systemctl list-timers` — shows next run time
   - `/opt/conga/scripts/check-config-integrity.sh` — run manually, should report OK
   - `systemctl status amazon-cloudwatch-agent` — agent running
4. Check CloudWatch console:
   - Log group `/conga/gateway` has log stream with journal entries
   - Alarm `conga-config-integrity-violation` in OK state
5. Tamper test (optional):
   - Modify `/opt/conga/data/myagent/openclaw.json` slightly
   - Wait for timer to fire (or run check manually)
   - Verify `CONFIG_INTEGRITY_VIOLATION` appears in CloudWatch logs
   - Verify alarm transitions to ALARM state
