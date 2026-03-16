terraform {
  backend "s3" {
    bucket         = "openclaw-terraform-state-123456789012"
    key            = "openclaw/terraform.tfstate"
    region         = "us-east-2"
    dynamodb_table = "openclaw-terraform-locks"
    encrypt        = true
    profile        = "123456789012_AdministratorAccess"
  }
}
