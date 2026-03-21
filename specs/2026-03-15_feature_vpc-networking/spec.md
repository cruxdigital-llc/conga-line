# Spec: VPC + Networking

## Overview
Create the shared VPC, subnets, fck-nat, security group, NACLs, and VPC Flow Logs in us-east-2. Single AZ. All resources tagged with `project_name`.

## Deliverables

### 1. `terraform/vpc.tf`

```hcl
data "aws_availability_zones" "available" {
  state = "available"
}

locals {
  az = data.aws_availability_zones.available.names[0]
}

resource "aws_vpc" "main" {
  cidr_block           = "10.0.0.0/24"
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = {
    Name = "${var.project_name}-vpc"
  }
}

resource "aws_internet_gateway" "main" {
  vpc_id = aws_vpc.main.id

  tags = {
    Name = "${var.project_name}-igw"
  }
}

# Public subnet — hosts the fck-nat instance
resource "aws_subnet" "public" {
  vpc_id                  = aws_vpc.main.id
  cidr_block              = "10.0.0.0/28"
  availability_zone       = local.az
  map_public_ip_on_launch = false

  tags = {
    Name = "${var.project_name}-public"
  }
}

# Private subnet — hosts the Conga Line EC2 instance
resource "aws_subnet" "private" {
  vpc_id            = aws_vpc.main.id
  cidr_block        = "10.0.0.16/28"
  availability_zone = local.az

  tags = {
    Name = "${var.project_name}-private"
  }
}

# Public route table — routes internet traffic through IGW
resource "aws_route_table" "public" {
  vpc_id = aws_vpc.main.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.main.id
  }

  tags = {
    Name = "${var.project_name}-public-rt"
  }
}

resource "aws_route_table_association" "public" {
  subnet_id      = aws_subnet.public.id
  route_table_id = aws_route_table.public.id
}

# Private route table — default route added by fck-nat module
resource "aws_route_table" "private" {
  vpc_id = aws_vpc.main.id

  tags = {
    Name = "${var.project_name}-private-rt"
  }
}

resource "aws_route_table_association" "private" {
  subnet_id      = aws_subnet.private.id
  route_table_id = aws_route_table.private.id
}
```

### 2. `terraform/nat.tf`

```hcl
module "fck_nat" {
  source  = "RaJiska/fck-nat/aws"
  version = "1.4.0"

  name          = "${var.project_name}-nat"
  vpc_id        = aws_vpc.main.id
  subnet_id     = aws_subnet.public.id
  instance_type = "t4g.nano"

  update_route_tables = true
  route_tables_ids = {
    "private" = aws_route_table.private.id
  }

  tags = {
    Name = "${var.project_name}-nat"
  }
}
```

### 3. `terraform/security.tf`

```hcl
# --- Security Group: Conga Line Host (zero ingress) ---

resource "aws_security_group" "conga_host" {
  name_prefix = "${var.project_name}-host-"
  description = "Conga Line host - zero ingress, HTTPS + DNS egress only"
  vpc_id      = aws_vpc.main.id

  tags = {
    Name = "${var.project_name}-host-sg"
  }

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_vpc_security_group_egress_rule" "https" {
  security_group_id = aws_security_group.conga_host.id
  description       = "HTTPS egress (Slack WSS, LLM APIs, Docker Hub, SSM)"
  ip_protocol       = "tcp"
  from_port         = 443
  to_port           = 443
  cidr_ipv4         = "0.0.0.0/0"
}

resource "aws_vpc_security_group_egress_rule" "dns_tcp" {
  security_group_id = aws_security_group.conga_host.id
  description       = "DNS TCP (VPC resolver)"
  ip_protocol       = "tcp"
  from_port         = 53
  to_port           = 53
  cidr_ipv4         = aws_vpc.main.cidr_block
}

resource "aws_vpc_security_group_egress_rule" "dns_udp" {
  security_group_id = aws_security_group.conga_host.id
  description       = "DNS UDP (VPC resolver)"
  ip_protocol       = "udp"
  from_port         = 53
  to_port           = 53
  cidr_ipv4         = aws_vpc.main.cidr_block
}

# --- NACLs: Private Subnet (defense-in-depth) ---

resource "aws_network_acl" "private" {
  vpc_id     = aws_vpc.main.id
  subnet_ids = [aws_subnet.private.id]

  tags = {
    Name = "${var.project_name}-private-nacl"
  }
}

# Outbound: allow HTTPS
resource "aws_network_acl_rule" "private_egress_https" {
  network_acl_id = aws_network_acl.private.id
  rule_number    = 100
  egress         = true
  protocol       = "tcp"
  rule_action    = "allow"
  cidr_block     = "0.0.0.0/0"
  from_port      = 443
  to_port        = 443
}

# Outbound: allow DNS TCP to VPC
resource "aws_network_acl_rule" "private_egress_dns_tcp" {
  network_acl_id = aws_network_acl.private.id
  rule_number    = 110
  egress         = true
  protocol       = "tcp"
  rule_action    = "allow"
  cidr_block     = aws_vpc.main.cidr_block
  from_port      = 53
  to_port        = 53
}

# Outbound: allow DNS UDP to VPC
resource "aws_network_acl_rule" "private_egress_dns_udp" {
  network_acl_id = aws_network_acl.private.id
  rule_number    = 120
  egress         = true
  protocol       = "udp"
  rule_action    = "allow"
  cidr_block     = aws_vpc.main.cidr_block
  from_port      = 53
  to_port        = 53
}

# Inbound: allow ephemeral return traffic (TCP responses to outbound HTTPS)
resource "aws_network_acl_rule" "private_ingress_ephemeral" {
  network_acl_id = aws_network_acl.private.id
  rule_number    = 100
  egress         = false
  protocol       = "tcp"
  rule_action    = "allow"
  cidr_block     = "0.0.0.0/0"
  from_port      = 1024
  to_port        = 65535
}

# Inbound: allow DNS UDP responses from VPC
resource "aws_network_acl_rule" "private_ingress_dns_udp" {
  network_acl_id = aws_network_acl.private.id
  rule_number    = 110
  egress         = false
  protocol       = "udp"
  rule_action    = "allow"
  cidr_block     = aws_vpc.main.cidr_block
  from_port      = 53
  to_port        = 53
}
```

Note: Using the newer `aws_vpc_security_group_egress_rule` resources instead of inline `egress` blocks. This is the recommended pattern for AWS provider v5+ — avoids conflicts and is more explicit.

### 4. `terraform/flow-logs.tf`

```hcl
resource "aws_cloudwatch_log_group" "vpc_flow_logs" {
  name              = "/${var.project_name}/vpc-flow-logs"
  retention_in_days = 30

  tags = {
    Name = "${var.project_name}-vpc-flow-logs"
  }
}

resource "aws_iam_role" "vpc_flow_logs" {
  name_prefix = "${var.project_name}-flow-logs-"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = {
        Service = "vpc-flow-logs.amazonaws.com"
      }
    }]
  })

  tags = {
    Name = "${var.project_name}-flow-logs-role"
  }
}

resource "aws_iam_role_policy" "vpc_flow_logs" {
  name_prefix = "${var.project_name}-flow-logs-"
  role        = aws_iam_role.vpc_flow_logs.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = [
        "logs:CreateLogGroup",
        "logs:CreateLogStream",
        "logs:PutLogEvents",
        "logs:DescribeLogGroups",
        "logs:DescribeLogStreams"
      ]
      Effect   = "Allow"
      Resource = "${aws_cloudwatch_log_group.vpc_flow_logs.arn}:*"
    }]
  })
}

resource "aws_flow_log" "main" {
  vpc_id                   = aws_vpc.main.id
  traffic_type             = "ALL"
  log_destination_type     = "cloud-watch-logs"
  log_destination          = aws_cloudwatch_log_group.vpc_flow_logs.arn
  iam_role_arn             = aws_iam_role.vpc_flow_logs.arn
  max_aggregation_interval = 600

  tags = {
    Name = "${var.project_name}-vpc-flow-log"
  }
}
```

### 5. New outputs in `terraform/outputs.tf`

Append to existing outputs:
```hcl
output "vpc_id" {
  description = "VPC ID"
  value       = aws_vpc.main.id
}

output "private_subnet_id" {
  description = "Private subnet ID for Conga Line host"
  value       = aws_subnet.private.id
}

output "conga_host_sg_id" {
  description = "Security group ID for Conga Line host"
  value       = aws_security_group.conga_host.id
}

output "nat_public_ip" {
  description = "fck-nat public IP (EIP)"
  value       = module.fck_nat.eip_public_ip
}
```

Note: The fck-nat module output name for the EIP may vary — verify against module docs during implementation.

## Edge Cases

| Scenario | Handling |
|---|---|
| No AZs available in us-east-2 | Extremely unlikely; `data.aws_availability_zones` would return empty, Terraform would fail with clear error |
| fck-nat AMI not found in us-east-2 | Module handles AMI discovery; if missing, Terraform plan fails before any resources are created |
| fck-nat instance stops/terminates | NAT is down → no egress → no SSM access. Must restart via console or separate IAM user. Documented as accepted risk for MVP |
| NACL rule conflicts | Using explicit rule numbers (100, 110, 120) with gaps for future insertions |

## Validation Steps

1. `terraform plan` — should show ~15-20 resources to create (VPC, subnets, IGW, route tables, SG, NACLs, flow logs, fck-nat module resources)
2. `terraform apply` — creates all resources
3. Verify via AWS CLI:
   - VPC exists with correct CIDR
   - Security group has zero ingress rules, 3 egress rules
   - NACL has correct inbound/outbound rules
   - Flow logs are active
   - fck-nat instance is running with EIP
4. Verify private subnet routing: `aws ec2 describe-route-tables` shows 0.0.0.0/0 → fck-nat instance
