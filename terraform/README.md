# Terraform — AWS Infrastructure

This directory contains the Terraform configuration for deploying CongaLine on AWS. It provisions a hardened, zero-ingress VPC with an EC2 host for running agent containers.

## Directory Structure

```
terraform/
├── bootstrap.sh                    # One-time: creates S3 bucket + DynamoDB for state
├── environments/
│   └── production/
│       ├── main.tf                 # Root module — wires infrastructure + congaline modules
│       ├── variables.tf            # All input variables
│       ├── backend.tf.example      # Template for S3 backend config
│       └── terraform.tfvars.example # Template for deployment settings
└── modules/
    ├── infrastructure/             # AWS resources (VPC, EC2, IAM, KMS, NAT, etc.)
    └── congaline/                  # Agent lifecycle via the conga Terraform provider
```

The two module layers have distinct responsibilities:

| Module | Provider | What it manages |
|--------|----------|-----------------|
| `infrastructure` | `hashicorp/aws` | VPC, EC2, IAM roles, KMS, NAT, security groups, monitoring |
| `congaline` | `cruxdigital-llc/conga` | Agents, secrets, channels, bindings, egress policy |

## Prerequisites

- **AWS CLI v2** with a configured SSO profile
- **Terraform** >= 1.5
- **session-manager-plugin** for SSM access to the EC2 instance

## Setup

### 1. Bootstrap Terraform state

The S3 bucket and DynamoDB table that store Terraform state must exist before `terraform init`. This is a one-time step:

```bash
export AWS_PROFILE=your-profile
export AWS_REGION=us-east-2

cd terraform
./bootstrap.sh
```

This creates:
- S3 bucket `conga-line-terraform-state-<account_id>` (versioned, encrypted, no public access)
- DynamoDB table `conga-line-terraform-locks` (state locking)

### 2. Configure the environment

```bash
cd environments/production

# S3 backend — Terraform can't use variables in backend blocks, so this is a separate file
cp backend.tf.example backend.tf
# Edit backend.tf: fill in your account ID, region, and profile

# Deployment settings
cp terraform.tfvars.example terraform.tfvars
# Edit terraform.tfvars: set agents, secrets, Slack tokens, etc.
```

Both `backend.tf` and `terraform.tfvars` are gitignored.

### 3. Deploy

```bash
terraform init
terraform plan
terraform apply
```

### 4. Ongoing changes

To add agents, update secrets, change policy, or modify channel bindings — edit `terraform.tfvars` and re-apply:

```bash
terraform plan
terraform apply
```

When using Terraform to manage the environment, the CLI is only needed for operational tasks that Terraform doesn't manage:

```bash
conga status --agent myagent          # check container health
conga logs --agent myagent            # tail container logs
conga connect --agent myagent         # open SSM tunnel to web UI
```

## What goes where

| Concern | Managed by | Why |
|---------|-----------|-----|
| VPC, EC2, IAM, KMS | Terraform (`infrastructure` module) | Infrastructure lifecycle, drift detection |
| Agents, secrets, channels, policy | Terraform (`congaline` module) | Declarative fleet definition, single source of truth |
| Day-to-day ops (status, logs, connect) | CLI | Operational concerns, not infrastructure state |

The `congaline` module uses the [conga Terraform provider](https://registry.terraform.io/providers/cruxdigital-llc/conga), which calls the same provider interface as the CLI. If you're managing your environment through Terraform, all agent lifecycle changes should go through `terraform apply` — not the CLI.

## Secrets

Never put real secret values in `.tf` files or commit them to git. Use `terraform.tfvars` (gitignored) or pass them via environment variables:

```bash
terraform apply -var='global_secrets={"anthropic-api-key":"sk-ant-..."}'
```

On the host, secrets are stored in AWS Secrets Manager and injected into containers via env files (mode 0400) on encrypted EBS.
