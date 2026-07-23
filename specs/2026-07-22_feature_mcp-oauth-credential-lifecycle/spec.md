# Technical Specification — Remote-MCP OAuth Credential Lifecycle

**Status**: Spec (pre-implementation) · **Branch**: `plan/fleet-mcp-oauth-provisioning`
**Reads**: `requirements.md`, `plan.md` in this dir.

---

## 0. Empirical findings (resolved open questions)

All verified live this session against `team-a` + `team-b` on the AWS host
(`ghcr.io/openclaw/openclaw:2026.6.5`).

| # | Question | Finding | Consequence for design |
|---|---|---|---|
| Q1 | Is `<hash>` in `mcp-oauth/<server>-<hash>.json` deterministic or random? | **Deterministic.** Both agents store the Linear cred as the *identical* `linear-4cca6302a658efcc.json` (same server URL `https://mcp.linear.app/mcp`); GitHub gets its own `github-336ff6f3750dcf7c.json`. Hash is derived from server config, stable across agents + reprovision. | Store/restore **by exact filename**. Filename is stable, so a restored file lands where OpenClaw looks. |
| Q2 | Does the persisted blob enable unattended recovery? | **Partial — two failure modes (see §1).** Blob holds `tokens.{access_token, refresh_token, expires_in:86100 (~24h), scope:"read write", token_type:"bearer"}` + a public PKCE `clientInformation` (`client_id`, **no** `client_secret`). Restore works **iff the refresh token is still valid upstream**. | Restore fixes *file loss*, not *token expiry/revocation*. Phase 1 (re-auth) and Phase 2 (restore) are complementary. |
| Q4 | Detection transport — is there a health subcommand in 2026.6.5? | **No.** `openclaw mcp list` supports only `--json` (lists *configured* servers, not health). `--status` is rejected. | Detection = **log-scan** for `bundle-mcp … requires OAuth authorization`. |
| Q3 | Capture as part of `login`, or separate? | Design decision (§4.1): **both** — capture is a step of `conga mcp login`, and re-capture runs on `refresh` so creds obtained via the raw flow are also persisted. | — |

**Blob format** (`mcp-oauth/<server>-<hash>.json`, mode 0600, owner uid 1000):
```
{ "discoveryState": {…}, "clientInformation": { "client_id", "client_id_issued_at",
  "client_name", "grant_types", "redirect_uris", "response_types",
  "token_endpoint_auth_method" }, "state", "codeVerifier", "lastAuthorizationUrl",
  "tokens": { "access_token", "refresh_token", "expires_in", "scope", "token_type" } }
```
Note: no absolute `expires_at` is stored — freshness is derived at runtime; **OpenClaw rewrites
this file on every token refresh (~daily)**, so the on-disk copy is authoritative over any stored
snapshot. This directly drives the non-destructive-restore + re-capture design.

---

## 1. The two failure modes (the core framing)

| Mode | Trigger | Symptom | Fixed by |
|---|---|---|---|
| **A — Credential lost** | Fresh provision of an agent; data-dir genuinely recreated; migration to a host without the persisted data volume. | `bundle-mcp … requires OAuth` with an **empty** `mcp-oauth/`. | **Phase 2** restore-from-secret (silent, no operator). |
| **B — Token expired / revoked** | Refresh token expired or revoked upstream (likely what hit `team-a` — its data dir persisted but the cred stopped working ~2026-07-18). | `bundle-mcp … requires OAuth` with a **present-but-dead** blob (or OpenClaw discarded it). | **Phase 1** detect + `conga mcp login` (browser re-auth — unavoidable). |

**Honest scope statement** (for `requirements.md` success criterion 7): "survives lifecycle events" is
true for **Mode A**. Mode B always requires a browser re-auth — the feature makes that *detected and
one-command*, not eliminated. On AWS the data volume is `prevent_destroy` EBS, so Mode A is rare there
(host *replacement* keeps the volume); Mode A dominates on local/remote and on genuine fresh provisions.

---

## 2. Interfaces & data model

### 2.1 `Runtime` interface addition (`pkg/runtime/runtime.go`)

Keep the OpenClaw-specific credential path out of provider code (OpenClaw-now / Hermes-later boundary):

```go
// OAuthStateDir returns the path, relative to ContainerDataPath(), where the
// runtime stores remote-MCP OAuth credential blobs, or "" if the runtime has
// no such state. OpenClaw: "mcp-oauth". Hermes: "" (no-op).
OAuthStateDir() string
```
- OpenClaw impl (`pkg/runtime/openclaw/`): returns `"mcp-oauth"`. Blob files are `<server>-<hash>.json`.
- Hermes impl (`pkg/runtime/hermes/`): returns `""`. All capture/restore logic guards on `!= ""`
  (mirrors the existing `CustomConfigFileName() == ""` no-op guard pattern).

### 2.2 Secret naming (per-agent secrets store)

Reserve a prefix so OAuth blobs are enumerable and never mistaken for env-var secrets:

```
mcp-oauth/<blob-filename>        e.g.  mcp-oauth/linear-4cca6302a658efcc.json
```
- **AWS**: `conga/agents/<name>/mcp-oauth/<blob-filename>` (Secrets Manager).
- **Local**: `~/.conga/secrets/agents/<name>/mcp-oauth/<blob-filename>` (mode 0400).
- **Remote**: `/opt/conga/secrets/agents/<name>/mcp-oauth/<blob-filename>` (mode 0400).
- Value = the blob JSON verbatim.
- **Critical**: these are **NOT** mapped into the env file. `common.SecretNameToEnvVar` / `ListSecrets`
  env-var mapping must **skip** the `mcp-oauth/` prefix (they are files to materialize, not `KEY=val`
  env vars). This is the one code path that must special-case the prefix.

### 2.3 No changes to `custom.json` / the MCP server definition

The server definition (`url`, `transport`, `auth: oauth`) stays in `agents/<name>/custom.json` →
`agent-managed-custom.json` (config-taxonomy layer 3). Only the *credential* becomes managed secret
state. No schema change to the include layers.

---

## 3. Phase 1 — Detect + one-command re-auth

### 3.1 `conga mcp login <agent> <server>` (CLI + JSON + MCP — Interface Parity MUST)

Two-step flow (the browser approval is operator-side, un-automatable):

- **Step 1 (start)**: `prov.ContainerExec(ctx, agent, ["openclaw","mcp","login",<server>])` → capture the
  authorize URL from stdout → present it.
- **Step 2 (complete)**: `prov.ContainerExec(ctx, agent, ["openclaw","mcp","login",<server>,"--code",<code>])`.

Surfaces:
- **CLI (human)**: `conga mcp login <agent> <server>` prints the URL, then interactively prompts for the
  `--code` (`ui.Prompt`). A `--code <code>` flag runs step 2 directly (non-interactive / resumable).
- **JSON I/O**: `--json` returns `{authorize_url, state}` for step 1; a second call with `--code`
  returns `{status:"ok"}`. `conga json-schema mcp-login` documents both.
- **MCP**: tool `conga_mcp_login` with params `{agent, server, code?}`. No `code` → returns
  `authorize_url` (agent/operator opens it). With `code` → completes. (This is exactly the flow used
  manually this session via `conga_container_exec`.)
- Wiring: attach `loginCmd` to the existing `mcpCmd` group (`internal/cmd/mcp.go`, which today hosts only
  `serve`); pattern from `internal/cmd/secrets.go`. Server name defaulting: if the agent has exactly one
  OAuth MCP server in `custom.json`, `<server>` may be omitted.
- **Phase 2 hook**: on a successful step 2, immediately **capture** (§4.1).

### 3.2 `conga doctor` — fleet OAuth health (CLI + JSON + MCP)

- New `internal/cmd/doctor.go` (pattern: `refresh.go`). Iterate `prov.ListAgents` → for each,
  `prov.GetLogs(ctx, name, N)` and scan for `bundle-mcp … requires OAuth authorization` (regex,
  capturing the server name from `failed to start server "<server>"`). De-dupe to latest occurrence.
- Output per broken agent/server: the **exact** remediation command
  `conga mcp login <agent> <server>`. Non-zero exit if any agent is unhealthy (scriptable → backs an
  alert). `--agent <name>` scopes to one.
- **JSON**: `{agents:[{name, servers:[{server, status:"needs_oauth", fix:"conga mcp login …"}]}]}`.
- **MCP**: tool `conga_doctor` (params `{agent?}`), sibling to `toolGetLogs` in
  `internal/mcpserver/tools_container.go`.
- Log-window caveat: a healthy server produces no line, and logs roll. `doctor` reports "no OAuth issue
  in last N lines," not a positive health assertion — documented in help text. (Future: if a later
  OpenClaw exposes real MCP health, swap the scan for it — isolated behind one function.)

### 3.3 Docs
- CLAUDE.md: add the failure mode + `conga mcp login` / `conga doctor` recovery next to the existing
  native-MCP-OAuth note (2026.6.5 requirement already documented).
- `product-knowledge/standards/`: extend `config-taxonomy.md` Secrets row to name the `mcp-oauth/`
  blob (already lists "OAuth tokens"), and add an operator runbook snippet.

---

## 4. Phase 2 — Persist + restore

### 4.1 Capture (blob → secret)

Triggered (a) at the end of a successful `conga mcp login`, and (b) during `RefreshAgent` (re-sync, so
the stored copy tracks the runtime-refreshed on-disk blob and catches raw-flow logins).

Algorithm (`common.CaptureMCPOAuth`, provider-agnostic given a read closure):
1. If `rt.OAuthStateDir() == ""` → no-op.
2. Enumerate `<dataDir>/<OAuthStateDir>/*.json` (AWS: `ReadFile`/`RunCommand ls`; local/remote:
   direct/`ReadFile`). Skip if empty.
3. For each blob file, `prov.SetSecret(ctx, agent, "mcp-oauth/"+filename, contents)`.
4. Never log the blob contents. Emit only counts + filenames.

Idempotent: re-capturing an unchanged blob rewrites the same secret value.

### 4.2 Restore (secret → blob) — **cold-only, non-destructive**

Runs on **provision** and **refresh**, in the pre-container-start window, after the dataDir chown:
- Local: `localprovider/provider.go` ~before `runAgentContainer` (beside plugin-seeding + `:974` chown).
- AWS: within `regenerateAgentConfigOnInstance` (`awsprovider/channels.go`) before
  `defineAndStartAgentService`.
- Remote: within `RefreshAgent` (`remoteprovider/provider.go`) before container start.

Algorithm (`common.RestoreMCPOAuth`):
1. If `rt.OAuthStateDir() == ""` → no-op.
2. `prov.ListSecrets(ctx, agent)` filtered to the `mcp-oauth/` prefix. Skip if none.
3. For each stored blob, target `<dataDir>/<OAuthStateDir>/<filename>`. **If the target file already
   exists on disk → skip it** (the on-disk copy is authoritative — it may be runtime-refreshed and
   newer). Only write when absent.
4. Write via the `Transport` seam (`managedhost.PutFile` on AWS/remote; direct write local), mode
   **0600**, owner **uid 1000** (chown on managed hosts; respects `feedback_chown_fix.md`).
5. Never log contents.

The cold-only rule is what makes restore compatible with the Agent-Data-Safety MUST (§6): restore never
mutates existing data, only repopulates a genuinely empty slot.

### 4.3 Drift / precedence (requirements Q8)
- **On-disk wins.** A present on-disk blob is never overwritten by restore (§4.2 step 3); instead
  `RefreshAgent`'s capture step (§4.1b) copies the on-disk blob *up* into the secret. So the flow is
  one-directional at steady state: runtime writes disk → refresh captures disk → secret. Restore only
  ever fires into an empty slot.
- A fresh `conga mcp login` overwrites the on-disk blob (OpenClaw) and the follow-on capture overwrites
  the secret → new source of truth. No surprising silent clobber of a good credential.

### 4.4 Residual risk — capture cadence (QA amendment)
Capture (§4.1b) runs on `conga refresh`, which is **operator-driven**, not automatic. If the provider
(e.g. Linear) rotates the refresh token on use, the persisted secret can go **stale** between refreshes;
a cold restore after a data loss beyond that window then yields a dead token → **Mode B** (needs
re-auth). This is bounded and detected (`doctor`), not silent, but it means Phase 2 does not *guarantee*
unattended recovery for low-`refresh`-frequency agents. Mitigations available if the risk proves real:
a periodic capture (heartbeat/cron) or a capture-on-token-rotation hook — deferred; not in scope unless
observed. Documented so the ceiling on "survives" is honest.

---

## 5. Edge cases & error handling

| Case | Handling |
|---|---|
| Refresh token expired/revoked (Mode B) but blob present | Restore skips (file present); server still fails; `doctor` flags it → operator runs `mcp login`. Feature can't auto-fix; **must not** claim success. |
| `conga mcp login` step 2 with a stale/expired `--code` | OpenClaw exchange errors; surface stderr verbatim; instruct to re-run step 1 (the `state`/`codeVerifier` are single-use — proven this session). |
| Capture when `mcp-oauth/` empty (no login yet) | No-op, not an error. |
| Restore when secret exists but on-disk newer | Skip (cold-only). Correct by design. |
| Partial blob / corrupt JSON in secret | Restore writes bytes verbatim (OpenClaw validates); if OpenClaw rejects, server fails → `doctor` catches. Optionally validate JSON parseability on capture and refuse to store garbage. |
| Two agents, same server, same filename | Secrets are **per-agent** namespaced (`conga/agents/<name>/…`) — no collision. Each agent's token is distinct even though the filename hash matches. |
| Hermes agent | `OAuthStateDir()==""` → all paths no-op. No Hermes behavior change. |
| `doctor` log window rolled past the error | Reports "clean in last N"; not a positive-health claim (documented). `--lines` overridable. |
| Concurrent `refresh-all` | Capture/restore are per-agent, within the existing `perAgentRefreshCtx` bound; no shared mutable state. |
| Egress to the MCP host not allowed | Out of this feature (egress is `custom.json`/tfvars concern); but `doctor` could note it as a follow-up. |

---

## 6. Data Safety (architecture.md — MUST)

The credential blob lives **inside** the agent data dir (`<dataDir>/mcp-oauth/`), and Agent Data Safety
rule 3 states refresh "must never touch the data directory contents." Compliance:
- **Restore is cold-only, non-destructive** (§4.2 step 3): it writes only into an *absent* file slot,
  never overwriting or deleting existing data. Repopulating a slot the data itself no longer has is
  recovery, not mutation — and only happens when the slot is empty.
- **Capture is read-only** against the data dir.
- No other data-dir path is written. `mcp-oauth/*.json` are the only files touched, and only when absent.
- **Data-persistence test** (per rule 6): provision → login → capture → simulate cold data dir → refresh
  → assert blob restored and byte-identical; and provision → login → refresh (warm) → assert on-disk
  blob **unchanged** (restore skipped).

## 7. Security (security.md — MUST §5)

- Blobs carry live access **and refresh** tokens → treated as secrets: stored in Secrets Manager (AWS,
  encrypted) / mode-0400 files (local/remote); restored files are mode 0600 owned by uid 1000 on
  encrypted EBS (AWS). Never in Terraform state, never logged.
- **Deliberate, documented deviation from "inject via env var"**: OpenClaw reads this credential from a
  file on disk; there is no env-var mechanism for it. Materializing a 0600 file in the data dir is the
  only option and does **not** reintroduce Issue #9627 (that concerns `${VAR}` substitution *inside
  openclaw.json*, which we still never do). Recorded as a NOTE in the standards gate.
- The `mcp-oauth/` prefix is excluded from env-file generation (§2.2) so a blob can never leak into the
  process table or `-e` args.
- **Authorization code in transport history (accepted, Phase 1)**: `conga mcp login … --code <code>`
  passes the code as an argv to `openclaw mcp login` via `ContainerExec`; on the AWS provider that argv
  is shell-quoted into an SSM `SendCommand`, so the code persists in SSM command-invocation history /
  CloudTrail. Accepted as low-risk: an OAuth **authorization code** is single-use and short-lived (spent
  for the token immediately, by the time any history is read), and — unlike the access/refresh tokens —
  never appears in argv (those live only in the in-container blob OpenClaw writes). If a threat model
  cares about SSM history retention, a follow-up can feed the code over **stdin** instead of argv.
  (Surfaced by PR #74 review.)

## 8. Test matrix (QA)

- **Unit**: `Runtime.OAuthStateDir()` (openclaw="mcp-oauth", hermes=""); `SecretNameToEnvVar` skips
  `mcp-oauth/`; `doctor` log-scan regex (positive/negative/multi-server/rolled-window);
  `CaptureMCPOAuth`/`RestoreMCPOAuth` (empty dir, present-file-skip, absent-file-write, multi-blob,
  corrupt-JSON, Hermes no-op).
- **Integration (local + remote suites — no AWS in CI per `project_no_aws_ci_integration`)**: provision →
  `mcp login` (mocked OpenClaw exchange) → capture → cold-dir refresh → restore byte-identical; warm-dir
  refresh → on-disk untouched; `doctor` flags a seeded `requires OAuth` log line with the exact fix cmd.
- **Interface parity**: each of `mcp login` / `doctor` exercised via CLI, `--json`, and MCP tool.
- **Live (AWS, manual, as this session)**: throwaway agent + real Linear server; capture; force a cold
  restore; confirm authenticated start; confirm Mode B (revoked) is *detected*, not falsely "fixed."

## 9. Task slices (for `implement-feature`)

1. **S1 — `Runtime.OAuthStateDir()`** + openclaw/hermes impls + `SecretNameToEnvVar` prefix skip. (`pkg/` → provider release.) Unit tests.
2. **S2 — `conga mcp login`** (CLI + JSON + MCP), all providers via `ContainerExec`. No capture yet.
3. **S3 — `conga doctor`** (CLI + JSON + MCP) log-scan detection.
4. **S4 — `common.CaptureMCPOAuth`** + wire into `mcp login` success + `RefreshAgent`.
5. **S5 — `common.RestoreMCPOAuth`** (cold-only) + wire into provision/refresh pre-start for all three providers.
6. **S6 — Docs** (CLAUDE.md, config-taxonomy runbook) + `json-schema` entries.
7. **S7 — Data-safety + parity tests** (§8), then `terraform-provider-conga` release (S1/S4/S5 touch `pkg/`).

**Sequencing**: S1→S2→S3 (Phase 1 shippable: immediate fleet relief) → S4→S5 (Phase 2 durability) →
S6/S7. Phase 1 alone (S2/S3, if kept in `internal/`) may not need a provider release; S1/S4/S5 do.

---

## Appendix — follow-up TODO: retire the GitHub PAT, switch to OAuth

**Verified 2026-07-22** (config + blob mtimes + logs, see README session log): `team-b`'s
**GitHub** MCP server authenticates via a **static `github_pat_…` bearer token embedded directly in
`agents/team-b/custom.json`** (→ `agent-managed-custom.json`) — the server def has **no
`auth: oauth`** field, only a `headers.Authorization` PAT. The `github-336ff6f3750dcf7c.json` OAuth
blob is a stale orphan (mtime 2026-06-11, never refreshed; not consulted). This is a plaintext
long-lived credential in a config include (contra security.md §5) and the tfvars comment claiming
`openclaw mcp login github` OAuth is inaccurate.

**Decision (operator, 2026-07-22)**: **switch GitHub to OAuth and retire the PAT.** Sequenced
**after** this feature ships — the credential-lifecycle machinery (detect + `conga mcp login` + persist/
restore) is exactly the right vehicle to manage the resulting GitHub OAuth credential. TODO steps when
picked up:
1. Rotate/revoke the exposed `github_pat_…` (it surfaced in tool output 2026-07-22 — do this regardless
   of timing; it does not depend on the feature).
2. Set `auth: "oauth"` on the `github` server in `agents/team-b/custom.json`, drop the
   `headers.Authorization` PAT, `conga refresh --agent team-b`.
3. `conga mcp login team-b github` (once this feature's CLI exists; until then the raw
   `openclaw mcp login github` flow).
4. Delete the stale orphan `github-336ff6f3750dcf7c.json` blob (or let capture overwrite it).
5. Fix the inaccurate tfvars comment (it already *describes* the intended OAuth end-state — make it true).

Not implemented by this feature; tracked here + in `PROJECT_STATUS.md`.
