# Requirements: VPC + Networking

## Goal
Create the shared VPC with networking infrastructure that supports a single EC2 host running multiple Docker containers, with zero-ingress security and cost-optimized NAT egress via fck-nat.

## Success Criteria
1. VPC exists with /24 CIDR and /28 subnets (1 public, 1 private)
2. fck-nat instance (t4g.nano) provides egress for the private subnet
3. Zero ingress — security group has no inbound rules
4. Egress limited to HTTPS (443) + DNS
5. NACLs provide defense-in-depth at the subnet level
6. VPC Flow Logs enabled to CloudWatch
7. Single AZ deployment
8. `terraform apply` creates all resources cleanly
