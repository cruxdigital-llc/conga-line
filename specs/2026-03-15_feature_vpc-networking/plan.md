# Plan: VPC + Networking

## Overview
Create the shared VPC, public/private subnets, fck-nat instance (via community Terraform module), security group, NACLs, and VPC Flow Logs. All in a single AZ in us-east-2.

## File Structure

New files added to `terraform/`:
```
terraform/
├── ...existing files...
├── vpc.tf                # VPC, subnets, IGW, route tables
├── nat.tf                # fck-nat module invocation
├── security.tf           # Security group (zero ingress) + NACLs
└── flow-logs.tf          # VPC Flow Logs to CloudWatch
```

## Step 1: VPC + Subnets (`vpc.tf`)

- VPC: `10.0.0.0/24` (256 IPs, plenty for single-host + NAT)
- Public subnet: `10.0.0.0/28` (for fck-nat instance, needs IGW route)
- Private subnet: `10.0.0.16/28` (for Conga Line host)
- Internet Gateway attached to VPC
- Public route table: `0.0.0.0/0 → IGW`
- Private route table: `0.0.0.0/0 → fck-nat` (handled by the fck-nat module)
- Single AZ: use `data.aws_availability_zones` to pick the first available

## Step 2: fck-nat (`nat.tf`)

Use the `RaJiska/fck-nat/aws` module (v1.4.0):
```hcl
module "fck_nat" {
  source        = "RaJiska/fck-nat/aws"
  version       = "1.4.0"
  name          = "${var.project_name}-nat"
  vpc_id        = aws_vpc.main.id
  subnet_id     = aws_subnet.public.id
  instance_type = "t4g.nano"

  update_route_tables = true
  route_tables_ids = {
    "private" = aws_route_table.private.id
  }
}
```

The module handles:
- AMI discovery (arm64, fck-nat-al2023)
- EIP allocation
- IAM instance profile
- Security group for the NAT instance
- Route table updates (0.0.0.0/0 → NAT instance)

## Step 3: Security Group (`security.tf`)

**Conga Line host security group** (zero ingress):
```hcl
resource "aws_security_group" "conga_host" {
  name_prefix = "${var.project_name}-host-"
  vpc_id      = aws_vpc.main.id

  # ZERO INBOUND RULES

  # HTTPS egress (Slack WSS, LLM APIs, Docker Hub)
  egress {
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  # DNS egress (within VPC)
  egress {
    from_port   = 53
    to_port     = 53
    protocol    = "tcp"
    cidr_blocks = ["10.0.0.0/24"]
  }
  egress {
    from_port   = 53
    to_port     = 53
    protocol    = "udp"
    cidr_blocks = ["10.0.0.0/24"]
  }
}
```

**NACLs for private subnet** (defense-in-depth):
- Inbound: allow ephemeral return traffic (1024-65535) from 0.0.0.0/0 on TCP
- Outbound: allow 443 TCP to 0.0.0.0/0, allow 53 TCP/UDP to VPC CIDR
- Deny all else (implicit)

## Step 4: VPC Flow Logs (`flow-logs.tf`)

- CloudWatch log group: `/conga/vpc-flow-logs`
- IAM role for flow logs (allows `logs:CreateLogGroup`, `logs:CreateLogStream`, `logs:PutLogEvents`)
- VPC Flow Log resource: capture ALL traffic, send to CloudWatch
- Log retention: 30 days (cost-conscious)

## Architect Review

- **CIDR allocation**: /24 VPC with two /28 subnets uses 32 of 256 IPs. Leaves room for future subnets if needed (e.g., additional AZ, management subnet).
- **fck-nat module**: Well-maintained (374k+ downloads), handles the boilerplate. t4g.nano is the smallest viable instance — sufficient for HTTPS-only egress.
- **Security group DNS rule**: Scoped to VPC CIDR only. AWS VPC DNS resolver lives at VPC CIDR base + 2 (10.0.0.2), so this is correct.
- **NACLs**: Stateless, so both inbound ephemeral return and outbound 443 rules are needed. NACLs are belt-and-suspenders on top of the security group.
- **No VPC endpoints**: Per earlier cost analysis, we're using fck-nat for all egress instead of VPC endpoints. SSM access will need the host to reach SSM endpoints via NAT — this works but means SSM depends on the NAT instance being healthy.
