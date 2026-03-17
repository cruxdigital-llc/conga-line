#!/usr/bin/env bash
set -euo pipefail

# Configuration
AWS_PROFILE="openclaw"
AWS_REGION="us-east-2"
STATE_BUCKET="openclaw-terraform-state-123456789012"
LOCK_TABLE="openclaw-terraform-locks"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info()  { echo -e "${GREEN}[INFO]${NC} $1"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $1"; }
error() { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

# Check prerequisites
command -v aws >/dev/null 2>&1 || error "AWS CLI is not installed. Install it first: https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html"

info "Verifying AWS credentials for profile: $AWS_PROFILE"
aws sts get-caller-identity --profile "$AWS_PROFILE" --region "$AWS_REGION" >/dev/null 2>&1 \
  || error "AWS profile '$AWS_PROFILE' is not configured or credentials are expired."

ACCOUNT_ID=$(aws sts get-caller-identity --profile "$AWS_PROFILE" --query Account --output text)
info "Authenticated as account: $ACCOUNT_ID"

# Create S3 bucket
if aws s3api head-bucket --bucket "$STATE_BUCKET" --profile "$AWS_PROFILE" 2>/dev/null; then
  info "S3 bucket '$STATE_BUCKET' already exists — skipping creation"
else
  info "Creating S3 bucket: $STATE_BUCKET"
  aws s3api create-bucket \
    --bucket "$STATE_BUCKET" \
    --region "$AWS_REGION" \
    --create-bucket-configuration LocationConstraint="$AWS_REGION" \
    --profile "$AWS_PROFILE"
fi

# Enable versioning
info "Enabling versioning on $STATE_BUCKET"
aws s3api put-bucket-versioning \
  --bucket "$STATE_BUCKET" \
  --versioning-configuration Status=Enabled \
  --profile "$AWS_PROFILE"

# Enable default encryption
info "Enabling default encryption (AES256) on $STATE_BUCKET"
aws s3api put-bucket-encryption \
  --bucket "$STATE_BUCKET" \
  --server-side-encryption-configuration \
    '{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"AES256"}}]}' \
  --profile "$AWS_PROFILE"

# Block all public access
info "Blocking all public access on $STATE_BUCKET"
aws s3api put-public-access-block \
  --bucket "$STATE_BUCKET" \
  --public-access-block-configuration \
    BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true \
  --profile "$AWS_PROFILE"

# Create DynamoDB table
if aws dynamodb describe-table --table-name "$LOCK_TABLE" --region "$AWS_REGION" --profile "$AWS_PROFILE" >/dev/null 2>&1; then
  info "DynamoDB table '$LOCK_TABLE' already exists — skipping creation"
else
  info "Creating DynamoDB table: $LOCK_TABLE"
  aws dynamodb create-table \
    --table-name "$LOCK_TABLE" \
    --attribute-definitions AttributeName=LockID,AttributeType=S \
    --key-schema AttributeName=LockID,KeyType=HASH \
    --billing-mode PAY_PER_REQUEST \
    --region "$AWS_REGION" \
    --profile "$AWS_PROFILE"

  info "Waiting for table to become ACTIVE..."
  aws dynamodb wait table-exists \
    --table-name "$LOCK_TABLE" \
    --region "$AWS_REGION" \
    --profile "$AWS_PROFILE"
fi

# Verify
echo ""
info "=== Bootstrap Complete ==="
echo ""

BUCKET_VERSIONING=$(aws s3api get-bucket-versioning --bucket "$STATE_BUCKET" --profile "$AWS_PROFILE" --query Status --output text)
TABLE_STATUS=$(aws dynamodb describe-table --table-name "$LOCK_TABLE" --region "$AWS_REGION" --profile "$AWS_PROFILE" --query Table.TableStatus --output text)

info "S3 Bucket:        $STATE_BUCKET (versioning: $BUCKET_VERSIONING)"
info "DynamoDB Table:   $LOCK_TABLE (status: $TABLE_STATUS)"
info "Region:           $AWS_REGION"
echo ""
info "Next step: cd terraform && terraform init"
