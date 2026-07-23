# Requirements — Remote-MCP OAuth Credential Lifecycle

## Goal

Remote-MCP OAuth credentials must be a **managed, fleet-durable** part of a Conga agent. A credential
loss must be **detected proactively**, **recoverable in a single documented `conga` command** on any
provider, and — where the runtime permits — **restored automatically** across container/host lifecycle
events instead of requiring a manual browser re-auth each time.

## Who / why

- **Operators** running the fleet (self-hosted, one or many agents). Today they discover an OAuth
  outage only when an agent reports it can't reach a tool, then hand-run an undocumented flow.
- **Teams self-serving MCP servers** on their own agents (`team-a`, `team-b`, and every
  future team). Each new OAuth MCP server multiplies the surface that can silently break.

## Success criteria

### Phase 1 — Detect + one-command re-auth (ships first)

1. **Detection.** A fleet-wide check reports, per agent, any MCP server stuck in the
   `bundle-mcp … requires OAuth authorization` state (and, where cheaply knowable, a token that will
   expire soon). Exit status / output is scriptable so it can back an alert.
2. **First-class re-auth CLI.** `conga mcp login <agent> <server>` drives the two-leg flow:
   it starts the login in the target container, surfaces the authorize URL to the operator, accepts the
   returned `--code`, and completes the exchange — **identically on aws, local, and remote** providers.
3. **Discoverability.** The failure mode and the recovery command are documented (CLAUDE.md + a
   standards doc), and the detection output names the exact command to run.
4. **No regression to the manual escape hatch.** The raw `openclaw mcp login` path still works for
   anything the wrapper doesn't cover.

### Phase 2 — Persist + restore (durability)

5. **Capture.** After a successful `conga mcp login`, the resulting
   `mcp-oauth/<server>-<hash>.json` blob is written to the **per-agent secrets store**
   (AWS Secrets Manager `conga/agents/<name>/*` / local `~/.conga/secrets/agents/<name>/` /
   remote `/opt/conga/secrets/agents/<name>/`), mode-restricted like every other secret.
6. **Restore.** On provision **and** refresh, before the agent container starts, any persisted MCP
   OAuth blob for that agent is materialized into the container's `/home/node/.openclaw/mcp-oauth/`
   so the server starts authenticated with **no** manual re-auth.
7. **Survives lifecycle events (Mode A only).** After a host replacement / container reprovision /
   fresh provision of an agent whose persisted credential is still valid upstream, its OAuth MCP
   servers come up authenticated with **no** manual step. This covers **credential-loss** (Mode A in
   `spec.md` §1). It explicitly does **not** cover **refresh-token expiry/revocation** (Mode B) — that
   always needs a browser re-auth, which criteria 1–2 make detected + one-command. "Survives" is
   scoped to Mode A by design; see `spec.md` §1 and §4.4.
8. **Drift is resolved deterministically.** A documented precedence rule decides the winner when the
   on-disk blob and the stored secret differ (e.g. a fresh in-container `openclaw mcp login` re-captures
   and becomes the new source of truth). No silent, surprising overwrite of a good credential.

## Constraints / non-negotiables

- **All three providers** (aws, local, remote) via the existing `Provider` secrets abstraction and the
  `managedhost` `Transport` seam (`PutFile`/`RunCommand`/`ReadFile`). No provider-specific one-offs
  that skip the seam.
- **Runtime boundary respected.** OpenClaw is the only runtime with this flow today; the credential
  path (`/home/node/.openclaw/mcp-oauth/`) is OpenClaw-specific. Design so Hermes can plug in later
  without reworking the secrets/lifecycle plumbing — do **not** hard-assume the OpenClaw path fleet-wide.
- **Secrets hygiene.** OAuth blobs contain live access + refresh tokens. Never log values, never write
  them to Terraform state, keep restored files at the same 0600/root-safe posture as the env file, and
  respect the existing chown-before-start rule (`feedback_chown_fix.md`).
- **The browser authorize leg stays operator-side.** We are not automating a headless browser OAuth;
  the human approves in a browser. In scope is only making the surrounding flow first-class + durable.
- **Fits the config taxonomy.** This is *persistence/secrets* state, not infra (tfvars) and not the
  runtime overlay (`agent.yaml`) — respect `feedback_terraform_runtime_split.md` and
  `product-knowledge/standards/config-taxonomy.md`. The MCP *server definition* stays in
  `custom.json`; only the *credential* becomes managed secret state.

## Explicitly out of scope

- Automating the interactive browser approval step.
- Static (non-OAuth) MCP tokens already expressible as ordinary per-agent secrets.
- Non-MCP OAuth (Slack, Google) — those are already handled via the secrets store.
- Hermes implementation (design must not preclude it; building it is a follow-up).

## Open questions (to resolve in `spec-feature`)

- **Q1.** Does the `<hash>` in `<server>-<hash>.json` derive deterministically from the server
  URL/config (so restore can predict the filename), or is it random per registration? Determines
  whether Phase 2 stores by exact filename or restores the whole `mcp-oauth/` directory verbatim.
- **Q2.** Can OpenClaw refresh an expired access token from the persisted refresh token unattended, or
  does refresh-token expiry still force a full browser re-auth? Sets the true ceiling on "survives."
- **Q3.** Should capture (Phase 2 secret write) be an explicit step of `conga mcp login`, or a
  separate `conga mcp sync`/`export` so operators can also capture creds obtained via the raw flow?
- **Q4.** Detection transport: extend the existing status/logs MCP + CLI path with a log-scan, or have
  OpenClaw expose MCP server health more directly (`openclaw mcp list --status`)? (Depends on 2026.6.5
  capabilities — verify.)
