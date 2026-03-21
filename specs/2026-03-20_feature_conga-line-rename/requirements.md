# Requirements: Conga Line Rename

## Goal
Rename the project from "OpenClaw"/"CruxClaw" to "Conga Line" and the CLI from `cruxclaw` to `conga`. Every project-owned reference changes; upstream Open Claw references (Docker image, GitHub links, issue numbers) stay as-is.

## Naming Convention

| Context | Old | New |
|---------|-----|-----|
| Human-readable brand | OpenClaw / CruxClaw / Crux Claw | Conga Line |
| CLI binary | `cruxclaw` | `conga` |
| Go module path | `github.com/cruxdigital-llc/openclaw-template/cli` | `github.com/cruxdigital-llc/conga-line/cli` |
| GoReleaser project | `cruxclaw` | `conga` |
| GitHub repo | `cruxdigital-llc/crux-claw` | `cruxdigital-llc/conga-line` |
| Terraform `project_name` default | `openclaw` | `conga-line` |
| SSM parameter prefix | `/openclaw/` | `/conga/` |
| Secrets Manager prefix | `openclaw/` | `conga/` |
| S3 object prefix | `openclaw/` | `conga/` |
| Docker container names | `openclaw-{agent}` | `conga-{agent}` |
| Docker network names | `openclaw-{agent}` | `conga-{agent}` |
| Systemd units | `openclaw-{agent}.service` | `conga-{agent}.service` |
| Host paths | `/opt/openclaw/` | `/opt/conga/` |
| Log files | `/var/log/openclaw-*` | `/var/log/conga-*` |
| EC2 instance tag | `openclaw-host` | `conga-host` |
| Router container | `openclaw-router` | `conga-router` |
| Router package name | `openclaw-slack-router` | `conga-slack-router` |
| Config file name | `openclaw.json` | `openclaw.json` (upstream — unchanged) |

## Do NOT Rename
- Docker image: `ghcr.io/openclaw/openclaw:*` — this is the upstream project
- GitHub links to upstream: `github.com/openclaw/openclaw/*`
- Upstream issue references: `#45311`, `#49514`, etc.
- The `openclaw.json` config file name — this is an upstream Open Claw format
- Any reference clearly describing the upstream Open Claw project/software

## Success Criteria
1. `go build` produces a `conga` binary that works identically to the old `cruxclaw`
2. All Terraform resource names, SSM paths, S3 prefixes, and Secrets Manager paths use new naming
3. All docs (CLAUDE.md, README, product-knowledge/, specs/) use "Conga Line" / `conga` branding
4. GoReleaser produces `conga_*` release artifacts
5. Bootstrap script, systemd units, Docker names all use new naming
6. No upstream Open Claw references are accidentally changed
7. CI passes (`go test`, `go vet`, `terraform validate`)

## Out of Scope
- Migrating live AWS resources (SSM params, secrets, S3 objects) — that's a deployment concern
- Renaming the GitHub repository itself — that's a manual step
- Renaming the local directory on disk
