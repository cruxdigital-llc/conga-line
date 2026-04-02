# Observed Standards

*This file is populated automatically by the `pattern-observer` module during normal workflow execution.*
*Items here are reviewed and promoted (or discarded) during `/glados/recombobulate`.*

---

<!-- Add observations below this line -->

### 2026-03-15 - S3 bucket names must include account ID
- **Source**: Implementation discovery — global namespace collision
- **Context**: `conga-terraform-state` was already taken; had to suffix with account ID
- **Proposed Standard**: "All S3 bucket names must be suffixed with the AWS account ID to avoid global namespace collisions"
- **Suggested Severity**: must
- **Confidence**: High
- **Status**: enforced — implemented in Terraform locals

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
- **Status**: promoted — security.md Principle #5 updated to "Secrets are protected at rest" with per-provider detail

### 2026-03-17 - Each OpenClaw user requires their own Slack app
- **Source**: Implementation discovery — Slack Socket Mode load-balances events across connections, causing ~50% message loss with shared apps
- **Context**: Attempted single-app multi-container architecture. Router prototype worked but blocked by OpenClaw HTTP webhook bug. Separate apps is the only working approach.
- **Proposed Standard**: "Each OpenClaw user/container must have its own Slack app with dedicated app and bot tokens. Do not share Slack apps across containers."
- **Suggested Severity**: must
- **Confidence**: Medium — this was resolved by the forked image with HTTP webhook fix. Single shared app + router is now the architecture.
- **Status**: superseded — single shared Slack app with HTTP fan-out router is the current architecture

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
- **Status**: enforced — implemented in PR #3 (open-source sanitization)

### 2026-03-18 - Docker image must be configurable, not hardcoded
- **Source**: User correction — pointed out upstream ghcr.io/openclaw/openclaw:latest doesn't work without PR #49514 bugfix
- **Context**: CLI scripts and Terraform templates had hardcoded image references that would fail for any new user
- **Proposed Standard**: "The OpenClaw Docker image name must be a configurable variable in both Terraform (openclaw_image) and CLI (config.toml openclaw_image). Never hardcode a specific registry/image."
- **Suggested Severity**: must
- **Confidence**: High
- **Status**: enforced — implemented in PR #3 (open-source sanitization)

### 2026-03-28 - Security defaults must be enforce, not validate
- **Source**: User explicit statement — "no definition for mode should default to enforce to honor our security first footprint"
- **Context**: Egress policy `mode` field defaulted to `validate` (warn-only). User directed that security controls should be active by default.
- **Proposed Standard**: "Security-relevant policy fields must default to the most restrictive option. Operators opt into permissive modes explicitly."
- **Suggested Severity**: must
- **Confidence**: High
- **Status**: promoted — implemented in Feature 18, `normalizeDefaults()` in `policy.go`

### 2026-03-28 - Agent data must survive all lifecycle operations
- **Source**: User explicit statement — asked to confirm no data loss and requested it as a critical standard
- **Context**: Planning AWS upgrade, concern about agent memory persistence across container restarts and config changes
- **Proposed Standard**: "Agent data directories must never be deleted, overwritten, or recreated by provisioning, refresh, or upgrade operations."
- **Suggested Severity**: must
- **Confidence**: High
- **Status**: promoted — formalized as "Agent Data Safety" standard in `architecture.md`

### 2026-04-01 - Internal package stays internal — new binaries must be in-module
- **Source**: Implementation constraint — Go `internal/` visibility rule forced restructuring
- **Context**: Terraform provider spec called for a separate Go module (`terraform-provider-conga/`), but Go prohibits importing `internal/` packages from external modules. Restructured to `cli/internal/tfprovider/` with `cli/cmd/terraform-provider-conga/` entry point.
- **Proposed Standard**: "New binaries that need access to `cli/internal/` must live within the CLI module as `cli/cmd/<binary-name>/`, not as separate Go modules."
- **Suggested Severity**: must
- **Confidence**: High
- **Status**: pending

### 2026-03-28 - CLI changes must be represented across all interfaces
- **Source**: User explicit statement — "Any changes to the CLI should support both human (arg) and agent (json) i/o as well as be represented in the MCP layer"
- **Context**: Adding `--delete-data` flag to teardown; user wanted to ensure parity across CLI, JSON, and MCP
- **Proposed Standard**: "Every CLI flag must have a JSON input field and an MCP tool parameter. Every command must have an MCP tool."
- **Suggested Severity**: must
- **Confidence**: High
- **Status**: promoted — formalized as "Interface Parity" standard in `architecture.md`
