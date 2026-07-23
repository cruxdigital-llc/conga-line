# CLAUDE.md

## Confidentiality — This Is a Public, Open-Source Repo (MUST)

Everything committed here — code and comments, **commit messages**, **PR titles/bodies**, `specs/**`,
`product-knowledge/**`, test fixtures, and any GLaDOS-produced artifact — is **public**. **Never put
real client, customer, deployment, agent, or person names — or operator-/client-specific identifiers
(AWS account/EC2 instance IDs, private IPs, Slack channel/member IDs, hostnames, secret values) — in any
committed or public-facing content.** An agent named after a customer (`<company>-team`) silently
discloses that customer; that is a breach. Use generic placeholders (`team-a`, `<user-agent>`,
`<account-id>`, `i-xxxx`, `10.x.x.x`) and describe real incidents generically. Real values live **only**
in gitignored config (`terraform.tfvars`, `backend.tf`), `~/.conga/`, and the local agent-memory store.
Naming a **public product** (e.g. "NVIDIA OpenShell") is fine; naming an **agent** after a client is
not. **Scrub before every commit, PR create/edit, and outward-facing send.** Full rule + placeholders:
`product-knowledge/standards/confidentiality.md` (severity: **must**, enforced by the GLaDOS standards gate).

## Project Overview

This is an infrastructure-as-code project deploying Conga Line (autonomous AI assistant) via pluggable providers. Supports **local Docker** (for dev/personal use), **remote SSH** (for VPS/bare-metal hosts), and **hardened AWS** (for teams/production). There is no application code — the deliverable is Terraform configuration + bootstrap scripts + a Go CLI.

## Key Context

- **Provider model**: CLI uses a `Provider` interface (`pkg/provider/provider.go`) with three implementations: `localprovider` (Docker CLI/file-based secrets), `remoteprovider` (SSH + Docker on any host), and `awsprovider` (EC2/SSM/Secrets Manager). Commands call `prov.Method()` and work identically on any provider.
- **Local architecture**: Per-agent Docker containers on the local machine. State in `~/.conga/`. No cloud services needed. Slack optional — can run gateway-only (web UI).
- **Remote architecture**: Per-agent Docker containers on any SSH-accessible host (VPS, bare metal, etc.). State on remote at `/opt/conga/`. Local config in `~/.conga/remote-config.json`. SSH tunneling for gateway access.
- **AWS architecture**: Single EC2 host (AL2023, ARM64) with per-agent Docker containers in a zero-ingress VPC. Instance sized at ~2GB per agent (e.g. r6g.medium for 3 agents). Each agent runs on a **deterministic static Docker network** `10.99.<idx>.0/24` (idx = `GatewayPort − 18789`): the agent container is pinned to `.2` and its Envoy egress proxy to `.3` (both via `docker run --ip`). This replaces Docker's auto-assigned `172.x` pools and is what makes the fleet **reboot-safe** (the proxy can't race the agent for the agent's `.2` on a simultaneous restart). See "Managed-Host Engine" below.
- **NAT**: fck-nat via `RaJiska/fck-nat/aws` module v1.4.0 (not AWS NAT Gateway)
- **Terraform state**: S3 bucket `<project_name>-terraform-state-<account_id>` + DynamoDB `<project_name>-terraform-locks`
- **Configuration**: Environment-specific values are in gitignored `terraform/terraform.tfvars` and `terraform/backend.tf`. See `.example` files.
- **Separation of concerns**: Terraform manages AWS infrastructure. CLI manages configuration (`admin setup`), agents (`admin add-user/add-team`), policies (`policy validate/deploy/set-*`), and channels (`channels add/remove/bind/unbind`). On AWS, agents are discovered from SSM Parameter Store at `/conga/agents/<name>` at boot time. On local, agents are stored as JSON files in `~/.conga/agents/`. On remote, agents are stored as JSON files on the remote host at `/opt/conga/agents/`.

## Provider System

- **Provider interface**: `pkg/provider/provider.go` — 26 methods covering identity, agent lifecycle, container ops, secrets, channels, connectivity, environment management, and teardown
- **Provider registry**: `pkg/provider/registry.go` — `Register(name, factory)` / `Get(name, cfg)`
- **AWS provider**: `pkg/provider/awsprovider/provider.go` — wraps existing `aws`, `discovery`, `tunnel` packages; drives the managed-host engine over an **SSM transport** (`ssmTransport`)
- **Managed-host engine**: `pkg/provider/managedhost/` — a **provider-agnostic Go provisioning engine** shared by the AWS and remote providers (AWS binds it over SSM, remote over SSH/SFTP). It owns the host-side lifecycle in tested Go behind a thin `Transport` seam (`{PutFile, RunCommand, ReadFile}`) instead of templated bash: `systemdSupervisor` renders + installs units (`ServiceSpec` → `RenderSystemdUnit`, `--env-file` always, never inline `-e`; `TimeoutStartSec=300`; bounded `Restart=always` policy), `AgentContainer` builds the `docker run` argv, `PlanAgentNetwork` computes the deterministic `10.99.<idx>` subnet + agent/proxy IPs, `ReconcileAgentNetwork` does fail-safe network migration (see below), `ReservedKeyGuardScript` emits the fail-closed PreStart guard, and `pkg/provider/iptables` generates the deterministic egress rules. `RefreshAgent` (not the provision scripts) owns config-gen + container start; the `add-user`/`add-team` scripts are infra-only (`refresh-user.sh.tmpl` was deleted).
- **Fail-safe network migration**: `ReconcileAgentNetwork` (`managedhost/network_reconcile.go`) migrates an agent's network to its static subnet **prepare-then-commit**: it force-disconnects foreign/dangling endpoints (e.g. a stale `conga-router` bridge endpoint) and flushes old-IP iptables BEFORE touching the agent, aborting with an actionable error (agent left **running on its old net**) if a Docker ghost can't be cleared; only once recreate is guaranteed does COMMIT stop → rm → step-verified `network rm` → create. A blocked migration never leaves an agent down. Returns `(migrated bool, err error)`; when `migrated`, `RefreshAgent` makes the post-migration egress redeploy fatal (an agent must never be left proxy-less).
- **Local provider**: `pkg/provider/localprovider/` — Docker CLI operations, file-based secrets (mode 0400), config integrity monitoring
- **Remote provider**: `pkg/provider/remoteprovider/` — SSH-based Docker operations on any remote host, file-based secrets (mode 0400), SSH tunneling for gateway access, config integrity monitoring
- **Common package**: `pkg/common/` — shared logic used by all providers: config generation, routing (`GenerateRoutingJSON`), behavior file resolution (`resolveBehaviorFiles`), manifest tracking, port allocation, validation
- **Behavior files**: `agents/_defaults/<runtime>/<type>/` has runtime+type-specific defaults (SOUL.md, AGENTS.md, USER.md.tmpl) for each combination of runtime (openclaw, hermes) and agent type (user, team). SOUL.md and AGENTS.md differ between user agents (privacy-focused, personal memory) and team agents (multi-user awareness, shared memory). `agents/<name>/` has per-agent overrides that fully replace the defaults. CLI: `conga agent {list,add,rm,show,diff}`. See `pkg/common/behavior.go` and `pkg/common/overlay.go`.
- **Per-agent runtime overlay**: `agents/<name>/agent.yaml` is an optional, versioned YAML file that lives alongside the prompt files. v1 carries `model:` (provider, name, base_url) to point a single agent at a self-hosted LLM. v2 adds `subagents:` for in-runtime delegation (see "Delegation Model" below). Future versions will absorb `memory:`, `tools:`, `limits:`, etc. — operators should NOT create new files per concern. Schema is strict-keyed; unknown keys fail loudly. See `agents/_example/agent.yaml.example` for the template and `product-knowledge/standards/config-taxonomy.md` for the full per-agent config taxonomy (infra → tfvars, policy → `conga-policy.yaml`, runtime overlay → `agent.yaml`, persistence → JSON/SSM, secrets → secrets store). Loader: `pkg/common/overlay_agent.go`; types: `pkg/runtime/overlay.go`.
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
- **Gateway runs in `mode: "local"`** (gateway + agent runtime in the same container — this is the default OpenClaw topology, unrelated to the congaline "remote" provider). The 0.0.0.0 binding inside the container that Docker `-p 127.0.0.1:<host>:18789` port forwarding requires comes from `gateway.bind: "lan"`, not from `mode`. OpenClaw v2026.3.22+ explicitly rejects `mode: "remote"` (split remote-transport topology) without `--allow-unconfigured`; we are not that topology.
- **`allowedOrigins`** must include both `localhost:18789` (for CLI tools inside the container) and `localhost:{hostPort}` (for browser access via SSM/SSH tunnels). Without both, `conga connect` gets "origin not allowed".

## Delegation Model

Two-tier delegation lets a top-tier orchestrator (Opus) drive personality + reasoning while a cheaper secondary model (typically Qwen via LiteLLM) handles mechanical work. See `specs/2026-05-22_feature_delegation-routing/`.

**Tier 1 — Subagents** (ephemeral, in-runtime). The agent's `agent.yaml` (schema `version: 2`) declares an optional `subagents:` block:

```yaml
version: 2
subagents:
  model:
    provider: openai
    name: qwen-2.5-72b-instruct
    base_url: https://litellm.lan/v1
  delegation_mode: prefer    # OpenClaw-only: "suggest" (default) or "prefer"
  max_concurrent: 4
  # max_spawn_depth: 1       # Hermes-only knob (1..3)
```

The orchestrator's runtime decides when to spawn a subagent — Conga's job is just to make the model available in the runtime config. Upstream mechanisms:
- **OpenClaw**: `sessions_spawn` tool + `agents.defaults.subagents.{model, delegationMode, maxConcurrent}` config. See `docs/tools/subagents.md` and `docs/concepts/parallel-specialist-lanes.md` in `github.com/openclaw/openclaw`.
- **Hermes**: `delegate_task` tool + `delegation:` block. See `website/docs/user-guide/features/delegation.md` in `github.com/NousResearch/hermes-agent`.

**Vocabulary note**: OpenClaw upstream uses **"delegate"** for an entirely different concept — a named org-identity agent acting on behalf of humans (`docs/concepts/delegate-architecture.md`). Both upstream runtimes converge on **"subagent"** for the ephemeral in-runtime concept Conga implements; we match that terminology.

**Tier 2 — Role agents** (persistent, model-bound). Five canonical roles ship as overlay packages under `agents/_defaults/<runtime>/role-*/`. Provision via `conga admin add-user --role <slug>` or `conga admin add-team --role <slug>`:

| Role | Slug | Type | Primary | Subagent | Use case |
|---|---|---|---|---|---|
| Operations | `role-ops` | user | Qwen | — | Monitoring, infra checks, status reports |
| Data | `role-data` | user | Qwen | — | Reporting, CSV/metrics analysis, format work |
| Research | `role-research` | user | Qwen | — | Web research, doc digests, competitive intel |
| Code/Dev | `role-code-dev` | team | Opus (runtime default) | Qwen | Code review, architecture, debugging |
| Writing | `role-writing` | team | Opus (runtime default) | Qwen | Drafts, edits, content strategy |

`role.meta` (single-line `type: user` or `type: team`) tells the CLI which sub-command the role expects. Mismatch (e.g. `add-user --role role-code-dev`) errors with an actionable message pointing at the correct command. The `--role` flag copies the role's `SOUL.md`, `AGENTS.md`, `USER.md.tmpl`, and `agent.yaml` into `agents/<name>/`, preserving any pre-existing operator customization — running `--role` twice is idempotent.

**Egress**: a v2 overlay's `base_url` host(s) need to be in the agent's egress allowlist (`agents.<name>.egress_allowed_domains` in tfvars, or the per-agent egress section of `conga-policy.yaml`). `conga admin add-user`/`add-team` calls `common.WarnOverlayEgressGaps` after the overlay loads and emits a stderr warning for every declared endpoint that isn't allowed. Non-blocking; the operator fixes the allowlist before first use.

**Customization**: every role package ships an `agent.yaml` with a placeholder `https://litellm.internal/v1` base_url. Operators MUST edit `agents/<name>/agent.yaml` after `--role` to point at their actual LLM proxy. The role's README explains.

## Planning

- GLaDOS planning docs in `product-knowledge/`
- Feature specs in `specs/YYYY-MM-DD_feature_name/`
- Security standards in `product-knowledge/standards/security.md` — review before making security-relevant changes
- Roadmap in `product-knowledge/ROADMAP.md`

## Channel × Runtime Compatibility

| Channel | OpenClaw runtime | Hermes runtime |
|---|---|---|
| `slack` | ✅ Supported | ✅ Supported |
| `telegram` | ❌ Unsupported — the v2026.5.x OpenClaw telegram plugin has no router-fanout receiver mode (see `specs/2026-05-22_feature_telegram-v2026.5-revamp/`) | ✅ Supported via the existing router |

Channels enforce this at provision + bind time via `Channel.SupportsRuntime`.
`conga admin add-user --runtime openclaw --channel telegram:<id>` (and the
`channels bind` equivalent) refuse with an operator-actionable error
pointing at the spec dir.

## Slack Architecture

- **Slack is optional** — agents can run in gateway-only mode (web UI) without any Slack configuration
- **Single shared Slack app** — one Slack app for all agents. The Slack event router (`router/slack/src/index.js`) holds the single Socket Mode connection and fans out events to per-agent containers via HTTP webhook. Telegram has its own router at `router/telegram/src/index.js`.
- **Containers use HTTP webhook mode** (`mode: "http"`) — they never connect to Slack directly. The router forwards events with signed HTTP requests.
- `signingSecret` and `botToken` are in `openclaw.json` under `channels.slack` (only when Slack is configured)
- `SLACK_APP_TOKEN` is held only by the router (in `router.env`) — containers do not need it
- Router runs with `--network host` and reaches each agent through its published `127.0.0.1:<hostPort>` (the agent's host-side `GatewayPort`). It is NOT attached to per-agent bridge networks — that hot-attach broke on Docker 25 + kernel 6.1.174 (route conflict). See `specs/2026-06-11_bugfix_router-host-networking/`. (Currently OpenClaw-only: Hermes serves its webhook on a separate, unpublished container port — loopback delivery for Hermes is a follow-up.)
- Routing config at routing.json maps channels and member IDs to container webhook URLs. With the host-networking router these are loopback URLs (`http://127.0.0.1:<hostPort>/slack/events`); `common.GenerateRoutingJSON` emits the bridge form (`http://conga-{name}:18789/slack/events`) only when no loopback resolver is passed.
- The deployed image is pinned to `ghcr.io/openclaw/openclaw:2026.6.5` (set via the `image` var default in `terraform/environments/production/variables.tf` and `terraform/modules/congaline/variables.tf`). Pinning to a specific minor (rather than tracking `:latest`) keeps deploys bisectable across upstream releases. Historical notes: (1) an earlier Slack socket-mode regression ([openclaw/openclaw#45311](https://github.com/openclaw/openclaw/issues/45311)) held the pin at `v2026.3.11` until it was resolved upstream in `v2026.3.22` (PR [#45953](https://github.com/openclaw/openclaw/pull/45953), Slack Bolt import-interop hardening). (2) v2026.5.18 leaked Claude `thinking` blocks into Slack channels through non-streaming delivery paths ([openclaw/openclaw#84319](https://github.com/openclaw/openclaw/issues/84319)); fixed in PR [#84322](https://github.com/openclaw/openclaw/pull/84322), first stable release containing the fix is v2026.5.20. (3) Upgraded 2026.5.26 → 2026.6.5 on 2026-06-11 for native remote-MCP OAuth (`openclaw mcp login <name>` + `--code`), used to authenticate the official Linear MCP server (`mcp.linear.app`) on a team agent without a static token on disk. 2026.6.5 is the stable that defers the risky session-metadata SQLite migration. **Native MCP OAuth requires ≥ 2026.6.x — 2026.5.26 lacks the `openclaw mcp login` subcommand.**

## Remote-MCP OAuth Credentials

Remote-MCP servers declared with `auth: "oauth"` (e.g. Linear `mcp.linear.app`) authenticate via a **per-container** OAuth credential OpenClaw stores at `/home/node/.openclaw/mcp-oauth/<server>-<hash>.json` (the `<hash>` is deterministic from the server URL; the blob holds access + refresh tokens). This state is **not** managed by tfvars, the secrets store, or `conga refresh` — so it silently fails after a token expires/is revoked, or is lost on a fresh provision / data-dir loss. Symptom in container logs: `[bundle-mcp] failed to start server "<name>": requires OAuth authorization`, and the agent reports it can't reach the tool. Requires OpenClaw ≥ 2026.6.x.

- **Detect**: `conga doctor` scans the fleet's logs for servers needing OAuth and prints the fix command per agent (with the last-error timestamp; a credential re-authed more recently is already fixed and the stale error ages out of the log window). `--agent <name>` scopes to one; non-zero exit if any need attention (scriptable). MCP tool: `conga_doctor`.
- **Re-auth**: `conga mcp login <server> --agent <name>` drives the two-leg flow (prints an authorize URL you open in a browser as the agent's identity → paste the `code` from the localhost redirect, or pass `--code`). `<server>` is optional when the agent has exactly one OAuth server. Works on all providers via `ContainerExec`. MCP tool: `conga_mcp_login` (call once for the URL, again with `code`). The browser approval is inherently operator-side.
- Full design + the planned Phase 2 (persist the blob to the per-agent secrets store + restore on provision/refresh so it survives host replacement) is in `specs/2026-07-22_feature_mcp-oauth-credential-lifecycle/`.

## OpenClaw Behavioral Issues

- **Billing/rate errors are cached**: When Anthropic returns a billing or rate limit error, OpenClaw's model fallback system caches the rejection. Even after the billing issue is resolved, the container must be restarted to clear the cached error state.
- **Container restart needs no router reconnection**: the router runs `--network host` and reaches every agent over loopback (`127.0.0.1:<hostPort>`), so a restarted agent container is reachable again the moment it rebinds its published port — there is no per-agent bridge re-attach. The agent unit's `ExecStartPost` re-applies the egress **iptables** rules (not router wiring). On AWS, `RefreshAgent()` regenerates `routing.json` and restarts the host-network router as a final step (to pick up added/removed agents); on local and remote, `RefreshAgent()` does the same after container restart.
- **Team agents leak preamble text to Slack channels** ([openclaw/openclaw#25592](https://github.com/openclaw/openclaw/issues/25592), still open at v2026.5.26): bare `text` content blocks emitted before a tool call (preamble narration, decision-not-to-reply prose, inter-tool commentary) are delivered to the channel as visible messages. Distinct from #84319 (structured `thinking` blocks), which is fixed. Conga's workaround for team agents only: `applyTeamChannelDiscipline` in `pkg/runtime/openclaw/config.go` emits `messages.groupChat.visibleReplies: "message_tool"` + `tools.alsoAllow: ["message"]`. Paired with a "Channel Discipline" section in team `AGENTS.md` defaults so the agent knows to call `message()` explicitly. Silent-drop risk if the model forgets the tool call (see [#85384](https://github.com/openclaw/openclaw/issues/85384) / closed-not-planned [#77320](https://github.com/openclaw/openclaw/issues/77320)). Full context: `product-knowledge/standards/upstream-openclaw-issues.md`.

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
- **Router networking**: The `conga-router.service` unit runs the router with `--network host`; it reaches each agent through its published `127.0.0.1:<hostPort>` (loopback delivery), so there is no per-agent bridge attach, no `connect-router-networks.sh` helper, and no `conga-router-networks.service` companion unit. See `specs/2026-06-11_bugfix_router-host-networking/`.
- **iptables egress rules**: Applied in agent systemd `ExecStartPost` from the agent's **pre-computed static IP** (`10.99.<idx>.2`, from `managedhost.PlanAgentNetwork`) — generated in Go (`pkg/provider/iptables/egressRuleSpecs`), no runtime IP-detection/retry loop. The rule set is deterministic: dst-subnet RETURN, ESTABLISHED/RELATED RETURN, **udp/tcp dport 53 RETURN (DNS — required so the container can reach the VPC resolver, which lives outside the per-agent subnet)**, then a final DROP (fail-closed). Cleaned up in `ExecStopPost`. Use `systemctl restart` (not `docker restart`) to ensure rules are properly cycled.
- **Reserved-key PreStart guard**: every agent unit runs a fail-closed `ExecStartPre` guard (`managedhost.ReservedKeyGuardScript`) that aborts container start if any `$include` config layer declares a Conga-owned reserved key (`$include`/`channels`/`gateway`/`plugins`). Prevention-first: a misconfigured include never reaches a running container.
- **`pre-start.sh` is flock-serialized**: the per-agent `ExecStartPre` `aws s3 sync` is wrapped in a bounded `flock -w 240` on `/var/lock/conga-prestart.lock`, so a simultaneous whole-fleet start (host reboot / docker daemon restart) can't run N concurrent syncs that blow `TimeoutStartSec` → crash-loop. The wait is bounded below `TimeoutStartSec=300` and proceeds on lock-timeout with on-disk behavior, so it can't deadlock.
- **`refresh-all` uses a per-agent timeout**: `conga admin refresh-all` bounds each agent's refresh independently (~6m each via `perAgentRefreshCtx`, decoupled from the global `--timeout`), so a large fleet processes every agent and one slow/failed agent doesn't starve the rest.

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
