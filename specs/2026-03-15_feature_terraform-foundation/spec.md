# Spec: Terraform Foundation

## Overview
Create the Terraform project structure and bootstrap the S3 state backend with DynamoDB locking. This is Epic 0 — all subsequent infrastructure work depends on it.

## Deliverables

### 1. `terraform/bootstrap.sh`

Idempotent shell script that creates the state backend resources using AWS CLI.

**Inputs** (configurable at top of script):
```bash
AWS_PROFILE="123456789012_AdministratorAccess"
AWS_REGION="us-east-2"
STATE_BUCKET="conga-terraform-state-123456789012"
LOCK_TABLE="conga-terraform-locks"
```

**Actions**:

1. **Check prerequisites**: Verify `aws` CLI is installed and profile is configured
2. **Create S3 bucket** (skip if exists):
   ```bash
   aws s3api create-bucket \
     --bucket "$STATE_BUCKET" \
     --region "$AWS_REGION" \
     --create-bucket-configuration LocationConstraint="$AWS_REGION" \
     --profile "$AWS_PROFILE"
   ```
3. **Enable versioning**:
   ```bash
   aws s3api put-bucket-versioning \
     --bucket "$STATE_BUCKET" \
     --versioning-configuration Status=Enabled \
     --profile "$AWS_PROFILE"
   ```
4. **Enable default encryption** (AES256):
   ```bash
   aws s3api put-bucket-encryption \
     --bucket "$STATE_BUCKET" \
     --server-side-encryption-configuration \
       '{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"AES256"}}]}' \
     --profile "$AWS_PROFILE"
   ```
5. **Block all public access**:
   ```bash
   aws s3api put-public-access-block \
     --bucket "$STATE_BUCKET" \
     --public-access-block-configuration \
       BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true \
     --profile "$AWS_PROFILE"
   ```
6. **Create DynamoDB table** (skip if exists):
   ```bash
   aws dynamodb create-table \
     --table-name "$LOCK_TABLE" \
     --attribute-definitions AttributeName=LockID,AttributeType=S \
     --key-schema AttributeName=LockID,KeyType=HASH \
     --billing-mode PAY_PER_REQUEST \
     --region "$AWS_REGION" \
     --profile "$AWS_PROFILE"
   ```
7. **Verify**: Confirm bucket and table exist, print status summary

**Idempotency**: Each step checks if the resource exists before creating. Uses `2>/dev/null` + exit code checks or `aws ... head-bucket` / `describe-table` to detect existing resources. Never fails on "already exists."

**Output**: Human-readable summary of what was created vs. what already existed.

### 2. `terraform/backend.tf`

```hcl
terraform {
  backend "s3" {
    bucket         = "conga-terraform-state-123456789012"
    key            = "conga/terraform.tfstate"
    region         = "us-east-2"
    dynamodb_table = "conga-terraform-locks"
    encrypt        = true
    profile        = "123456789012_AdministratorAccess"
  }
}
```

Note: Backend blocks cannot use variables — all values are hardcoded. This is a Terraform limitation.

### 3. `terraform/providers.tf`

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

### 4. `terraform/variables.tf`

```hcl
variable "aws_region" {
  description = "AWS region for all resources"
  type        = string
  default     = "us-east-2"
}

variable "aws_profile" {
  description = "AWS CLI profile name"
  type        = string
  default     = "123456789012_AdministratorAccess"
}

variable "project_name" {
  description = "Project name used for resource naming"
  type        = string
  default     = "conga"
}
```

### 5. `terraform/outputs.tf`

```hcl
output "state_bucket" {
  description = "S3 bucket name for Terraform state"
  value       = "conga-terraform-state-123456789012"
}

output "lock_table" {
  description = "DynamoDB table name for state locking"
  value       = "conga-terraform-locks"
}
```

### 6. `terraform/terraform.tfvars.example`

```hcl
# AWS Configuration
aws_region  = "us-east-2"
aws_profile = "123456789012_AdministratorAccess"

# Project
project_name = "conga"
```

### 7. `terraform/.gitignore`

```
*.tfstate
*.tfstate.backup
*.tfvars
!terraform.tfvars.example
.terraform/
.terraform.lock.hcl
```

## Edge Cases

| Scenario | Handling |
|---|---|
| S3 bucket name already taken globally | Resolved: suffixed with account ID (123456789012) |
| AWS profile not configured | Script checks `aws sts get-caller-identity` first and exits with instructions |
| DynamoDB table in CREATING state | Script waits for table to become ACTIVE before proceeding |
| Re-running bootstrap after success | No-op for existing resources; safe to run multiple times |
| `terraform init` before running bootstrap | Fails with clear S3 error; README should document the ordering |

## Validation Steps

1. Run `./bootstrap.sh` — should create bucket and table (or confirm they exist)
2. Run `terraform init` in `terraform/` — should configure S3 backend
3. Run `terraform plan` — should show "No changes" (empty state, no resources defined yet)
4. Verify in AWS Console or CLI:
   - Bucket exists with versioning enabled, encryption enabled, public access blocked
   - DynamoDB table exists with `LockID` partition key, PAY_PER_REQUEST billing
