# Spec: Open-Source Sanitization

## Overview

Remove all hardcoded environment-specific values from the repository so it can be published as an early-preview open-source project. Any developer should be able to clone, configure, and deploy their own Conga Line instance.

## 1. Terraform Genericization

### 1.1 Gitignore and example files

**`.gitignore`** — append:
```
terraform/backend.tf
terraform/terraform.tfvars
```

After adding to `.gitignore`, run `git rm --cached terraform/backend.tf terraform/terraform.tfvars` if they are currently tracked, so they stop being committed while remaining on disk.

**`terraform/backend.tf.example`** — new file:
```hcl
# Copy to backend.tf and fill in your values.
# backend.tf is gitignored — Terraform does not support variables in backend blocks.
terraform {
  backend "s3" {
    bucket         = "<project_name>-terraform-state-<account_id>"
    key            = "<project_name>/terraform.tfstate"
    region         = "<aws_region>"
    dynamodb_table = "<project_name>-terraform-locks"
    encrypt        = true
    profile        = "<aws_profile>"
  }
}
```

**`terraform/terraform.tfvars.example`** — updated:
```hcl
# AWS Configuration
aws_region   = "us-east-2"
aws_profile  = "your-aws-profile"
project_name = "conga"

# Conga Line Docker image — REQUIRED
# The upstream ghcr.io/openclaw/openclaw:latest does not work with Slack
# without the fix from PR https://github.com/openclaw/openclaw/pull/49514.
# Build your own image with that fix applied and push to ECR or another registry.
# Example: conga_image = "123456789012.dkr.ecr.us-east-2.amazonaws.com/conga:latest"
conga_image = ""

# Monitoring
config_check_interval_minutes = 5
alert_email = ""  # Set to receive alert emails

# Users — add entries to onboard new users
# Keys are Slack Member IDs, found in Slack profile > "..." > "Copy member ID"
users = {
  # UXXXXXXXXXX = {
  #   slack_channel = "CXXXXXXXXXX"
  # }
}
```

### 1.2 New variable: `conga_image`

**`terraform/variables.tf`** — add:
```hcl
variable "conga_image" {
  description = "Docker image for Conga Line containers (ECR, GHCR, or Docker Hub). Required — upstream image needs PR #49514 fix."
  type        = string

  validation {
    condition     = length(var.conga_image) > 0
    error_message = "conga_image must be set. See terraform.tfvars.example for details."
  }
}
```

Also change `users` default from the real user map to `default = {}`.

### 1.3 Dynamic locals

**`terraform/data.tf`** — add `lock_table`:
```hcl
locals {
  state_bucket = "${var.project_name}-terraform-state-${data.aws_caller_identity.current.account_id}"
  lock_table   = "${var.project_name}-terraform-locks"
}
```

### 1.4 Outputs

**`terraform/outputs.tf`** — replace hardcoded strings:
```hcl
output "state_bucket" {
  description = "S3 bucket name for Terraform state"
  value       = local.state_bucket
}

output "lock_table" {
  description = "DynamoDB table name for state locking"
  value       = local.lock_table
}
```

### 1.5 Template variable plumbing

**`terraform/router.tf`** — add to the `templatefile` call:
```hcl
locals {
  bootstrap_content = templatefile("${path.module}/user-data.sh.tftpl", {
    aws_region                    = var.aws_region
    project_name                  = var.project_name
    users                         = var.users
    config_check_interval_minutes = var.config_check_interval_minutes
    conga_image                = var.conga_image
    state_bucket                  = local.state_bucket
    routing_json = jsonencode({
      channels = { for uid, cfg in var.users : cfg.slack_channel => "http://conga-${uid}:18789/slack/events" }
      members  = { for uid, cfg in var.users : uid => "http://conga-${uid}:18789/slack/events" }
    })
  })
}
```

**`terraform/compute.tf`** — add `state_bucket` to the shim templatefile:
```hcl
user_data = base64encode(templatefile("${path.module}/user-data-shim.sh.tftpl", {
  aws_region   = var.aws_region
  state_bucket = local.state_bucket
}))
```

### 1.6 Template files

**`terraform/user-data-shim.sh.tftpl`** — line 6:
```bash
# Before:
aws s3 cp s3://conga-terraform-state-123456789012/conga/bootstrap/bootstrap.sh /tmp/bootstrap.sh --region ${aws_region}

# After:
aws s3 cp s3://${state_bucket}/conga/bootstrap/bootstrap.sh /tmp/bootstrap.sh --region ${aws_region}
```

**`terraform/user-data.sh.tftpl`** — three changes:

1. **ECR image block** (lines 92-96) — replace with configurable image + auto-detect ECR:
```bash
CONGA_IMAGE="${conga_image}"

# Auto-login to ECR if image is from ECR
ECR_DOMAIN=$(echo "$CONGA_IMAGE" | cut -d'/' -f1)
if [[ "$ECR_DOMAIN" == *".dkr.ecr."* ]]; then
  aws ecr get-login-password --region "$AWS_REGION" | docker login --username AWS --password-stdin "$ECR_DOMAIN"
fi

docker pull "$CONGA_IMAGE"
docker pull node:22-alpine
```

2. **S3 router downloads** (lines 108-109):
```bash
# Before:
aws s3 cp s3://conga-terraform-state-123456789012/conga/router/package.json ...
aws s3 cp s3://conga-terraform-state-123456789012/conga/router/src/index.js ...

# After:
aws s3 cp s3://${state_bucket}/conga/router/package.json ...
aws s3 cp s3://${state_bucket}/conga/router/src/index.js ...
```

3. **Region fallback** (line 351):
```bash
# Before:
REGION=$${REGION:-us-east-2}

# After:
REGION=$${REGION:-${aws_region}}
```

### 1.7 Shell scripts

**`terraform/bootstrap.sh`** — replace hardcoded config block (lines 4-8):
```bash
# Configuration — override via environment variables
PROJECT_NAME="${1:-conga}"
AWS_PROFILE="${AWS_PROFILE:-conga}"
AWS_REGION="${AWS_REGION:-us-east-2}"

# ... (existing prerequisite checks) ...

info "Verifying AWS credentials for profile: $AWS_PROFILE"
aws sts get-caller-identity --profile "$AWS_PROFILE" --region "$AWS_REGION" >/dev/null 2>&1 \
  || error "AWS profile '$AWS_PROFILE' is not configured or credentials are expired."

ACCOUNT_ID=$(aws sts get-caller-identity --profile "$AWS_PROFILE" --query Account --output text)
info "Authenticated as account: $ACCOUNT_ID"

# Derive resource names (matches terraform/data.tf pattern)
STATE_BUCKET="${PROJECT_NAME}-terraform-state-${ACCOUNT_ID}"
LOCK_TABLE="${PROJECT_NAME}-terraform-locks"
```

Remove the duplicate `ACCOUNT_ID` fetch that was on the old line 27. Update the final message:
```bash
info "Next step: cp backend.tf.example backend.tf && edit values, then terraform init"
```

**`terraform/populate-secrets.sh`** — replace hardcoded lines 4-5:
```bash
AWS_PROFILE="${AWS_PROFILE:-conga}"
AWS_REGION="${AWS_REGION:-us-east-2}"
```

## 2. CLI Genericization

### 2.1 Interactive first-run setup (`conga init`)

New command: `conga init`. Triggered automatically when required config is missing. Prompts for:

1. **AWS Region** — `"AWS region for your Conga Line deployment"` (default: `us-east-2`)
2. **SSO Start URL** — `"AWS SSO start URL (e.g., https://your-org.awsapps.com/start/)"` (no default)
3. **SSO Account ID** — `"AWS account ID"` (no default)
4. **SSO Role Name** — `"SSO role/permission set name"` (default: `Conga LineUser`)
5. **Conga Line Image** — `"Docker image for Conga Line (e.g., 123456789012.dkr.ecr.us-east-2.amazonaws.com/conga:latest)"` (no default)

Writes `~/.conga/config.toml`:
```toml
region = "us-east-2"
sso_start_url = "https://your-org.awsapps.com/start/"
sso_account_id = "123456789012"
sso_role_name = "Conga LineUser"
instance_tag = "conga-host"
conga_image = "123456789012.dkr.ecr.us-east-2.amazonaws.com/conga:latest"
```

**Trigger logic**: In `PersistentPreRun`, after `config.Load()`, check if required fields are empty. If so, print "First-time setup required. Running `conga init`..." and invoke the setup flow. Skip for `conga init` itself, `conga --help`, and `conga auth login`.

### 2.2 Config struct changes

**`cli/pkg/config/config.go`**:

```go
type Config struct {
    Region        string `toml:"region"`
    SSOStartURL   string `toml:"sso_start_url"`
    SSOAccountID  string `toml:"sso_account_id"`
    SSORoleName   string `toml:"sso_role_name"`
    InstanceTag   string `toml:"instance_tag"`
    Conga LineImage string `toml:"conga_image"`
}

func Defaults() *Config {
    return &Config{
        InstanceTag: "conga-host",
    }
}
```

All other fields default to empty string. Env var overrides remain (add `CONGA_CONGA_IMAGE`).

Add helper:
```go
func (c *Config) RequiredFieldsMissing() []string {
    var missing []string
    if c.Region == "" {
        missing = append(missing, "region")
    }
    if c.SSOStartURL == "" {
        missing = append(missing, "sso_start_url")
    }
    if c.SSOAccountID == "" {
        missing = append(missing, "sso_account_id")
    }
    if c.Conga LineImage == "" {
        missing = append(missing, "conga_image")
    }
    return missing
}
```

### 2.3 New command file: `cli/cmd/init.go`

```go
var initCmd = &cobra.Command{
    Use:   "init",
    Short: "Configure Conga Line for first use",
    RunE:  runInit,
}
```

`runInit` function:
1. Load existing config (may be partial)
2. For each required field, if empty, prompt using `ui.TextPrompt` with a descriptive label and optional default
3. Write `~/.conga/config.toml` using `toml.Encode`
4. Print summary of what was written
5. Suggest `conga auth login` as the next step

### 2.4 Auto-trigger in PersistentPreRun

In `root.go` `PersistentPreRun`:
```go
cfg = config.Load()
if flagRegion != "" {
    cfg.Region = flagRegion
}

// Auto-trigger init if required config is missing
// Skip for: init, help, completion
cmdName := cmd.Name()
if cmdName != "init" && cmdName != "help" && cmdName != "completion" {
    if missing := cfg.RequiredFieldsMissing(); len(missing) > 0 {
        fmt.Printf("Missing configuration: %s\n", strings.Join(missing, ", "))
        fmt.Println("Running first-time setup...\n")
        if err := runInit(cmd, nil); err != nil {
            return
        }
        cfg = config.Load() // reload after init
    }
}
```

### 2.5 Configurable Docker image in templates

**`cli/scripts/add-user.sh.tmpl`** — line 113:
```bash
# Before:
... $ENV_FLAGS ghcr.io/openclaw/openclaw:latest

# After:
... $ENV_FLAGS {{.Conga LineImage}}
```

**`cli/scripts/refresh-user.sh.tmpl`** — line 43:
```bash
# Before:
... $ENV_FLAGS ghcr.io/openclaw/openclaw:latest

# After:
... $ENV_FLAGS {{.Conga LineImage}}
```

**`cli/cmd/admin.go`** — template execution struct (line 149):
```go
err = tmpl.Execute(&buf, struct {
    MemberID      string
    SlackChannel  string
    AWSRegion     string
    GatewayPort   int
    Conga LineImage string
}{
    MemberID:      memberID,
    SlackChannel:  slackChannel,
    AWSRegion:     cfg.Region,
    GatewayPort:   gatewayPort,
    Conga LineImage: cfg.Conga LineImage,
})
```

**`cli/cmd/refresh.go`** — template execution struct (line 45):
```go
err = tmpl.Execute(&buf, struct {
    MemberID      string
    AWSRegion     string
    Conga LineImage string
}{
    MemberID:      memberID,
    AWSRegion:     cfg.Region,
    Conga LineImage: cfg.Conga LineImage,
})
```

### 2.6 Error message scrub

**`cli/cmd/root.go`**:
- Line 69: `UEXAMPLE01` → `UXXXXXXXXXX`
- Line 76: `CEXAMPLE01` → `CXXXXXXXXXX`

**`cli/cmd/auth.go`**:
- Line 33: `"conga"` → use the profile flag or a generic name. Actually, this is fine — `conga` is a reasonable generic profile suggestion.
- Lines 38-41: These print `cfg.SSOStartURL`, `cfg.Region`, etc. — these will now come from config, so they'll show the user's values. If empty (shouldn't happen if init ran), they'll show empty. This is fine.

## 3. Documentation

### 3.1 Consolidate READMEs

**Delete** `cli/README.md` (move its content into root). The **root `README.md`** becomes the single project README with these sections:

1. **What is this** — One-paragraph project description
2. **Architecture** — Brief overview (single EC2 host, per-user Docker containers, zero-ingress VPC)
3. **Prerequisites** — AWS account, AWS CLI v2, session-manager-plugin, Go (for CLI build)
4. **Quick Start** — Ordered steps:
   a. Clone repo
   b. Run `terraform/bootstrap.sh` to create state backend
   c. Copy `backend.tf.example` → `backend.tf`, fill in values
   d. Copy `terraform.tfvars.example` → `terraform.tfvars`, fill in values
   e. Build and push Docker image (with PR #49514 fix note)
   f. `terraform init && terraform plan && terraform apply`
   g. Build CLI: `cd cli && go build -o conga .`
   h. `conga init` (configures CLI)
   i. `conga auth login` → `aws sso login`
   j. `conga secrets set anthropic-api-key`
   k. `conga connect`
5. **CLI Commands** — Table from current cli/README.md
6. **Onboarding a New User** — Admin + user steps
7. **Docker Image** — Section explaining the Slack bugfix requirement, linking to PR #49514, with instructions to build a patched image
8. **How It Works** — Brief architecture explanation

All example output uses placeholder values (`<account_id>`, `UXXXXXXXXXX`, etc.).

### 3.2 CLAUDE.md

Replace all real values with generic patterns:
- `123456789012` → `<account_id>` (or describe the pattern)
- `UEXAMPLE01` → `<member_id>`
- `CEXAMPLE01` → `<channel_id>`
- `exampleuser` → `<username>`
- `conga-myagent` → `conga-<member_id>`
- SSO URL → `<sso_start_url>`
- S3 bucket → `<project_name>-terraform-state-<account_id>`
- DynamoDB → `<project_name>-terraform-locks`

### 3.3 ROADMAP.md

Replace:
- `CEXAMPLE01` → `<channel_id>`
- `conga-terraform-state-123456789012` → `<project_name>-terraform-state-<account_id>`
- `conga-terraform-locks` → `<project_name>-terraform-locks`
- `vpc-067ea4b769f7e994a` → `<vpc_id>`
- `subnet-06119ed58d773bd9d` → `<subnet_id>`
- `sg-0f0c53457d0220f7c` → `<sg_id>`
- "Aaron" → "first user" or generic language
- `conga-myagent` → `conga-<member_id>`

## 4. Edge Cases

| Scenario | Behavior |
|----------|----------|
| `conga_image` empty in Terraform | `terraform plan` fails with clear validation error |
| `users = {}` in Terraform | Plan succeeds — creates infrastructure with no user containers |
| `~/.conga/config.toml` doesn't exist | `conga init` auto-triggers on first command, creates the file |
| `config.toml` partially filled | `conga init` prompts only for missing fields (skips already-configured ones) |
| ECR image in `user-data.sh.tftpl` | Auto-detects `.dkr.ecr.` in domain, runs `ecr get-login-password` |
| Non-ECR image (GHCR, Docker Hub) | Skips ECR login, just `docker pull` |
| `bootstrap.sh` run without project name arg | Defaults to `conga` |
| User runs `conga init` again | Overwrites config.toml with new values (re-prompts for all fields) |

## 5. Files Changed

| # | File | Change Type |
|---|------|-------------|
| 1 | `.gitignore` | Edit — add 2 entries |
| 2 | `terraform/variables.tf` | Edit — empty users default, add `conga_image` |
| 3 | `terraform/data.tf` | Edit — add `local.lock_table` |
| 4 | `terraform/outputs.tf` | Edit — use locals |
| 5 | `terraform/backend.tf.example` | New file |
| 6 | `terraform/terraform.tfvars.example` | Edit — placeholders |
| 7 | `terraform/user-data-shim.sh.tftpl` | Edit — template var for bucket |
| 8 | `terraform/compute.tf` | Edit — pass `state_bucket` |
| 9 | `terraform/user-data.sh.tftpl` | Edit — image, bucket, region vars |
| 10 | `terraform/router.tf` | Edit — pass new template vars |
| 11 | `terraform/bootstrap.sh` | Edit — dynamic derivation |
| 12 | `terraform/populate-secrets.sh` | Edit — env var defaults |
| 13 | `cli/pkg/config/config.go` | Edit — empty defaults, add fields, add validation |
| 14 | `cli/cmd/init.go` | New file — `conga init` command |
| 15 | `cli/cmd/root.go` | Edit — auto-trigger init, scrub example IDs |
| 16 | `cli/cmd/admin.go` | Edit — pass `Conga LineImage` to template |
| 17 | `cli/cmd/refresh.go` | Edit — pass `Conga LineImage` to template |
| 18 | `cli/scripts/add-user.sh.tmpl` | Edit — use `{{.Conga LineImage}}` |
| 19 | `cli/scripts/refresh-user.sh.tmpl` | Edit — use `{{.Conga LineImage}}` |
| 20 | `README.md` | Rewrite — consolidated project README |
| 21 | `cli/README.md` | Delete — consolidated into root |
| 22 | `CLAUDE.md` | Edit — replace all real values |
| 23 | `product-knowledge/ROADMAP.md` | Edit — replace real IDs |
