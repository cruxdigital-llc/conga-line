# Requirements: Conga Line CLI

## Goal

Build a cross-platform CLI tool (`conga`) so non-technical users can manage their OpenClaw deployment with nothing but AWS SSO credentials — no repo clone, no Terraform, no bash.

## Success Criteria

1. A new team member can go from zero to web UI access with only:
   - AWS SSO credentials
   - Download `conga` binary from GitHub Releases
   - `conga auth login`
   - `conga secrets set anthropic-api-key`
   - `conga connect`
2. User identity is auto-resolved from IAM — no need to know Slack member IDs
3. CLI discovers infrastructure via AWS APIs (EC2 tags, SSM Parameter Store) — no Terraform state access
4. Single static binary runs on macOS (Intel + Apple Silicon), Linux (amd64 + arm64), and Windows (amd64)
5. Shell scripts remain functional for power users — CLI coexists, does not replace

## Commands

### User Commands
| Command | Replaces | Purpose |
|---------|----------|---------|
| `conga auth login` | manual `aws sso login` | Initiate SSO flow, cache credentials |
| `conga auth status` | — | Show identity, resolved user, session expiry |
| `conga secrets set <name>` | `onboard-user.sh` | Create/update a secret (hidden input prompt) |
| `conga secrets list` | — | List user's secrets |
| `conga secrets delete <name>` | — | Remove a secret |
| `conga connect` | `connect-ui.sh` | SSM tunnel + token display + device pairing |
| `conga refresh` | `refresh-user.sh` | Restart container with fresh secrets |
| `conga status` | — | Show container status |
| `conga logs` | — | Tail container logs |

### Admin Commands
| Command | Replaces | Purpose |
|---------|----------|---------|
| `conga admin add-user <id> <channel>` | `add-user.sh` | Provision new user on instance |
| `conga admin list-users` | — | Show all provisioned users |
| `conga admin remove-user <id>` | — | Tear down user container + cleanup |
| `conga admin cycle-host` | manual SSM | Stop/start EC2 instance for re-bootstrap |

## Design Decisions

- **Automatic user resolution**: CLI calls `sts:GetCallerIdentity`, looks up `/conga/users/by-iam/{identity}` in SSM Parameter Store. `--user` flag is optional override.
- **SSM Parameter Store for discovery**: User config (`slack_channel`, `gateway_port`) stored at `/conga/users/{member_id}`. IAM-to-user mapping at `/conga/users/by-iam/{iam_identity}`. Eliminates Terraform state dependency.
- **Embedded bash scripts**: Container setup and refresh scripts embedded in Go binary via `//go:embed`. Accepts duplication with Terraform `user-data.sh.tftpl` for self-contained binary.
- **`session-manager-plugin` dependency**: Required for port forwarding. CLI checks PATH and prints platform-specific install instructions if missing.
- **Config file at `~/.conga/config.toml`**: SSO defaults baked into binary, overridable via config file, overridable via CLI flags.

## Non-Goals (Phase 1)

- Homebrew tap or `curl | sh` install script — GoReleaser / GitHub Releases only
- Self-update command
- Interactive TUI (keep it a standard CLI)
- Replacing Terraform for infrastructure provisioning
