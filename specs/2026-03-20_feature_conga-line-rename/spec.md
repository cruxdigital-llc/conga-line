# Spec: Conga Line Rename

## Overview

Comprehensive rename of the project from "OpenClaw"/"CruxClaw" to "Conga Line" and the CLI from `cruxclaw` to `conga`. This is a pure find-and-replace operation with no behavioral changes.

## Naming Map

| Pattern | Old | New | Notes |
|---------|-----|-----|-------|
| Brand | OpenClaw (our project) / CruxClaw / Crux Claw | Conga Line | Human-readable name |
| CLI binary | `cruxclaw` | `conga` | Command users type |
| Go module | `github.com/cruxdigital-llc/openclaw-template/cli` | `github.com/cruxdigital-llc/conga-line/cli` | Module path |
| GitHub repo | `cruxdigital-llc/crux-claw` | `cruxdigital-llc/conga-line` | Release URLs |
| `project_name` | `openclaw` | `conga-line` | Terraform variable, resource naming |
| SSM prefix | `/openclaw/` | `/conga/` | Parameter Store paths |
| Secrets prefix | `openclaw/` | `conga/` | Secrets Manager paths |
| S3 prefix | `openclaw/` | `conga/` | Object key prefixes |
| Docker prefix | `openclaw-` | `conga-` | Container and network names |
| Systemd prefix | `openclaw-` | `conga-` | Service unit names |
| Host path | `/opt/openclaw/` | `/opt/conga/` | EC2 filesystem |
| Log prefix | `/var/log/openclaw-` | `/var/log/conga-` | Log files |
| EC2 tag | `openclaw-host` | `conga-host` | Instance discovery tag |
| CW namespace | `OpenClaw` | `CongaLine` | CloudWatch metrics |
| CW log group | `/openclaw/gateway` | `/conga/gateway` | CloudWatch Logs |
| Host user | `openclaw` | `conga` | Linux user on EC2 |
| Sysctl conf | `99-openclaw.conf` | `99-conga.conf` | Sysctl drop-in |
| Router package | `openclaw-slack-router` | `conga-slack-router` | npm package name |
| Config file | `openclaw.json` | `openclaw.json` | **UNCHANGED** — upstream format |

## Do NOT Rename

- `ghcr.io/openclaw/openclaw:*` — upstream Docker image
- `github.com/openclaw/openclaw/*` — upstream GitHub links
- Issue references: `#45311`, `#49514`, `#9627`
- `openclaw.json` filename — upstream config format
- `/home/node/.openclaw/` — upstream container path
- `npx openclaw devices` — upstream CLI command
- Any description that clearly refers to "Open Claw" the upstream software project

## Phase 1: CLI Go Code

### 1a. Module path
**File**: `cli/go.mod` line 1
```
- module github.com/cruxdigital-llc/openclaw-template/cli
+ module github.com/cruxdigital-llc/conga-line/cli
```

### 1b. Import paths (all `.go` files)
Replace across all files in `cli/`:
```
- github.com/cruxdigital-llc/openclaw-template/cli/pkg/aws
+ github.com/cruxdigital-llc/conga-line/cli/pkg/aws

- github.com/cruxdigital-llc/openclaw-template/cli/pkg/discovery
+ github.com/cruxdigital-llc/conga-line/cli/pkg/discovery

- github.com/cruxdigital-llc/openclaw-template/cli/pkg/tunnel
+ github.com/cruxdigital-llc/conga-line/cli/pkg/tunnel

- github.com/cruxdigital-llc/openclaw-template/cli/pkg/ui
+ github.com/cruxdigital-llc/conga-line/cli/pkg/ui

- github.com/cruxdigital-llc/openclaw-template/cli/scripts
+ github.com/cruxdigital-llc/conga-line/cli/scripts

- github.com/cruxdigital-llc/openclaw-template/cli/cmd
+ github.com/cruxdigital-llc/conga-line/cli/cmd
```

**Affected files** (17 files with imports):
- `cli/main.go`
- `cli/cmd/root.go`, `admin.go`, `admin_setup.go`, `admin_provision.go`, `admin_cycle.go`, `admin_refresh_all.go`, `admin_remove.go`
- `cli/cmd/auth.go`, `connect.go`, `logs.go`, `refresh.go`, `secrets.go`, `status.go`
- `cli/pkg/discovery/agent.go`, `identity.go`, `instance.go`
- `cli/pkg/tunnel/tunnel.go`

### 1c. CLI binary name and help text
**File**: `cli/cmd/root.go`
```go
- const defaultInstanceTag = "openclaw-host"
+ const defaultInstanceTag = "conga-host"

- Use:   "cruxclaw",
+ Use:   "conga",

- Short: "CruxClaw — manage your OpenClaw deployment",
+ Short: "Conga Line — manage your OpenClaw deployment",

- Long:  "Cross-platform CLI for managing OpenClaw containers on AWS via SSM.",
+ Long:  "Cross-platform CLI for managing OpenClaw containers on AWS via SSM.",
  // (keep "OpenClaw containers" — refers to upstream software)

- `cruxclaw admin add-user`
+ `conga admin add-user`
```

**File**: `cli/cmd/version.go`
```go
- fmt.Printf("cruxclaw %s (commit: %s, built: %s)\n", ...)
+ fmt.Printf("conga %s (commit: %s, built: %s)\n", ...)
```

### 1d. SSM path constants
**File**: `cli/cmd/admin_setup.go`
```go
- "/openclaw/config/setup-manifest"
+ "/conga/config/setup-manifest"

- fmt.Sprintf("/openclaw/config/%s", key)
+ fmt.Sprintf("/conga/config/%s", key)

- `cruxclaw admin cycle-host`
+ `conga admin cycle-host`
```

**File**: `cli/cmd/admin_provision.go`
```go
- fmt.Sprintf("/openclaw/agents/%s", agentName)   (2 occurrences)
+ fmt.Sprintf("/conga/agents/%s", agentName)

- "/openclaw/config/state-bucket"   (2 occurrences)
+ "/conga/config/state-bucket"

- fmt.Printf("  1. cruxclaw secrets set ...
+ fmt.Printf("  1. conga secrets set ...

- fmt.Printf("  2. cruxclaw refresh ...
+ fmt.Printf("  2. conga refresh ...

- fmt.Printf("  3. cruxclaw connect ...
+ fmt.Printf("  3. conga connect ...
```

**File**: `cli/pkg/discovery/agent.go`
```go
- fmt.Sprintf("/openclaw/agents/%s", name)
+ fmt.Sprintf("/conga/agents/%s", name)

- `cruxclaw admin add-user`
+ `conga admin add-user`

- awsutil.GetParametersByPath(ctx, ssmClient, "/openclaw/agents/")   (2 occurrences)
+ awsutil.GetParametersByPath(ctx, ssmClient, "/conga/agents/")
```

### 1e. Docker/systemd/log references in CLI
**File**: `cli/cmd/status.go`
```go
- SVC=openclaw-%s
+ SVC=conga-%s

- `cruxclaw logs`
+ `conga logs`
```

**File**: `cli/cmd/logs.go`
```go
- "docker logs openclaw-%s --tail %d 2>&1"
+ "docker logs conga-%s --tail %d 2>&1"
```

**File**: `cli/cmd/connect.go`
```go
- '/opt/openclaw/data/%s/openclaw.json'
+ '/opt/conga/data/%s/openclaw.json'
  // Note: the filename openclaw.json stays — it's upstream format

- "cruxclaw status"
+ "conga status"

- "docker exec openclaw-%s npx openclaw devices list"
+ "docker exec conga-%s npx openclaw devices list"
  // Note: "npx openclaw" stays — it's the upstream CLI

- "docker exec openclaw-%s npx openclaw devices approve"
+ "docker exec conga-%s npx openclaw devices approve"
```

**File**: `cli/cmd/secrets.go`
```go
- "Manage your OpenClaw secrets"  → keep (refers to upstream)
- "your OpenClaw container"  → keep (refers to upstream)

- `cruxclaw refresh`  → `conga refresh`
- `cruxclaw secrets set`  → `conga secrets set`  (all example strings)

- fmt.Sprintf("openclaw/agents/%s/%s", ...)   (2 occurrences)
+ fmt.Sprintf("conga/agents/%s/%s", ...)

- fmt.Sprintf("openclaw/agents/%s/", ...)
+ fmt.Sprintf("conga/agents/%s/", ...)

- `cruxclaw secrets set <name>`
+ `conga secrets set <name>`
```

**File**: `cli/cmd/admin_remove.go`
```go
- fmt.Sprintf("/openclaw/agents/%s", agentName)
+ fmt.Sprintf("/conga/agents/%s", agentName)

- fmt.Sprintf("openclaw/agents/%s/", agentName)
+ fmt.Sprintf("conga/agents/%s/", agentName)

- fmt.Sprintf("openclaw/agents/%s/%s", agentName, s.Name)
+ fmt.Sprintf("conga/agents/%s/%s", agentName, s.Name)

- "/opt/openclaw/bin/update-dashboard.sh"
+ "/opt/conga/bin/update-dashboard.sh"
```

**File**: `cli/cmd/admin_cycle.go`
```go
- `cruxclaw status`
+ `conga status`
```

**File**: `cli/cmd/auth.go`
```go
- `cruxclaw auth login`
+ `conga auth login`
```

### 1f. Test files
**File**: `cli/pkg/aws/ssm_test.go`
```go
- "/openclaw/agents/myagent"  → "/conga/agents/myagent"
- "/openclaw/agents/leadership"  → "/conga/agents/leadership"
- "/openclaw/agents/by-iam/user@example.com"  → "/conga/agents/by-iam/user@example.com"
- "/openclaw/agents/"  → "/conga/agents/"
```

**File**: `cli/pkg/discovery/identity_test.go`
```go
- "/openclaw/agents/myagent"  → "/conga/agents/myagent"
```

### 1g. CLI script templates
**Files**: `cli/scripts/*.sh.tmpl`

In all templates (`add-user.sh.tmpl`, `add-team.sh.tmpl`, `refresh-user.sh.tmpl`, `refresh-all.sh.tmpl`, `remove-agent.sh.tmpl`, `deploy-behavior.sh.tmpl`):
```
- /opt/openclaw/  → /opt/conga/
- openclaw-$AGENT_NAME  → conga-$AGENT_NAME  (container, network, systemd, log)
- openclaw-router  → conga-router
- openclaw/agents/$AGENT_NAME/  → conga/agents/$AGENT_NAME/  (Secrets Manager)
- openclaw/shared/  → conga/shared/  (Secrets Manager)
- /openclaw/config/openclaw-image  → /conga/config/openclaw-image
  // Note: "openclaw-image" is the SSM config KEY name for the upstream image — rename to just "image"?
  // Decision: rename the key to "image" since it's our key, not upstream's
- /var/log/openclaw-  → /var/log/conga-
```

**Special case**: The config key `openclaw-image` in `variables.tf` should become `image` since it's our naming for "which Docker image to use", not an upstream reference.

### Verify Phase 1
```bash
cd cli && go build -o conga . && go test ./... && go vet ./...
```

## Phase 2: GoReleaser + CI

**File**: `cli/.goreleaser.yaml`
```yaml
- project_name: cruxclaw
+ project_name: conga

- binary: cruxclaw
+ binary: conga

- -X github.com/cruxdigital-llc/openclaw-template/cli/cmd.Version=...
+ -X github.com/cruxdigital-llc/conga-line/cli/cmd.Version=...
  (same for .Commit and .Date)

- name_template: "cruxclaw_{{ .Os }}_{{ .Arch }}"
+ name_template: "conga_{{ .Os }}_{{ .Arch }}"

- name: crux-claw
+ name: conga-line
```

**Files**: `.github/workflows/*.yml` — no openclaw/cruxclaw references found in CI workflows.

## Phase 3: Terraform

### 3a. Variable defaults
**File**: `terraform/variables.tf`
```hcl
- default     = "openclaw"   # aws_profile
+ default     = "conga-line"

- default     = "openclaw"   # project_name
+ default     = "conga-line"

- description = "... OpenClaw host. Size at ..."
+ description = "... Conga Line host. Size at ..."

- description = "... `cruxclaw admin setup` ..."
+ description = "... `conga admin setup` ..."

- "openclaw-image" = "Docker image for OpenClaw ..."
+ "image" = "Docker image for OpenClaw ..."
  // Keep "for OpenClaw" — describes the upstream software

- "openclaw-image" = "ghcr.io/openclaw/openclaw:2026.3.11"
+ "image" = "ghcr.io/openclaw/openclaw:2026.3.11"
  // Keep the actual image value — it's upstream

- "openclaw/shared/slack-bot-token"  → "conga/shared/slack-bot-token"
- "openclaw/shared/slack-app-token"  → "conga/shared/slack-app-token"
- "openclaw/shared/slack-signing-secret"  → "conga/shared/slack-signing-secret"
- "openclaw/shared/google-client-id"  → "conga/shared/google-client-id"
- "openclaw/shared/google-client-secret"  → "conga/shared/google-client-secret"

- # cruxclaw admin add-user
+ # conga admin add-user

- /openclaw/agents/<name>
+ /conga/agents/<name>
```

### 3b. Resource identifiers (Terraform-local names)
These are internal references — changing them doesn't affect AWS resource names (those use `var.project_name`).

| File | Old resource name | New resource name |
|------|-------------------|-------------------|
| `compute.tf` | `aws_launch_template.openclaw` | `aws_launch_template.conga` |
| `compute.tf` | `aws_instance.openclaw` | `aws_instance.conga` |
| `security.tf` | `aws_security_group.openclaw_host` | `aws_security_group.conga_host` |
| `iam.tf` | `aws_iam_role.openclaw_host` | `aws_iam_role.conga_host` |
| `iam.tf` | `aws_iam_instance_profile.openclaw_host` | `aws_iam_instance_profile.conga_host` |
| `ecr.tf` | `aws_ecr_repository.openclaw` | `aws_ecr_repository.conga` |
| `ecr.tf` | `aws_ecr_lifecycle_policy.openclaw` | `aws_ecr_lifecycle_policy.conga` |

All cross-references to these resources must also be updated (e.g., `aws_security_group.openclaw_host.id` → `aws_security_group.conga_host.id`).

### 3c. SSM parameter paths
**File**: `terraform/ssm-parameters.tf`
```hcl
- name  = "/openclaw/config/setup-manifest"
+ name  = "/conga/config/setup-manifest"

- name  = "/openclaw/config/state-bucket"
+ name  = "/conga/config/state-bucket"

- # cruxclaw admin setup
+ # conga admin setup

- /openclaw/config/<key>
+ /conga/config/<key>
```

### 3d. IAM policy paths
**File**: `terraform/iam.tf`
```hcl
- secret:openclaw/shared/*  → secret:conga/shared/*
- secret:openclaw/agents/*  → secret:conga/agents/*
- parameter/openclaw/*  → parameter/conga/*
- openclaw/router/*  → conga/router/*
- openclaw/bootstrap/*  → conga/bootstrap/*
- openclaw/behavior/*  → conga/behavior/*
- openclaw/scripts/*  → conga/scripts/*
- "openclaw/*"  → "conga/*"  (S3 prefix condition)
- "OpenClaw"  → "CongaLine"  (CloudWatch namespace)
```

### 3e. S3 object keys
**File**: `terraform/behavior.tf`
```hcl
- key = "openclaw/behavior/${each.value}"
+ key = "conga/behavior/${each.value}"

- key = "openclaw/scripts/deploy-behavior.sh"
+ key = "conga/scripts/deploy-behavior.sh"
```

**File**: `terraform/router.tf`
```hcl
- key = "openclaw/router/package.json"
+ key = "conga/router/package.json"

- key = "openclaw/router/src/index.js"
+ key = "conga/router/src/index.js"

- key = "openclaw/bootstrap/bootstrap.sh"
+ key = "conga/bootstrap/bootstrap.sh"
```

### 3f. Output names
**File**: `terraform/outputs.tf`
```hcl
- output "openclaw_host_sg_id"  → output "conga_host_sg_id"
- description "... OpenClaw host"  → "... Conga Line host"  (4 occurrences)
- cruxclaw connect  → conga connect
```

### 3g. Security group description
**File**: `terraform/security.tf`
```hcl
- description = "OpenClaw host - zero ingress, HTTPS + DNS egress only"
+ description = "Conga Line host - zero ingress, HTTPS + DNS egress only"
```

### 3h. Secrets comments
**File**: `terraform/secrets.tf`
```
- cruxclaw admin setup  → conga admin setup
- cruxclaw secrets set  → conga secrets set
- cruxclaw admin remove-agent  → conga admin remove-agent
```

### 3i. Example files
**File**: `terraform/terraform.tfvars.example`
```
- project_name = "openclaw"  → project_name = "conga-line"
- `cruxclaw admin setup`  → `conga admin setup`
- "openclaw-image"  → "image"
- "openclaw/shared/*"  → "conga/shared/*"
```

**File**: `terraform/backend.tf.example` — uses `<project_name>` placeholder, no change needed.

### Verify Phase 3
```bash
cd terraform && terraform validate
```

## Phase 4: Bootstrap / User Data Template

**File**: `terraform/user-data.sh.tftpl`

This is the largest single file. All changes are mechanical substitutions:

| Find | Replace | Count (approx) |
|------|---------|-----------------|
| `/var/log/openclaw-` | `/var/log/conga-` | 8 |
| `/opt/openclaw/` | `/opt/conga/` | 60+ |
| `openclaw-$AGENT_NAME` | `conga-$AGENT_NAME` | 25+ |
| `openclaw-router` | `conga-router` | 12 |
| `openclaw-image-refresh` | `conga-image-refresh` | 5 |
| `openclaw-config-check` | `conga-config-check` | 5 |
| `openclaw-session-metrics` | `conga-session-metrics` | 5 |
| `openclaw-integrity` | `conga-integrity` | 3 |
| `openclaw/shared/` | `conga/shared/` | 5 |
| `/openclaw/config/openclaw-image` | `/conga/config/image` | 2 |
| `/openclaw/agents/` | `/conga/agents/` | 4 |
| `openclaw/agents/$AGENT_NAME/` | `conga/agents/$AGENT_NAME/` | 4 |
| `useradd -m -s /bin/bash openclaw` | `useradd -m -s /bin/bash conga` | 1 |
| `usermod -aG docker openclaw` | `usermod -aG docker conga` | 1 |
| `99-openclaw.conf` | `99-conga.conf` | 1 |
| `cruxclaw admin setup` | `conga admin setup` | 1 |
| `"openclaw"` (PROJECT_NAME fallback) | `"conga"` | 1 |
| `/openclaw/gateway` (CW log group) | `/conga/gateway` | 2 |

**PRESERVE**: `openclaw.json` filename, `/home/node/.openclaw/` path (upstream).

**File**: `terraform/user-data-shim.sh.tftpl`
```
- /var/log/openclaw-bootstrap.log  → /var/log/conga-bootstrap.log
- openclaw/bootstrap/bootstrap.sh  → conga/bootstrap/bootstrap.sh
```

## Phase 5: Router

**File**: `router/package.json`
```json
- "name": "openclaw-slack-router"
+ "name": "conga-slack-router"
```

**File**: `router/src/index.js`
```js
- '/opt/openclaw/config/routing.json'
+ '/opt/conga/config/routing.json'
```

## Phase 6: Documentation

### CLAUDE.md
- "OpenClaw" (our project) → "Conga Line" where it refers to our deployment
- Keep "OpenClaw" where it describes the upstream software
- `cruxclaw` → `conga`
- `/openclaw/` paths → `/conga/`
- `openclaw-` container/network/systemd → `conga-`
- `/opt/openclaw/` → `/opt/conga/`
- `/var/log/openclaw-` → `/var/log/conga-`
- Keep: `ghcr.io/openclaw/openclaw:2026.3.11`, `openclaw.json`, upstream links

### README.md
- Title: "Crux Claw - Run an OpenClaw 'cluster' on AWS" → "Conga Line - Run an OpenClaw 'cluster' on AWS"
- `cruxclaw` → `conga` (command examples, install instructions)
- `crux-claw` → `conga-line` (GitHub repo in URLs)
- `openclaw-host` → `conga-host` (EC2 tag)
- `/openclaw/` paths → `/conga/`
- Keep: upstream links, `ghcr.io/openclaw/openclaw:2026.3.11`

### CONTRIBUTING.md
- `crux-claw` → `conga-line` (GitHub URL)

### SECURITY.md
- `crux-claw` → `conga-line` (GitHub URL)

## Phase 7: Spec Files (Historical)

Update references in all `specs/**/*.md` files:
- `cruxclaw` → `conga`
- `/openclaw/` → `/conga/`
- `openclaw-` (container/service) → `conga-`
- `/opt/openclaw/` → `/opt/conga/`

These are historical records — update to avoid confusion when referenced.

## Phase 8: Product Knowledge

**Files**: `product-knowledge/PROJECT_STATUS.md`, `ROADMAP.md`, `TECH_STACK.md`, `MISSION.md`, `standards/security.md`, `observations/observed-standards.md`

- "CruxClaw" → "Conga" / "Conga Line" as appropriate
- `cruxclaw` → `conga`
- `/openclaw/` → `/conga/`
- `openclaw-` (our naming) → `conga-`
- Keep upstream references

## Phase 9: Misc Config

**File**: `.claude/settings.local.json` — update any path references

## Edge Cases

1. **Config key rename**: `openclaw-image` → `image` in `variables.tf` and all SSM path references. This is our key naming the upstream image — cleaner to just call it `image`.

2. **CloudWatch namespace**: `"OpenClaw"` → `"CongaLine"` (no spaces in CW namespaces). This affects the IAM condition in `iam.tf` and any CW metric publishing in the bootstrap.

3. **`openclaw.json` filename in templates**: The bootstrap generates files named `$AGENT_NAME-openclaw.json` as intermediates, then copies them to `openclaw.json` in the data dir. The intermediate name can change to `$AGENT_NAME-config.json` for clarity, but the final `openclaw.json` must stay.

4. **`/home/node/.openclaw/`**: This is inside the Docker container and is the upstream Open Claw data directory. Do NOT rename.

5. **`OPENCLAW_IMAGE` shell variable**: This is a local variable in the bootstrap script holding the Docker image reference. Rename to `CONGA_IMAGE` for consistency (it's our variable, not upstream's).

6. **Secrets Manager path `openclaw/agents/$NAME/`**: The `sed` commands in templates that strip this prefix to derive env var names must be updated to strip `conga/agents/$NAME/` instead.

## Deployment Migration (Out of Scope for Code Changes)

For a live environment after the code rename:
1. `terraform state mv` for all renamed Terraform resources
2. Create new SSM parameters at `/conga/` paths
3. Create new Secrets Manager entries at `conga/` paths
4. Upload new S3 objects at `conga/` prefixes
5. Terminate and re-bootstrap the EC2 instance (simplest path)

## Verification Checklist

- [ ] `cd cli && go build -o conga . && go test ./... && go vet ./...`
- [ ] `cd terraform && terraform validate`
- [ ] Grep `cruxclaw` in `*.go`, `*.tf`, `*.yaml`, `*.sh`, `*.tftpl` → 0 hits
- [ ] Grep `openclaw` in `*.go`, `*.tf`, `*.sh`, `*.tftpl` → only upstream refs (`ghcr.io/openclaw`, `openclaw.json`, `/home/node/.openclaw`, `npx openclaw`)
- [ ] README install URLs reference `conga-line` repo
- [ ] CLAUDE.md reflects all new naming
- [ ] GoReleaser config produces `conga` binary
