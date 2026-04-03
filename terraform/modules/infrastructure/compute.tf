# --- AMI Lookup ---

data "aws_ssm_parameter" "al2023_arm64" {
  name = "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-arm64"
}

# --- Launch Template ---

resource "aws_launch_template" "conga" {
  name_prefix   = "${var.project_name}-"
  image_id      = data.aws_ssm_parameter.al2023_arm64.value
  instance_type = var.instance_type

  iam_instance_profile {
    name = aws_iam_instance_profile.conga_host.name
  }

  vpc_security_group_ids = [aws_security_group.conga_host.id]

  block_device_mappings {
    device_name = "/dev/xvda"
    ebs {
      volume_size           = 20
      volume_type           = "gp3"
      encrypted             = true
      kms_key_id            = aws_kms_key.ebs.arn
      delete_on_termination = true
    }
  }

  metadata_options {
    http_endpoint               = "enabled"
    http_tokens                 = "required"
    http_put_response_hop_limit = 1
  }

  user_data = base64encode(templatefile("${path.module}/user-data-shim.sh.tftpl", {
    aws_region   = var.aws_region
    state_bucket = local.state_bucket
  }))

  tag_specifications {
    resource_type = "instance"
    tags = {
      Name = "${var.project_name}-host"
    }
  }

  tag_specifications {
    resource_type = "volume"
    tags = {
      Name = "${var.project_name}-host-ebs"
    }
  }

  tags = {
    Name = "${var.project_name}-launch-template"
  }
}

# --- Persistent Data Volume ---

resource "aws_ebs_volume" "data" {
  availability_zone = local.az
  size              = 20
  type              = "gp3"
  encrypted         = true
  kms_key_id        = aws_kms_key.ebs.arn

  tags = {
    Name = "${var.project_name}-data"
  }

  lifecycle {
    prevent_destroy = true
  }
}

# --- EC2 Instance ---

resource "aws_instance" "conga" {
  subnet_id = aws_subnet.private.id

  launch_template {
    id      = aws_launch_template.conga.id
    version = "$Latest"
  }

  # Ensure bootstrap script and router source are in S3 before the instance
  # boots — the user-data shim downloads them at first boot.
  depends_on = [
    aws_s3_object.bootstrap_script,
    aws_s3_object.router_package_json,
    aws_s3_object.router_index_js,
  ]

  tags = {
    Name = "${var.project_name}-host"
  }
}

resource "aws_volume_attachment" "data" {
  device_name = "/dev/xvdf"
  volume_id   = aws_ebs_volume.data.id
  instance_id = aws_instance.conga.id
}

# --- Bootstrap Readiness Gate ---
# Blocks until the bootstrap script finishes on the EC2 instance.
# The congaline module depends_on this module, so agent configuration
# won't start until the host is fully ready.

resource "terraform_data" "bootstrap_ready" {
  depends_on = [aws_instance.conga, aws_volume_attachment.data]

  triggers_replace = aws_instance.conga.id

  provisioner "local-exec" {
    interpreter = ["bash", "-c"]
    command     = <<-EOT
      echo "Waiting for bootstrap to complete on ${aws_instance.conga.id}..."
      for i in $(seq 1 60); do
        RESULT=$(aws ssm send-command \
          --instance-ids "${aws_instance.conga.id}" \
          --document-name "AWS-RunShellScript" \
          --parameters '{"commands":["test -f /opt/conga/.bootstrap-complete && echo READY || echo WAITING"]}' \
          --region "${var.aws_region}" \
          --profile "${var.aws_profile}" \
          --output text --query "Command.CommandId" 2>/dev/null) || { sleep 15; continue; }
        sleep 5
        OUTPUT=$(aws ssm get-command-invocation \
          --command-id "$RESULT" \
          --instance-id "${aws_instance.conga.id}" \
          --region "${var.aws_region}" \
          --profile "${var.aws_profile}" \
          --output text --query "StandardOutputContent" 2>/dev/null) || { sleep 10; continue; }
        if echo "$OUTPUT" | grep -q "READY"; then
          echo "Bootstrap complete."
          exit 0
        fi
        echo "Attempt $i/60: bootstrap still running..."
        sleep 15
      done
      echo "ERROR: Bootstrap did not complete within 15 minutes."
      exit 1
    EOT
  }
}
