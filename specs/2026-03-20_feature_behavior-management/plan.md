# Plan: Behavior Management

## Approach

Behavior markdown files are authored in the repo, uploaded to S3 via Terraform, and composed into agent workspaces at runtime. The CLI is a thin controller — it triggers agent restarts, and the runtime (systemd ExecStartPre) handles S3 sync + composition automatically.

## Architecture

```
behavior/                (repo, version-controlled)
  base/
    SOUL.md              shared identity, philosophy, boundaries
    AGENTS.md            shared session guidelines, red lines
  user/
    SOUL.md              DM-only additions
    USER.md.tmpl         per-agent template ({{.AgentName}})
  team/
    SOUL.md              multi-human channel additions
    USER.md.tmpl         per-agent template ({{.AgentName}}, {{.SlackChannel}})
  overrides/             optional per-agent full replacements
    <agent_name>/
      SOUL.md

    ↓ terraform apply

S3: <bucket>/openclaw/behavior/**

    ↓ ExecStartPre (every container start)

/opt/openclaw/behavior/    (host staging area)

    ↓ deploy-behavior.sh (composition)

/opt/openclaw/data/<agent>/data/workspace/SOUL.md  (container-visible)
```

**Composition**: For each file, check `overrides/<agent>/` first. If no override, concatenate `base/<file>` + `<type>/<file>`. For USER.md, render the `.tmpl` with sed substitution.

**MEMORY.md**: Never touched — OpenClaw manages it.

## Implementation Steps

### 1. Create `behavior/` directory with initial content
- `behavior/base/SOUL.md` — shared identity (starting from OpenClaw's reference template)
- `behavior/base/AGENTS.md` — shared session guidelines
- `behavior/user/SOUL.md` — individual agent additions
- `behavior/user/USER.md.tmpl` — per-user template
- `behavior/team/SOUL.md` — team channel context, multi-user guidelines
- `behavior/team/USER.md.tmpl` — per-team template

### 2. Terraform: S3 upload (`terraform/behavior.tf`)
- `for_each` over `fileset()` to upload all behavior files
- `etag = md5(...)` for change detection (same pattern as `router.tf`)

### 3. Terraform: IAM update (`terraform/iam.tf`)
- Add `openclaw/behavior/*` to S3 read policy

### 4. Host-side deploy helper (`cli/scripts/deploy-behavior.sh.tmpl`)
- Installed to `/opt/openclaw/bin/deploy-behavior.sh` during bootstrap
- Arguments: `<agent_name> <agent_type>`
- Handles composition logic (override > concat), template rendering, ownership

### 5. Bootstrap integration (`terraform/user-data.sh.tftpl`)
- Add S3 sync for behavior files after router download
- Install deploy helper to `/opt/openclaw/bin/`
- Call deploy helper in `setup_agent_common()` (add `AGENT_TYPE` parameter)
- Add systemd `ExecStartPre` to sync S3 + compose on every container start
- Store agent type in `/opt/openclaw/config/<agent>.type` for ExecStartPre

### 6. CLI provisioning integration (`cli/scripts/add-user.sh.tmpl`, `add-team.sh.tmpl`)
- After workspace mkdir: sync S3, write type file, call deploy helper

### 7. New CLI command: `cruxclaw admin refresh-all`
- Bounces all agent containers via systemd restart
- Reconnects router to each agent's Docker network
- Generic command — not behavior-specific

### 8. State bucket name for provisioning scripts
- Store in SSM at `/openclaw/config/state-bucket` during `admin setup`
- Or derive from `<project_name>-terraform-state-<account_id>` pattern

## Files to Modify/Create

| File | Action |
|------|--------|
| `behavior/base/SOUL.md` | Create |
| `behavior/base/AGENTS.md` | Create |
| `behavior/user/SOUL.md` | Create |
| `behavior/user/USER.md.tmpl` | Create |
| `behavior/team/SOUL.md` | Create |
| `behavior/team/USER.md.tmpl` | Create |
| `terraform/behavior.tf` | Create |
| `terraform/iam.tf` | Modify — add S3 read for `openclaw/behavior/*` |
| `terraform/user-data.sh.tftpl` | Modify — S3 sync, helper install, ExecStartPre, setup_agent_common |
| `cli/scripts/deploy-behavior.sh.tmpl` | Create |
| `cli/scripts/refresh-all.sh.tmpl` | Create |
| `cli/scripts/embed.go` | Modify — embed new templates |
| `cli/scripts/add-user.sh.tmpl` | Modify — S3 sync + deploy helper call |
| `cli/scripts/add-team.sh.tmpl` | Modify — S3 sync + deploy helper call |
| `cli/cmd/admin_refresh_all.go` | Create |
| `cli/cmd/admin.go` | Modify — register refresh-all subcommand |

## Operator Workflow

**New agent**: `terraform apply` then `cruxclaw admin add-user/add-team` — behavior files deployed automatically.

**Update behavior**: Edit `behavior/`, `terraform apply`, `cruxclaw admin refresh-all`.

**Host cycle**: Bootstrap handles everything automatically.

**Any restart**: ExecStartPre syncs latest behavior from S3.

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| MEMORY.md overwritten | Deploy helper explicitly skips MEMORY.md |
| S3 sync fails on ExecStartPre | Use `2>/dev/null` or `|| true` — stale behavior is acceptable over failing to start |
| File permissions wrong | `chown 1000:1000` after every write |
| First-run workspace doesn't exist | mkdir -p already happens in setup_agent_common before deploy |
| ExecStartPre adds container start latency | S3 sync is fast (small files, same region) — ~1-2s acceptable |
