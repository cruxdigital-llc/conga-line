# CLAUDE.md

## Project Overview

This is an infrastructure-as-code project deploying Conga Line (autonomous AI assistant) via pluggable providers. Supports **local Docker** (for dev/personal use), **remote SSH** (for VPS/bare-metal hosts), and **hardened AWS** (for teams/production). There is no application code — the deliverable is Terraform configuration + bootstrap scripts + a Go CLI.

## Key Context

- **Provider model**: CLI uses a `Provider` interface (`pkg/provider/provider.go`) with three implementations: `localprovider` (Docker CLI/file-based secrets), `remoteprovider` (SSH + Docker on any host), and `awsprovider` (EC2/SSM/Secrets Manager). Commands call `prov.Method()` and work identically on any provider.
- **Local architecture**: Per-agent Docker containers on the local machine. State in `~/.conga/`. No cloud services needed. Slack optional — can run gateway-only (web UI).
- **Remote architecture**: Per-agent Docker containers on any SSH-accessible host (VPS, bare metal, etc.). State on remote at `/opt/conga/`. Local config in `~/.conga/remote-config.json`. SSH tunneling for gateway access.
- **AWS architecture**: Single EC2 host (AL2023, ARM64) with per-agent Docker containers in a zero-ingress VPC. Instance sized at ~2GB per agent (e.g. r6g.medium for 3 agents)
- **NAT**: fck-nat via `RaJiska/fck-nat/aws` module v1.4.0 (not AWS NAT Gateway)
- **Terraform state**: S3 bucket `<project_name>-terraform-state-<account_id>` + DynamoDB `<project_name>-terraform-locks`
- **Configuration**: Environment-specific values are in gitignored `terraform/terraform.tfvars` and `terraform/backend.tf`. See `.example` files.
- **Separation of concerns**: Terraform manages AWS infrastructure. CLI manages configuration (`admin setup`), agents (`admin add-user/add-team`), policies (`policy validate/deploy/set-*`), and channels (`channels add/remove/bind/unbind`). On AWS, agents are discovered from SSM Parameter Store at `/conga/agents/<name>` at boot time. On local, agents are stored as JSON files in `~/.conga/agents/`. On remote, agents are stored as JSON files on the remote host at `/opt/conga/agents/`.

## Provider System

- **Provider interface**: `pkg/provider/provider.go` — 26 methods covering identity, agent lifecycle, container ops, secrets, channels, connectivity, environment management, and teardown
- **Provider registry**: `pkg/provider/registry.go` — `Register(name, factory)` / `Get(name, cfg)`
- **AWS provider**: `pkg/provider/awsprovider/provider.go` — wraps existing `aws`, `discovery`, `tunnel` packages
- **Local provider**: `pkg/provider/localprovider/` — Docker CLI operations, file-based secrets (mode 0400), config integrity monitoring
- **Remote provider**: `pkg/provider/remoteprovider/` — SSH-based Docker operations on any remote host, file-based secrets (mode 0400), SSH tunneling for gateway access, config integrity monitoring
- **Common package**: `pkg/common/` — shared logic used by all providers: config generation, routing (`GenerateRoutingJSON`), behavior file resolution (`resolveBehaviorFiles`), manifest tracking, port allocation, validation
- **Behavior files**: `behavior/default/` has shared defaults (SOUL.md, AGENTS.md, USER.md.tmpl); `behavior/agents/<name>/` has per-agent overrides that fully replace the defaults. CLI: `conga agent {list,add,rm,show,diff}`. See `pkg/common/behavior.go` and `pkg/common/overlay.go`
- **Provider selection**: `--provider aws|local|remote` flag, persisted in `~/.conga/config.json` (default: `local`)
- **Slack is optional**: When no Slack tokens are provided, openclaw.json omits the `channels` section and the agent runs in gateway-only mode. The router is only started when Slack is configured.

## Working with Terraform

- All Terraform files are in `terraform/`
- Always `cd terraform` before running terraform commands
- AWS provider is `~> 6.0` (v6.36.0) — required by the fck-nat module
- `backend.tf` is gitignored (Terraform limitation — no variables in backend blocks). Copy from `backend.tf.example`
- `terraform.tfvars` is gitignored. Copy from `terraform.tfvars.example`
- S3 bucket names include the account ID suffix to avoid global namespace collisions

### Terraform Provider (`terraform-provider-conga`)

- **Separate repo**: `cruxdigital-llc/terraform-provider-conga` — imports `pkg/` from this repo
- **Registry**: `registry.terraform.io/providers/cruxdigital-llc/conga`
- **When to release a new provider version**: Any change to `pkg/` (common, provider, channels, policy, discovery) requires tagging congaline, updating the provider's `go.mod`, and publishing a new provider release. Changes only to `internal/`, `scripts/`, or `terraform/` do NOT require a provider release.
- **Release flow**: Tag congaline → `go get` + `go mod tidy` in provider repo → push → tag provider → GoReleaser publishes to registry
- **Local plugin cache**: `~/.terraform.d/plugins/registry.terraform.io/cruxdigital-llc/conga/` can cache stale versions. Delete before `terraform init -upgrade` if terraform can't find a new version.
- **SSM timeout minimum**: AWS SSM `SendCommand` requires `timeoutSeconds >= 30`. All `runOnInstance` and `uploadFile` calls must use `>= 30*time.Second`.

## Secrets

- **AWS provider**: Secrets in AWS Secrets Manager under `conga/shared/*` and `conga/agents/<name>/*`
- **Local provider**: Secrets as files under `~/.conga/secrets/shared/` and `~/.conga/secrets/agents/<name>/` (mode 0400)
- **Remote provider**: Secrets as files on remote host under `/opt/conga/secrets/shared/` and `/opt/conga/secrets/agents/<name>/` (mode 0400)
- Shared secrets created via `conga admin setup` (prompts interactively for missing values)
- Per-agent secrets — users self-serve via `conga secrets set`
- Never put real secret values in Terraform files or state
- OpenClaw reads secrets from environment variables (highest priority over config file)
- Do NOT use `${VAR}` substitution in `openclaw.json` — Issue #9627 causes secret values to be written to disk
- **AWS bootstrap requires `admin setup` first**: shared secrets must exist before cycling the host

## OpenClaw-Specific

- Docker image: configured via `conga admin setup`, stored in SSM (AWS), `~/.conga/local-config.json` (local), or `~/.conga/remote-config.json` (remote)
- Container runs as `node` user (uid 1000 inside container)
- Config at `/home/node/.openclaw/openclaw.json` inside container — mapped from host data directory
- Env file at `~/.conga/config/{agent_name}.env` (local), `/opt/conga/config/{agent_name}.env` (remote/AWS) — secrets, mode 0400
- OpenClaw hot-reload writes `.tmp` files next to `openclaw.json` — the config directory must be writable by the container user
- Container needs `NODE_OPTIONS="--max-old-space-size=1536"` to avoid V8 heap OOM
- Container memory limit: 2GB per agent (idle ~500MB, spikes to 1-1.5GB during heavy conversations)
- **Agents are keyed by agent name** (e.g. `myagent`, `leadership`), not Slack member ID or username
- Two agent types: **user agents** (DM-only, `dmPolicy: "allowlist"`) and **team agents** (channel-based, `groupPolicy: "allowlist"`)
- Gateway listens on port **18789** inside every container (`BaseGatewayPort` in `pkg/common/ports.go`). Each agent gets a unique **host** port (18789, 18791, 18792, etc.) via Docker `-p 127.0.0.1:{hostPort}:18789`. The `agent.GatewayPort` field is the host port, NOT the container port.
- **Gateway mode is always `"remote"`** (binds `0.0.0.0` inside the container) with `remote.url: "http://localhost:18789"`. This is an OpenClaw setting unrelated to the congaline "remote" provider — it means the gateway accepts connections from outside its network namespace (required for Docker port mapping and inter-container routing).
- **`allowedOrigins`** must include both `localhost:18789` (for CLI tools inside the container) and `localhost:{hostPort}` (for browser access via SSM/SSH tunnels). Without both, `conga connect` gets "origin not allowed".

## Planning

- GLaDOS planning docs in `product-knowledge/`
- Feature specs in `specs/YYYY-MM-DD_feature_name/`
- Security standards in `product-knowledge/standards/security.md` — review before making security-relevant changes
- Roadmap in `product-knowledge/ROADMAP.md`

## Slack Architecture

- **Slack is optional** — agents can run in gateway-only mode (web UI) without any Slack configuration
- **Single shared Slack app** — one Slack app for all agents. The Slack event router (`router/slack/src/index.js`) holds the single Socket Mode connection and fans out events to per-agent containers via HTTP webhook. Telegram has its own router at `router/telegram/src/index.js`.
- **Containers use HTTP webhook mode** (`mode: "http"`) — they never connect to Slack directly. The router forwards events with signed HTTP requests.
- `signingSecret` and `botToken` are in `openclaw.json` under `channels.slack` (only when Slack is configured)
- `SLACK_APP_TOKEN` is held only by the router (in `router.env`) — containers do not need it
- Router must be connected to each agent's Docker network (`docker network connect conga-<agent_name> conga-router`) so it can reach the container's webhook endpoint
- Routing config at routing.json maps channels and member IDs to container webhook URLs (`http://conga-{name}:18789/slack/events`)
- The deployed image is pinned to `ghcr.io/openclaw/openclaw:2026.3.11` (`29dc654`), the last stable release before a Slack socket mode regression in v2026.3.12 ([#45311](https://github.com/openclaw/openclaw/issues/45311))

## OpenClaw Behavioral Issues

- **Billing/rate errors are cached**: When Anthropic returns a billing or rate limit error, OpenClaw's model fallback system caches the rejection. Even after the billing issue is resolved, the container must be restarted to clear the cached error state.
- **Container restart reconnects router automatically**: On AWS, agent systemd units include `ExecStartPost` to reconnect the router. On local and remote, `RefreshAgent()` reconnects the router after container restart.

## Known Limitations

- Docker rootless mode deferred — AL2023 lacks `fuse-overlayfs` and `slirp4netns` packages needed for rootless Docker CE. Using standard Docker with cap-drop ALL, no-new-privileges, and resource limits instead.
- Config file cannot be made read-only at the filesystem level due to OpenClaw's hot-reload `.tmp` file behavior. Config integrity is enforced via hash-check monitoring.
- Env file with secrets is on disk (mode 0400). On AWS, encrypted EBS provides additional protection. On local, disk encryption is the user's responsibility.
- Local provider uses Docker bridge networks (not `--internal`) because `--internal` prevents `-p` port publishing to localhost. Isolation is enforced by separate per-agent networks, localhost-only port binding, and the egress proxy.

## Bootstrap Script Conventions

The AWS bootstrap script (`terraform/modules/infrastructure/user-data.sh.tftpl`) runs on EC2 boot via cloud-init. Key conventions:

- **umask 077** is set globally. Files that container users need must be explicitly `chown`'d: uid 1000 for node containers, uid 101 for Envoy egress proxies. Use `umask 022` subshells for `npm install`.
- **Terraform template escaping**: Bash `${VAR}` must be written as `$${VAR}` in `.tftpl` files. Only `${aws_region}`, `${project_name}`, `${state_bucket}`, and `${config_check_interval_minutes}` are Terraform interpolations.
- **Bootstrap sentinel**: `/opt/conga/.bootstrap-complete` is written only on full success. The `terraform_data.bootstrap_ready` resource polls for it via SSM, blocking the congaline module until the host is ready.
- **Router network connections**: The router must be connected to every agent's Docker network. The `connect-router-networks.sh` helper discovers networks via `docker network ls --filter name=conga-`. It runs at boot, via the router's `ExecStartPost`, and via the `conga-router-networks.service` companion unit.
- **iptables egress rules**: Applied in agent systemd `ExecStartPost` with a 10-retry IP detection loop. Cleaned up in `ExecStopPost`. Use `systemctl restart` (not `docker restart`) to ensure rules are properly cycled.

## Debugging

### AWS
- Connect to instance: `aws ssm start-session --target <instance-id> --region <region> --profile <profile>`
- Bootstrap log: `cat /var/log/conga-bootstrap.log`
- Service status: `systemctl status conga-<agent_name>`
- Container logs: `docker logs conga-<agent_name> --tail 50`
- Journal: `journalctl -u conga-<agent_name> --no-pager -n 50`

### Remote
- Container status: `conga status --agent <name>`
- Container logs: `conga logs --agent <name>`
- Config file (on remote): `/opt/conga/data/<name>/openclaw.json`
- Env file (on remote): `/opt/conga/config/<name>.env`
- Agent config (on remote): `/opt/conga/agents/<name>.json`
- SSH into host: use credentials from `~/.conga/config.json` (ssh_host, ssh_user, ssh_key_path)
- Teardown and restart: `conga admin teardown && conga admin setup --provider remote`

### Local
- Container status: `conga status --agent <name>`
- Container logs: `conga logs --agent <name>` or `docker logs conga-<name> --tail 50`
- Config file: `cat ~/.conga/data/<name>/openclaw.json`
- Env file: `cat ~/.conga/config/<name>.env`
- Agent config: `cat ~/.conga/agents/<name>.json`
- Teardown and restart: `conga admin teardown && conga admin setup --provider local`
