# Plan: Config Integrity + Monitoring

## Overview
Install CloudWatch agent for log shipping, create a config hash-check script + systemd timer, set up CloudWatch metric filter + alarm + SNS topic for alerting. All intervals and recipients configurable.

## File Structure

New/modified files in `terraform/`:
```
terraform/
├── ...existing files...
├── monitoring.tf              # CloudWatch alarm, metric filter, SNS topic
├── variables.tf               # New variables for check interval, alert email
└── user-data.sh.tftpl         # Add CloudWatch agent install + config hash-check setup
```

## Step 1: Terraform Variables

Add to `variables.tf`:
- `config_check_interval_minutes` (default: 5)
- `alert_email` (default: "" — empty means no subscriber)

## Step 2: User-Data Additions

Add to the bootstrap script:

### 2a. Install CloudWatch Agent
- Install `amazon-cloudwatch-agent` from AL2023 repos
- Generate agent config that tails Docker container logs (JSON log driver writes to `/var/lib/docker/containers/`)
- Actually simpler: configure Docker to use `json-file` driver (default), then have CloudWatch agent tail the container log file
- Or: use `docker logs` output redirected to a known log file, then tail that
- **Simplest approach**: The systemd unit already sends container stdout/stderr to the journal. Configure CloudWatch agent to read from the journal for the `conga-myagent` unit.

### 2b. Config Hash-Check Script
Create `/opt/conga/scripts/check-config-integrity.sh`:
```bash
#!/bin/bash
EXPECTED_HASH=$(cat /opt/conga/config/openclaw.json.sha256)
CURRENT_HASH=$(sha256sum /opt/conga/data/myagent/openclaw.json | cut -d' ' -f1)
if [ "$EXPECTED_HASH" != "$CURRENT_HASH" ]; then
  echo "CONFIG_INTEGRITY_VIOLATION user=myagent expected=$EXPECTED_HASH actual=$CURRENT_HASH"
  logger -t conga-integrity "CONFIG_INTEGRITY_VIOLATION user=myagent"
fi
```

At bootstrap time, compute and store the known-good hash:
```bash
sha256sum /opt/conga/config/openclaw.json | cut -d' ' -f1 > /opt/conga/config/openclaw.json.sha256
chmod 0444 /opt/conga/config/openclaw.json.sha256
```

### 2c. Systemd Timer
Create a systemd timer + service pair:
- `conga-config-check.service` — runs the hash-check script
- `conga-config-check.timer` — fires every N minutes (from Terraform variable)
- Output goes to journal → CloudWatch agent picks it up

## Step 3: CloudWatch Agent Config

Agent config file at `/opt/aws/amazon-cloudwatch-agent/etc/amazon-cloudwatch-agent.json`:
```json
{
  "logs": {
    "logs_collected": {
      "journald": {
        "unit": ["conga-myagent", "conga-config-check"],
        "log_group_name": "/conga/gateway",
        "log_stream_name": "{instance_id}/journal"
      }
    }
  }
}
```

This ships both the Conga Line container logs and config integrity check results to CloudWatch.

## Step 4: Monitoring Resources (`monitoring.tf`)

### SNS Topic
```hcl
resource "aws_sns_topic" "conga_alerts" {
  name = "${var.project_name}-alerts"
}

resource "aws_sns_topic_subscription" "email" {
  count     = var.alert_email != "" ? 1 : 0
  topic_arn = aws_sns_topic.conga_alerts.arn
  protocol  = "email"
  endpoint  = var.alert_email
}
```

### CloudWatch Metric Filter
Filter the gateway log group for `CONFIG_INTEGRITY_VIOLATION`:
```hcl
resource "aws_cloudwatch_log_metric_filter" "config_violation" {
  name           = "${var.project_name}-config-integrity-violation"
  pattern        = "CONFIG_INTEGRITY_VIOLATION"
  log_group_name = aws_cloudwatch_log_group.gateway.name

  metric_transformation {
    name      = "ConfigIntegrityViolation"
    namespace = "Conga Line"
    value     = "1"
  }
}
```

### CloudWatch Alarm
```hcl
resource "aws_cloudwatch_alarm" "config_violation" {
  alarm_name          = "${var.project_name}-config-integrity-violation"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "ConfigIntegrityViolation"
  namespace           = "Conga Line"
  period              = 300
  statistic           = "Sum"
  threshold           = 0
  alarm_actions       = [aws_sns_topic.conga_alerts.arn]
  alarm_description   = "Conga Line config file integrity violation detected"
}
```

## Step 5: IAM Update

The instance role already has CloudWatch Logs permissions from Epic 2. Need to add:
- `cloudwatch:PutMetricData` for the custom metric (actually, the metric filter creates the metric from logs — no additional IAM needed)
- CloudWatch agent needs `logs:PutLogEvents`, `logs:CreateLogStream` — already granted

## Architect Review

- **Journal-based log shipping**: CloudWatch agent reads from systemd journal, which captures both container stdout/stderr and the config check output. No need to configure Docker log drivers or tail files. Clean and compatible with rootless migration.
- **Hash baseline stored separately**: The known-good hash is at `/opt/conga/config/openclaw.json.sha256` (root-owned, 0444). The config itself is in the data dir (writable by the container). If the container modifies the config, the hash won't match.
- **Metric filter approach**: No custom CloudWatch agent metrics needed. The log metric filter parses log lines for `CONFIG_INTEGRITY_VIOLATION` and creates a metric. The alarm watches that metric. Simple, no additional IAM.
- **SNS with no subscribers**: Topic exists but no one gets notified until `alert_email` is set. Zero cost when empty.
- **Timer interval as variable**: Terraform passes the interval to user-data, which sets the systemd timer `OnCalendar` accordingly.
