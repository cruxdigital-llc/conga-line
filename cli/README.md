# CruxClaw CLI

Cross-platform CLI for managing your OpenClaw deployment on AWS. Designed for non-technical users — all you need is AWS SSO credentials.

## Install

### One-liner (recommended)

Requires the [GitHub CLI](https://cli.github.com/) (`brew install gh && gh auth login`):

```bash
gh release download --repo cruxdigital-llc/crux-claw -p "cruxclaw_*$(uname -s | tr A-Z a-z)_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').tar.gz" -O- | sudo tar xz -C /usr/local/bin cruxclaw
```

This auto-detects your platform, downloads the latest release, and installs to `/usr/local/bin`.

### Build from source (for maintainers)

```bash
cd cli
go build -o cruxclaw .
```

### Prerequisites

You also need:

- **AWS CLI v2** — [Install guide](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html)
- **session-manager-plugin** (required for `cruxclaw connect`)
  - macOS: `brew install --cask session-manager-plugin`
  - Linux/Windows: [AWS install guide](https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html)

## Quick Start

### 1. Authenticate

```bash
# First time — configure your SSO profile
aws configure sso --profile openclaw
```

When prompted, use these settings:

| Setting | Value |
|---------|-------|
| SSO session name | `openclaw` |
| SSO start URL | `https://crux-login.awsapps.com/start/` |
| SSO region | `us-east-1` |
| SSO registration scopes | (leave default) |

Your browser will open to complete the SSO authorization. Then back in the terminal:

| Setting | Value |
|---------|-------|
| CLI default client Region | `us-east-2` |
| CLI default output format | `json` |
| CLI profile name | `openclaw` |

Then set it as your default profile so you don't need `--profile` on every command:

```bash
# Add to your ~/.zshrc or ~/.bashrc
export AWS_PROFILE=openclaw

# Log in (do this whenever your session expires)
aws sso login
```

### 2. Verify your identity

```bash
cruxclaw auth status
```

Output:
```
Identity:  arn:aws:sts::167595588574:assumed-role/.../aaronstone
Account:   167595588574
User:      UA13HEGTS
```

Your OpenClaw user is auto-detected from your IAM identity. No need to know your Slack member ID.

### 3. Set your API key

```bash
cruxclaw secrets set anthropic-api-key
# Prompts for value (hidden input)
```

### 4. Restart to pick up new secrets

```bash
cruxclaw refresh
```

### 5. Connect to the web UI

```bash
cruxclaw connect
```

This starts an SSM tunnel to your container's gateway, displays the auth token, and auto-approves device pairing. Open http://localhost:18789 in your browser.

## Commands

### User Commands

| Command | Description |
|---------|-------------|
| `cruxclaw auth login` | Show SSO setup instructions |
| `cruxclaw auth status` | Show your AWS identity and OpenClaw user |
| `cruxclaw secrets set <name>` | Create or update a secret |
| `cruxclaw secrets list` | List your secrets |
| `cruxclaw secrets delete <name>` | Delete a secret |
| `cruxclaw connect` | Open SSM tunnel to web UI |
| `cruxclaw refresh` | Restart container with fresh secrets |
| `cruxclaw status` | Show container status and resource usage |
| `cruxclaw logs` | Tail container logs |

### Admin Commands

| Command | Description |
|---------|-------------|
| `cruxclaw admin add-user <id> <channel>` | Provision a new user |
| `cruxclaw admin list-users` | Show all provisioned users |
| `cruxclaw admin remove-user <id>` | Remove a user |
| `cruxclaw admin cycle-host` | Stop/start the EC2 instance |

### Global Flags

| Flag | Description |
|------|-------------|
| `--profile` | AWS CLI profile (default: `AWS_PROFILE` env var) |
| `--region` | AWS region (default: us-east-2) |
| `--user` | Override auto-detected user |
| `--verbose` | Verbose output |

## Onboarding a New User

### Admin does:

```bash
# Provision the user on the instance
cruxclaw admin add-user U01NEWUSER C0NEWCHAN

# The CLI auto-assigns a gateway port and prompts for the user's SSO identity
```

### User does:

```bash
# 1. Set up AWS SSO (one time)
aws configure sso --profile openclaw
export AWS_PROFILE=openclaw

# 2. Log in
aws sso login

# 3. Add their Anthropic API key
cruxclaw secrets set anthropic-api-key

# 4. Refresh to pick up the new secret
cruxclaw refresh

# 5. Connect to the web UI
cruxclaw connect
```

## How It Works

The CLI discovers infrastructure via AWS APIs — no Terraform, no repo clone needed:

- **Instance**: Found by EC2 tag `Name=openclaw-host`
- **User config**: Stored in SSM Parameter Store at `/openclaw/users/{member_id}`
- **Identity mapping**: Your SSO username maps to your member ID via `/openclaw/users/by-iam/{sso_name}`
- **Secrets**: Managed in AWS Secrets Manager under `openclaw/{member_id}/`
- **Remote operations**: Executed via SSM RunCommand (no SSH, no ingress)
