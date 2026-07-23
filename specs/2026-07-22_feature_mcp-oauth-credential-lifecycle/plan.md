# High-Level Plan — Remote-MCP OAuth Credential Lifecycle

> Approach approved this session: **two phases**. Phase 1 (detect + one-command re-auth) ships and
> delivers value on its own; Phase 2 (persist + restore) adds durability. Code anchors below are the
> real symbols to build against — the detailed slicing happens in `spec-feature`.

## Architecture at a glance

```
                        ┌─────────────────── Phase 2 (durability) ───────────────────┐
  operator                                                                            │
    │ conga mcp login <agent> <server>                                                │
    ▼                                                                                 ▼
  ContainerExec ──► `openclaw mcp login` (in container) ──► mcp-oauth/<server>-<hash>.json
    │ (authorize URL out, --code in)                                    │  (0600, holds refresh token)
    │                                                                   │ capture
    ▼                                                                   ▼
  server authenticated NOW                                   per-agent secrets store
                                                             (SM / ~/.conga / /opt/conga)
                                                                        │ restore (before container start)
  Phase 1 detect: fleet scan of GetLogs for                            ▼
  `bundle-mcp … requires OAuth` ──► names the fix cmd        dataDir ▶ /home/node/.openclaw/mcp-oauth/
```

The credential is a per-server JSON blob at `<ContainerDataPath>/mcp-oauth/<server>-<hash>.json`
(OpenClaw `ContainerDataPath() = /home/node/.openclaw`, `pkg/runtime/openclaw/container.go:51`). The
dataDir is already bind-mounted into the container (`managedhost/container.go:60`), so restore is a
"write a file into dataDir before start" step — the same shape as the existing plugin-seeding + chown
window.

---

## Phase 1 — Detect + one-command re-auth

### 1a. `conga mcp login <agent> <server>` CLI (all providers)

- **Where**: attach a `login` child to the existing `mcpCmd` group — `internal/cmd/mcp.go:82`
  (`mcpCmd.AddCommand(loginCmd)`). Today `mcp` only hosts `serve`.
- **Pattern to copy**: the `secrets` sub-command group (`internal/cmd/secrets.go:15-52`) for parent/child
  wiring; `refresh.go:21-26` for agent resolution + `prov` handle usage.
- **Mechanism**: `cobra.ExactArgs(2)` (agent, server) →
  `prov.ContainerExec(ctx, agent, []string{"openclaw","mcp","login",server})` to get the authorize URL,
  print it, prompt for/accept `--code`, then `ContainerExec(... "openclaw","mcp","login",server,"--code",code)`.
  Works uniformly because `ContainerExec` is a Provider method with all three impls already
  (`provider.go:175`; local `docker.go`, remote `provider.go:617`, aws `provider.go:398`).
- **Ergonomics**: support a one-shot `--code` flag (skip the interactive prompt, for scripting) and echo
  the exact server name from `custom.json` so operators don't guess. The browser approval stays
  operator-side (out of scope to automate — see requirements).

### 1b. Fleet detection — `conga doctor`

- **Where**: new `internal/cmd/doctor.go` following `refresh.go`; iterate `prov.ListAgents` + `prov.GetLogs`
  and flag any agent whose recent logs contain `bundle-mcp … requires OAuth authorization`.
- **Output**: per-agent status; for each broken server, print the exact `conga mcp login <agent> <server>`
  to run. Scriptable exit code so it can back an alert.
- **MCP-tool parity**: add a sibling tool in `internal/mcpserver/tools_container.go` next to
  `toolGetLogs` (:49) / `toolGetProxyLogs` (:91) so the same check is available over the MCP surface
  (this is how the operator hit the problem this session).
- **Verify first (open Q4)**: check whether 2026.6.5 exposes `openclaw mcp list --status` for a cleaner
  signal than log-scraping; fall back to the log scan if not.

### 1c. Docs

- Add the failure mode + `conga mcp login` recovery to CLAUDE.md (near the existing native-MCP-OAuth
  note) and a new `product-knowledge/standards/` entry. `conga doctor` output self-documents the fix.

**Phase 1 exit**: an operator can discover a broken OAuth MCP server fleet-wide and fix it with one
documented command on any provider. No `pkg/` type changes strictly required beyond the new commands —
but see the release-flow note below (new CLI in `internal/` alone does **not** need a provider release).

---

## Phase 2 — Persist + restore

### 2a. Capture (on successful login)

- After `conga mcp login` reports success, read the blob back out of the container
  (`prov.ContainerExec` `cat`, or a `ReadFile` on the dataDir path) and store it via the existing
  per-agent secrets API `prov.SetSecret(ctx, agent, secretName, value)` (`provider.go:180`).
  - AWS → `conga/agents/<name>/<secret>` (`awsprovider/provider.go:854`)
  - Local → `~/.conga/secrets/agents/<name>/` mode 0400 (`localprovider/secrets.go:21,27`)
  - Remote → `/opt/conga/secrets/agents/<name>/` mode 0400 (`remoteprovider/secrets.go:22,97`)
- **Secret naming**: reserve a namespace, e.g. `mcp-oauth/<server>` (or a flattened
  `mcp-oauth-<server>` key), so restore can enumerate them via `prov.ListSecrets` and they're clearly
  distinguishable from ordinary env-var secrets. **Do not** map these into the env file — they are files
  to materialize, not `OPENAI_API_KEY`-style env vars.
- Resolve **open Q1** here: if `<hash>` is deterministic from server config, store the exact filename;
  if random, store the blob keyed by server name and reconstruct/[restore verbatim] the whole
  `mcp-oauth/` payload.
- Optionally expose capture standalone (`conga mcp sync`, **open Q3**) so creds obtained via the raw
  `openclaw mcp login` are also captured.

### 2b. Restore (on provision + refresh, before container start)

- **Runtime boundary (open, Architect)**: add a Runtime-interface method for the OAuth state location
  (e.g. `OAuthStateDir()` returning `mcp-oauth` under `ContainerDataPath()`), implemented in
  `pkg/runtime/openclaw/` and returning empty for Hermes (`pkg/runtime/runtime.go:22`). This keeps the
  OpenClaw path (`/home/node/.openclaw/mcp-oauth/`) out of provider code and lets Hermes plug in later.
- **Hook points** (materialize persisted blobs into `dataDir/<OAuthStateDir>/` right before container
  start, inside the existing pre-start window):
  - Local: `localprovider/provider.go` ~:981-989 (beside plugin-seeding, after the `:974` chown, before
    `runAgentContainer` :992).
  - AWS: within `regenerateAgentConfigOnInstance` (`awsprovider/channels.go:456`, uploads via
    `uploadFile`) before `defineAndStartAgentService` (`engine.go:35`).
  - Remote: within `RefreshAgent` (`remoteprovider/provider.go:720`) before its container start, via
    `p.ssh.Upload`.
- **Ownership/mode**: restored files must land at 0600 and be owned by the container user (uid 1000) —
  respect the existing chown-before-start rule (`feedback_chown_fix.md`); on managed hosts route through
  the `Transport` seam (`managedhost/transport.go:31` `PutFile`).

### 2c. Drift resolution (open Q8 / QA)

- Define precedence: a fresh in-container `openclaw mcp login` re-captures and **becomes the new source
  of truth** (secret is overwritten on next capture); restore never clobbers a **newer** valid on-disk
  blob silently. Document the rule; surface it in `conga doctor` when secret and on-disk disagree.

---

## Cross-cutting

- **Provider release flow**: any change under `pkg/` (Provider/Runtime interface additions, restore
  logic, secret plumbing) requires the two-repo release (`reference_provider_release_flow.md`). Phase 1
  can likely stay in `internal/` (no provider release); Phase 2 touches `pkg/` (does).
- **Security**: blobs carry live access + refresh tokens — never log values, never to Terraform state,
  same on-disk posture as the env file. (requirements → constraints.)
- **QA focus** (drives `spec-feature` test matrix): expired vs. missing token; host replacement; fresh
  provision; concurrent `refresh-all` (per-agent timeout already exists); partial/corrupt restore must
  not start-then-401; `<hash>` change across re-registration; secret↔on-disk drift precedence.
- **Verification**: live on the AWS host via `conga` MCP + SSM (as this session did) — provision a
  throwaway agent with an OAuth MCP server, capture, force a refresh/replace, confirm it comes back
  authenticated. Local/remote covered by the integration suite (no AWS in CI —
  `project_no_aws_ci_integration.md`).

## Sequencing recommendation (Product Manager)

Ship **Phase 1a + 1b + 1c** first (immediate fleet relief; the incident that started this is already
one `conga mcp login` away from being a documented, discoverable fix). Then **Phase 2**, gated on
resolving open Q1/Q2 (filename determinism + whether the refresh token truly survives unattended) in
`spec-feature`, since those set the real ceiling on "survives lifecycle events."

## Next step

Run `spec-feature` to turn this into a detailed technical spec + task slices (resolving open questions
Q1–Q4 from `requirements.md`).
