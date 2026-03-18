# Observed Standards

*This file is populated automatically by the `pattern-observer` module during normal workflow execution.*
*Items here are reviewed and promoted (or discarded) during `/glados/recombobulate`.*

---

<!-- Add observations below this line -->

### 2026-03-15 - S3 bucket names must include account ID
- **Source**: Implementation discovery — global namespace collision
- **Context**: `openclaw-terraform-state` was already taken; had to suffix with account ID
- **Proposed Standard**: "All S3 bucket names must be suffixed with the AWS account ID to avoid global namespace collisions"
- **Suggested Severity**: must
- **Confidence**: High
- **Status**: pending

### 2026-03-15 - Pin third-party module versions and verify provider compatibility
- **Source**: Implementation discovery — fck-nat v1.4.0 required AWS provider >= 6.0, breaking our ~> 5.0 constraint
- **Context**: `terraform init` failed until we upgraded the provider constraint
- **Proposed Standard**: "Always check third-party module provider requirements before pinning versions. Run `terraform init` early to catch version conflicts."
- **Suggested Severity**: should
- **Confidence**: High
- **Status**: pending

### 2026-03-15 - Secrets on disk tradeoff for systemd env files
- **Source**: Architecture decision during EC2 bootstrap spec
- **Context**: Systemd needs a way to re-inject env vars on container restart. Pure in-memory injection isn't feasible.
- **Proposed Standard**: "Secrets env files must be mode 0400, owned by the service user, on encrypted EBS. Document as accepted deviation from 'secrets never touch disk' principle."
- **Suggested Severity**: should
- **Confidence**: High
- **Status**: pending

### 2026-03-17 - Each OpenClaw user requires their own Slack app
- **Source**: Implementation discovery — Slack Socket Mode load-balances events across connections, causing ~50% message loss with shared apps
- **Context**: Attempted single-app multi-container architecture. Router prototype worked but blocked by OpenClaw HTTP webhook bug. Separate apps is the only working approach.
- **Proposed Standard**: "Each OpenClaw user/container must have its own Slack app with dedicated app and bot tokens. Do not share Slack apps across containers."
- **Suggested Severity**: must
- **Confidence**: High
- **Status**: pending

### 2026-03-17 - Verify third-party HTTP mode before building on it
- **Source**: Implementation discovery — spent significant time building a router only to find OpenClaw's HTTP webhook mode is broken in the compiled Docker image
- **Context**: OpenClaw logs "http mode listening" but the route is never registered on the gateway HTTP server due to a module identity split bug
- **Proposed Standard**: "Before building infrastructure that depends on a third-party feature, prototype and verify the feature works in isolation first."
- **Suggested Severity**: should
- **Confidence**: High
- **Status**: pending

### 2026-03-18 - Environment-specific values must never be hardcoded in committed files
- **Source**: User explicit statement — "All details NEED to be made available in a variables file that is gitignored with an example"
- **Context**: Preparing repository for open-source release; discovered 150+ hardcoded references across 22+ files
- **Proposed Standard**: "All environment-specific values (account IDs, Slack IDs, SSO URLs, usernames, deployed resource IDs) must be in gitignored config files with committed .example templates. Terraform backend.tf and terraform.tfvars are always gitignored."
- **Suggested Severity**: must
- **Confidence**: High
- **Status**: pending

### 2026-03-18 - Docker image must be configurable, not hardcoded
- **Source**: User correction — pointed out upstream ghcr.io/openclaw/openclaw:latest doesn't work without PR #49514 bugfix
- **Context**: CLI scripts and Terraform templates had hardcoded image references that would fail for any new user
- **Proposed Standard**: "The OpenClaw Docker image name must be a configurable variable in both Terraform (openclaw_image) and CLI (config.toml openclaw_image). Never hardcode a specific registry/image."
- **Suggested Severity**: must
- **Confidence**: High
- **Status**: pending
