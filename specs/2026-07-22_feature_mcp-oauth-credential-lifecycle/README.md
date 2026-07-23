# Feature: Remote-MCP OAuth Credential Lifecycle (fleet-scalable)

**Trace Log** — GLaDOS `plan-feature` workflow

- **Created**: 2026-07-22
- **Owner**: <operator>
- **Status**: ✅ **COMPLETE — shipped + released + live-verified** (2026-07-23). congaline `v0.0.32` /
  `terraform-provider-conga` `v0.1.10` (on the registry). Merged via PRs #74, #75, #77, #78, #79.
  Phase 1 (detect + re-auth) and Phase 2 (persist + restore, all 3 providers) both done. Optional
  follow-ups deferred (see bottom).
- **Spec dir**: `specs/2026-07-22_feature_mcp-oauth-credential-lifecycle/`
- **Origin**: Live incident on `team-a` (this session). The agent reported "no access to
  Linear". Root cause was **not** the recent WireGuard-VPN-to-DGX-Spark egress change (the operator's
  first hypothesis) — the Linear MCP server definition was intact in `agent-managed-custom.json` and
  `mcp.linear.app` was still in the effective egress allowlist (and egress mode is `validate` =
  log-only anyway). The real cause: the container's OAuth credential had gone missing, so
  `[bundle-mcp] failed to start server "linear": requires OAuth authorization. Run openclaw mcp login
  linear` had been firing on every turn since ~2026-07-18. Fixed live via the manual two-leg flow
  (`openclaw mcp login linear` → authorize URL → `--code`). That manual recovery does not scale as
  teams self-configure MCP servers across the fleet.

## One-line

Make remote-MCP OAuth credentials a **managed, fleet-durable** part of a Conga agent — so a token loss
is **detected** (not discovered by a confused agent), **re-authed in one command** across all
providers, and **survives** container/host lifecycle events instead of vanishing on every reprovision.

## Problem statement (ground truth from this session)

OpenClaw stores each remote-MCP server's OAuth credential as a **per-container** file:
`/home/node/.openclaw/mcp-oauth/<server>-<hash>.json` (mode 0600). On `team-a` the live file was
`linear-4cca6302a658efcc.json` (2825 bytes) with keys:
`discoveryState`, `clientInformation` (the DCR-registered OAuth client), `state`, `codeVerifier`,
`lastAuthorizationUrl`, `tokens` (access **and refresh** token).

Consequences:
- **Invisible to the declarative fleet.** It lives in container state, not `terraform.tfvars` and not
  the per-agent secrets store. `terraform apply` / `conga refresh` neither manage nor restore it
  (contrast `project_tfvars_canonical_aws.md`: tfvars is meant to be the canonical fleet source on AWS).
- **Ephemeral across lifecycle events.** Any container reprovision / host replacement / fresh provision
  wipes it (cf. `project_aws_host_replacement_recovery.md`). Tokens can also simply expire.
- **Failure is silent + late.** The only signal is a `bundle-mcp … requires OAuth` line buried in
  container logs; the operator learns about it when the agent says it "can't access Linear."
- **Recovery is manual + interactive + operator-side** (browser authorize leg + paste `--code`) and
  is **not documented as a first-class `conga` workflow**. Already affects `team-a` (Linear) and
  `team-b` (Linear **and** GitHub); grows with every team that self-serves an MCP server.

## Chosen direction (operator decision, this session)

**Two-phase** (see `plan.md`):
1. **Detect + one-command re-auth** — a fleet health check that surfaces `requires OAuth`, plus a
   first-class `conga mcp login <agent> <server>` CLI wrapping the two-leg flow across all three
   providers. Delivers recovery ergonomics fast.
2. **Persist + restore** — snapshot the `mcp-oauth/<server>-<hash>.json` blob into the per-agent
   secrets store after a successful login, and restore it into container state on provision/refresh —
   so the credential (carrying the refresh token) survives host replacement and reprovision.

## Active Personas

- **Architect** — the per-agent secrets-store abstraction across the 3 providers (AWS Secrets Manager /
  local files / remote files), OAuth-blob format + naming (`<server>-<hash>.json`), refresh-token
  rotation semantics, exactly where restore hooks into the `managedhost` provision/refresh lifecycle
  (before container start), and the OpenClaw-now / Hermes-later runtime boundary for state paths.
- **QA** — the central promise is "credentials survive lifecycle events." Adversarial coverage: expired
  vs. missing token, host replacement, fresh provision, concurrent `refresh-all`, partial/corrupt
  restore, secret-vs-on-disk drift (which wins?), the `<hash>` in the filename changing across
  re-registration, and never leaving a half-restored credential that starts then 401s.
- **Product Manager** — scope discipline (ship Phase 1 before Phase 2; don't boil the ocean), the
  self-serve-teams UX ("discoverable + low-friction" made concrete), and what an operator is expected
  to do after a deliberate host replacement.

## Active Capabilities

- **conga MCP / AWS SSM** — live fleet introspection + `container_exec` (used this session to locate
  the on-disk blob and run the live re-auth). Enables live verification on the AWS host.
- **GitHub (`gh`)** — PR flow + the two-repo provider-release flow (any `pkg/` change → tag congaline →
  bump + release `terraform-provider-conga`).
- _No browser/UI or DB tools relevant — this is infra/provisioning + CLI work. The one browser leg
  (OAuth authorize) is inherently operator-side and out of scope for automation._

## Session log

- **2026-07-22** — Diagnosed the `team-a` Linear outage live (ruled out the VPN/egress hypothesis;
  confirmed missing OAuth credential via container logs + `agent_show_config` + proxy logs). Restored
  service with the manual `openclaw mcp login linear` two-leg flow. Located the on-disk credential blob
  (`mcp-oauth/linear-<hash>.json`) and its key structure. Branched `plan/fleet-mcp-oauth-provisioning`
  off `main`. Ran `plan-feature`: operator chose the two-phase direction + all three personas.
  Authored `README.md`, `requirements.md`, `plan.md`.
- **2026-07-22 (spec-feature)** — Resumed the trace. Resolved open questions **empirically** against the
  live AWS fleet (`team-a` + `team-b`, 2026.6.5): **Q1** filename hash is *deterministic*
  (both agents share `linear-4cca6302a658efcc.json`) → store/restore by exact filename; **Q2** blob
  carries a `refresh_token` (public PKCE client, ~24h access token) → refined into a **two-failure-mode**
  model (A: credential lost → restore fixes; B: token expired/revoked → only re-auth fixes); **Q4** no
  `openclaw mcp list --status` in 2026.6.5 → detection is a **log-scan**. Discovered a data-safety tension
  (blob lives inside the data dir vs. architecture.md "refresh never touches data" MUST) → resolved with
  **cold-only non-destructive restore**. Authored `spec.md`. Side-finding (out of scope): `team-b`
  GitHub MCP uses a static `github_pat_…` embedded in `custom.json`, not OAuth — flagged for a separate
  remediation ticket.

## Persona Review — Specification (all APPROVE)

- **Architect** — APPROVE. `common`-package capture/restore with provider closures fits "shared logic in
  common; providers are transport-only" (architecture principle 2); `Runtime.OAuthStateDir()` is the
  right OpenClaw/Hermes seam. **Must-address in impl**: the `mcp-oauth/` prefix-skip means `ListSecrets`
  now returns non-env entries — audit every `GenerateEnvFile`/`SecretNameToEnvVar` caller so a blob can
  never leak into the env file (`spec.md` §2.2). Elevated to an implementation gate.
- **QA** — APPROVE (1 amendment folded → `spec.md` §4.4). Two-failure-mode honesty + cold-only/
  warm-untouched data-persistence tests are sound. Amendment: capture cadence is operator-driven
  (`conga refresh`), so a low-traffic agent's persisted blob can go stale if the provider rotates
  refresh tokens → bounded, detected residual risk; documented, mitigations deferred.
- **Product Manager** — APPROVE (1 amendment folded → `requirements.md` criterion 7). Phase 1 ships
  independently (immediate fleet relief); GitHub-PAT finding correctly carved out. Amendment: criterion 7
  reworded to scope "survives lifecycle events" to Mode A, so we don't fail our own criterion on Mode B.

**Synthesis: no blocker. Proceed.**

## Standards Gate Report (pre-implementation)

| Standard | Scope | Severity | Verdict |
|---|---|---|---|
| security.md §5 — secrets protected at rest | secrets | must | ✅ PASSES — blob → SM (AWS, encrypted) / 0400 file (local·remote); restored 0600 uid 1000 on encrypted EBS; never in TF state; never logged; `mcp-oauth/` excluded from env-file gen |
| security.md §5 — "inject via env, not config file" (Issue #9627) | secrets | must | ℹ️ NOTE — deliberate documented deviation: OpenClaw reads this credential from a *file*, no env mechanism exists; a 0600 data-dir file is the only option and does not reintroduce `${VAR}`-in-`openclaw.json` (§7) |
| security.md §2 / reserved-key guard | config | must | ✅ N/A — feature touches neither `channels`/`gateway`/`plugins`/`$include` nor the managed root |
| architecture.md — Agent Data Safety | lifecycle | must | ✅ PASSES — restore is cold-only/non-destructive, capture is read-only; only `mcp-oauth/*.json` touched, only when absent; data-persistence test required (§6) |
| architecture.md — Interface Parity | cli | must | ✅ PASSES — `mcp login` + `doctor` specified across CLI + JSON + MCP (§3.1–3.2) |
| architecture.md — provider contract / shared logic | arch | must | ✅ PASSES — `CaptureMCPOAuth`/`RestoreMCPOAuth` in `common`; providers supply transport only |
| architecture.md — CLI Conventions | cli | should | ✅ PASSES — cobra `RunE`+`init()`, `resolveAgentName`, `%w` error wrapping (patterned on `secrets.go`/`refresh.go`) |
| config-taxonomy.md — credential → secrets store | config | must | ✅ PASSES — decision-rule step 5; taxonomy Secrets row already lists "OAuth tokens" |
| egress-controls.md | network | should | ℹ️ NOTE — no egress change; `doctor` noting egress gaps for OAuth hosts is a documented future option |
| agent-cost-and-runtime-config.md | runtime | may | ℹ️ NOTE — a future periodic-capture mitigation (§4.4) would incur heartbeat/cron cost; consult this standard if pursued |

**Gate decision: PASS** (no `must` violation). One tracked implementation gate: env-file/`SecretNameToEnvVar`
caller audit (Architect). No philosophies dir present.

## Handoff
Spec complete + gate PASS. Next: `/glados:implement-feature` (slice order S1→S7 in `spec.md` §9;
`pkg/`-touching slices S1/S4/S5 require a `terraform-provider-conga` release per
`reference_provider_release_flow`).

## Implementation (implement-feature)

- **2026-07-22 (implement-feature)** — Resumed trace. Interim ops during planning (not feature code):
  manually re-authed Linear OAuth on `team-a` and `team-b` (both were in the Mode-B
  expired-token state this feature will detect + one-command-fix). Created `tasks.md` from spec §9
  slices; operator reviewed the breakdown and chose **Phase 1 only** (detect + re-auth; no provider
  release). Implemented + verified live.

### Phase 1 delivered (Phase 2 = S1/S4/S5/S7 deferred)

**New files**
- `internal/mcpoauth/mcpoauth.go` — pure shared helpers: `ParseAuthorizeURL`, `NormalizeCode`,
  `DetectOAuthServer`, `ScanOAuthNeeds` (+ `OAuthNeed{Server,LastSeen}`). No provider/transport code.
- `internal/mcpoauth/mcpoauth_test.go` — table tests for all four helpers.
- `internal/cmd/mcp_login.go` — `conga mcp login [server]` (leg-1 URL / leg-2 `--code` / interactive;
  idempotent already-authed path; JSON output).
- `internal/cmd/doctor.go` — `conga doctor` fleet/`--agent` OAuth log-scan; text (non-zero exit) + JSON.
- `internal/mcpserver/tools_mcp_oauth.go` — MCP tools `conga_mcp_login`, `conga_doctor`.

**Modified**
- `internal/mcpserver/tools.go` — register the two new tools.
- `internal/cmd/json_schema.go` — `mcp.login` + `doctor` schema entries.
- `CLAUDE.md` — new "Remote-MCP OAuth Credentials" section.
- `product-knowledge/standards/config-taxonomy.md` — Secrets-row note on the unmanaged `mcp-oauth/` blob.

**Design choices worth noting**
- Phase 1 needs **no `pkg/` change** → **no provider release** (rides on existing `ContainerExec` /
  `GetLogs` / `ListAgents`). Shared logic isolated in `internal/mcpoauth` for DRY CLI+MCP reuse + tests.
- Live testing surfaced a real **log-window false-positive**: `doctor` flags a credential that was
  *just* re-authed because stale errors linger in the window. Mitigation (no health API exists in
  2026.6.5 — Q4): report each finding's **last-error timestamp** + a note that a more-recent re-auth is
  already fixed. Recorded as a known limitation; Phase 2 can cross-check blob mtime for certainty.

**Verification**: `go build/vet/test ./...` + `gofmt` all green; live on AWS — `doctor` (fleet + agent,
text + JSON) and `mcp login` (auto-detect + leg-1 + idempotent) confirmed.

## Handoff (implementation)
Phase 1 ready to ship (internal-only, no provider release). Next: `/glados:verify-feature` for Phase 1,
or `/glados:implement-feature` again to build Phase 2 (S1/S4/S5/S7 — persist/restore, which *does* need
a `terraform-provider-conga` release).

---

## Phase 1 shipped — PR #74 + #75 (2026-07-22)

- **PR #74** — Phase 1 (`conga doctor` + `conga mcp login`, CLI+JSON+MCP) + the new **confidentiality
  MUST standard** (`product-knowledge/standards/confidentiality.md`, CLAUDE.md, security.md principle).
  Independent code review: no correctness bugs; fixed a finding-shape Interface-Parity gap (unified the
  `doctor` finding via `mcpoauth.BuildFinding`) and documented the accepted single-use-auth-code-in-SSM
  note. Merged (admin, CI green).
- **PR #75** — repo-wide scrub of client/deployment/person names + the `Owner:` template leak +
  operator paths, to generic placeholders (`team-a`/`user-a`/`<operator>`), per the new standard.
  Merged. Tracked follow-up issue #76 for the ~36-file `aaron/zach/nathan` example-name tail.

> **Confidentiality breach + fix (2026-07-22):** real client/agent names
> leaked into the PR body, commit messages, and committed spec files before the operator caught it.
> Remediated: branch history rewritten, PR text scrubbed, files scrubbed, and the MUST standard added +
> saved as a durable feedback memory. All example names in this spec are now placeholders.

## Phase 2 shipped — PR #77 + #78 (2026-07-23)

- **PR #77** — Phase 2 core + **local** wiring: `Runtime.OAuthStateDir()`, `mcp-oauth/` secret prefix +
  env-file exclusion (S1); `common.CaptureMCPOAuth` wired into `conga mcp login` (S4, all providers);
  `common.RestoreMCPOAuth` cold-only (S5 core) + local `RefreshAgent` wiring. New integration test
  `TestMCPOAuthRestoreOnRefresh` (`-tags integration`). Review: clean; confirmed no blob can reach the
  env file on any provider. **CI caught a real non-root-Linux ownership bug** (blob `0600` unreadable by
  uid 1000 when the host can't chown) → fixed to `0644`/dir `0755` matching the local provider's
  container-read data-dir convention.
- **PR #78** — **remote + AWS** restore wiring (S5): remote over SSH (`test -f` + `ssh.Upload` 0644),
  AWS over SSM (`test -f` + `uploadFile` 0600, chown→1000 on encrypted EBS). New remote integration test.
  Review found a **critical AWS bug**: the existence probe read `err`, but `awsutil.RunCommand` returns
  `(result, nil)` for both Success and Failed SSM statuses → `exists()` always true → AWS restore was a
  silent no-op. Fixed to read stdout via a marker (`mcpFileExists`) + unit test `TestMCPFileExists`.
  A second CI round exposed the cold-only test fighting file ownership (opposite between local/remote/
  cap-dropped container) → rewrote it to prove cold-only by **changing the stored secret** instead of
  writing the file. CI green; merged.

**Per-provider mode (as-built, resolves spec §7):** the restored blob is `0600` root→uid-1000 on the
managed hosts (AWS/remote run as root / SSH-root; encrypted EBS) but `0644` on **local** — matching how
the local provider already writes every container-read data-dir file (`openclaw.json` + the `$include`
layers are 0644), because a non-root local host can't chown to uid 1000. The blob's confidentiality is
bounded by the agent's private data dir on local (dev/personal; disk security is the operator's), and by
Secrets Manager + encrypted EBS on AWS.

## Release — PR #79 (2026-07-23)

congaline tagged **`v0.0.32`** → `terraform-provider-conga` go.mod bumped → CI green → tagged **`v0.1.10`**
→ GoReleaser published the signed GitHub release → **Terraform Registry has `0.1.10`**. Pin bumped
`0.1.9 → 0.1.10` in `terraform/environments/production/main.tf` + `terraform/modules/congaline/main.tf`.
Per `reference_provider_release_flow`.

## Live verification — AWS `team-b` (2026-07-23)

End-to-end recovery test with the freshly-built binary against the real fleet (validates the AWS restore
path, which has no CI integration test):
1. **Capture** — `conga mcp login linear --agent team-b` → "backed up 2 MCP OAuth credential(s)";
   `mcp-oauth/linear-…` + `mcp-oauth/github-…` now in Secrets Manager.
2. **Simulated loss** — deleted the on-disk `linear` blob (baseline sha256 `fa4cf233…ad37f`).
3. **Restore** — `conga refresh --agent team-b` re-materialized it from Secrets Manager,
   **byte-identical** (sha256 matched), `0600` `node`-owned.
4. **Cold-only** — the present `github` blob was left untouched.
5. **Health** — clean container startup, no `requires OAuth`, `conga doctor` exit 0.

## Deferred (optional, tracked)

- **Re-capture on refresh** (§4.4 freshness) — capture currently runs on `conga mcp login`; a stored
  blob can go stale between logins if the provider rotates refresh tokens.
- **`ListSecrets` blob-display polish** (PR #77 review finding #3) — `mcp-oauth/` secrets show
  inconsistently across providers (display-only, no leak).
- **`team-b` GitHub PAT → OAuth migration** (`spec.md` appendix) — its GitHub MCP still uses a
  static PAT embedded in `custom.json`, not OAuth.
- **Example-name scrub** (issue #76) — `aaron/zach/nathan` as example names across ~36 other files.
