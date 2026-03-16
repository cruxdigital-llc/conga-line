# Requirements: Terraform Foundation

## Goal
Set up Terraform S3 backend with state locking so all subsequent epics have a reliable, shared state foundation.

## Success Criteria
1. `terraform init` successfully configures the S3 backend
2. State file is stored in a versioned, encrypted S3 bucket
3. DynamoDB table provides state locking (prevents concurrent applies)
4. A bootstrap script/config exists to create the state bucket itself (chicken-and-egg problem)
5. Works with the `167595588574_AdministratorAccess` AWS profile
