# --- AMI Lookup ---

data "aws_ssm_parameter" "al2023_arm64" {
  name = "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-arm64"
}

# --- Launch Template ---

resource "aws_launch_template" "openclaw" {
  name_prefix   = "${var.project_name}-"
  image_id      = data.aws_ssm_parameter.al2023_arm64.value
  instance_type = "r6g.medium"

  iam_instance_profile {
    name = aws_iam_instance_profile.openclaw_host.name
  }

  vpc_security_group_ids = [aws_security_group.openclaw_host.id]

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

resource "aws_instance" "openclaw" {
  subnet_id = aws_subnet.private.id

  launch_template {
    id      = aws_launch_template.openclaw.id
    version = "$Latest"
  }

  tags = {
    Name = "${var.project_name}-host"
  }
}

resource "aws_volume_attachment" "data" {
  device_name = "/dev/xvdf"
  volume_id   = aws_ebs_volume.data.id
  instance_id = aws_instance.openclaw.id
}
