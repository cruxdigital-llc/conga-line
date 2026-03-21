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
