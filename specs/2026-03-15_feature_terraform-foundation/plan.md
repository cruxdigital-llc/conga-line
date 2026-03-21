# Plan: Terraform Foundation

## Overview
Bootstrap the Terraform state backend (S3 + DynamoDB) via a shell script, then set up the main Terraform project structure with the S3 backend configured.

## File Structure

```
terraform/
├── bootstrap.sh              # One-time script to create state bucket + lock table
├── backend.tf                 # S3 backend configuration
├── providers.tf               # AWS provider, region, profile
├── variables.tf               # Input variables
├── outputs.tf                 # Output values
└── terraform.tfvars.example   # Example variable values
```

## Step 1: Bootstrap Script (`bootstrap.sh`)

Shell script using AWS CLI (profile `123456789012_AdministratorAccess`) to:
1. Create S3 bucket `conga-terraform-state` in us-east-2
   - Enable versioning
   - Enable server-side encryption (AES256 default)
   - Block all public access
2. Create DynamoDB table `conga-terraform-locks` in us-east-2
   - Partition key: `LockID` (String)
   - On-demand billing (pay-per-request, essentially free at this scale)
3. Verify both resources exist

Script should be idempotent — safe to run multiple times.

## Step 2: Backend Configuration (`backend.tf`)

```hcl
terraform {
  backend "s3" {
    bucket         = "conga-terraform-state"
    key            = "conga/terraform.tfstate"
    region         = "us-east-2"
    dynamodb_table = "conga-terraform-locks"
    encrypt        = true
    profile        = "123456789012_AdministratorAccess"
  }
}
```

## Step 3: Provider Configuration (`providers.tf`)

```hcl
terraform {
  required_version = ">= 1.5"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region  = var.aws_region
  profile = var.aws_profile
}
```

## Step 4: Variables (`variables.tf`)

- `aws_region` (default: `us-east-2`)
- `aws_profile` (default: `123456789012_AdministratorAccess`)
- `project_name` (default: `conga`)

## Step 5: Validate

1. Run `bootstrap.sh`
2. Run `terraform init` — should configure S3 backend successfully
3. Run `terraform plan` — should show no changes (empty state, no resources yet)

## Architect Review

- **State bucket naming**: Using account-specific naming avoids global collision. S3 bucket names are globally unique — if `conga-terraform-state` is taken, we'll need to suffix with account ID.
- **State key path**: `conga/terraform.tfstate` leaves room for future state separation if needed.
- **No KMS for state bucket**: Using AES256 default encryption rather than a dedicated KMS key. State bucket contains infrastructure metadata, not application secrets. KMS can be added later if needed.
- **Profile in backend block**: Hardcoded because backend blocks don't support variables. This is standard Terraform behavior.
