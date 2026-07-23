# Implementation Tasks — Remote-MCP OAuth Credential Lifecycle

Derived from `spec.md` §9. Order: **S1→S2→S3** (Phase 1, shippable) → **S4→S5** (Phase 2) → **S6→S7**.
Legend: `[ ]` todo · `[~]` in progress · `[x]` done. Files are the expected touch-points (confirm on read).

> **Session scope (2026-07-22): Phase 1 only** — `conga mcp login` + `conga doctor` + docs, `internal/`
> only, **no provider release**. Phase 1 rides on existing Provider methods (`ContainerExec`, `GetLogs`,
> `ListAgents`), so **S1 is NOT needed for it** and is deferred with Phase 2 (S1/S4/S5/S7).
> Shared pure logic landed in a new `internal/mcpoauth` package (URL parse, code normalize, server
> detect, log scan) — unit-tested and reused by both the CLI and the MCP server.

> **Phase 2 progress (2026-07-23, branch `feat/mcp-oauth-persist-restore`, PR #77):** S1 ✅, S4 ✅
> (capture, all providers via `conga mcp login`), S5 core ✅ + **local** restore wiring ✅. Prefix const +
> `IsMCPOAuthSecret` live in `pkg/runtime` (not `pkg/common`) since common→runtime is the import
> direction. **Remaining:** S5 remote/AWS restore wiring, re-capture on refresh (§4.4), S7 release.

---

## S1 — `Runtime.OAuthStateDir()` + secret-prefix plumbing  *(pkg/ → provider release)* ✅
- [x] S1.1 `OAuthStateDir() string` on the `Runtime` interface (`pkg/runtime/runtime.go`).
- [x] S1.2 OpenClaw → `"mcp-oauth"`; Hermes → `""`.
- [x] S1.3 `runtime.MCPOAuthSecretPrefix` + `runtime.IsMCPOAuthSecret`; both runtimes' `GenerateEnvFile`
  skip the prefix (blob can never become an env var). Placed in `pkg/runtime` (common imports runtime).
- [x] S1.4 Unit tests: `OAuthStateDir` per runtime; `IsMCPOAuthSecret`; env-file exclusion (blob value absent).

## S2 — `conga mcp login [server] --agent <name>`  *(CLI + JSON + MCP — Interface Parity MUST)* ✅
- [x] S2.1 `loginCmd` on existing `mcpCmd` (`internal/cmd/mcp_login.go`); pattern from `secrets.go`.
  `[server]` positional optional; auto-detects the sole OAuth server via `openclaw mcp list --json`.
- [x] S2.2 Leg 1 (start): parse + print authorize URL. Idempotent already-authed case (no URL) reports
  `authenticated` rather than a false pending state.
- [x] S2.3 Leg 2 (complete): `--code` or interactive prompt; `mcpoauth.NormalizeCode` is forgiving
  (percent-encoded, `code=`/whole-URL/trailing-`&state`).
- [x] S2.4 JSON I/O: leg 1 → `{authorize_url,status,next}` or `{status:"authenticated"}`; leg 2 →
  `{status:"authenticated"}`; `conga json-schema mcp.login`.
- [x] S2.5 MCP tool `conga_mcp_login {agent_name,server?,code?}` (`internal/mcpserver/tools_mcp_oauth.go`).
- [x] S2.6 Tests (`internal/mcpoauth`): URL parse, code normalize, server-detect (single/none/ambiguous/
  unparseable). Live-verified: auto-detect + leg-1 + idempotent path on `team-a` (AWS).

## S3 — `conga doctor` fleet OAuth health  *(CLI + JSON + MCP)* ✅
- [x] S3.1 `internal/cmd/doctor.go` (pattern: `refresh.go`); `--agent` scope (global flag), `--lines` override.
- [x] S3.2 Log-scan (`mcpoauth.ScanOAuthNeeds`): `ListAgents` → `GetLogs` → regex capturing `server` +
  the timestamp of the latest occurrence per server.
- [x] S3.3 Output names the exact `conga mcp login <server> --agent <name>` fix; non-zero exit if unhealthy
  (text); JSON exit 0 with data.
- [x] S3.4 JSON shape (`spec.md` §3.2); MCP tool `conga_doctor {agent_name?,lines?}` beside `toolGetLogs`.
- [x] S3.5 Help + report document the "clean in last N ≠ positive health" caveat; report shows last-error
  timestamp + a note that a more-recent re-auth is already fixed (mitigates the log-window false-positive
  found in live testing).
- [x] S3.6 Tests (`internal/mcpoauth`): regex positive/negative/multi-server/no-timestamp/latest-wins.
  Live-verified: fleet + `--agent` scope, text + JSON, on the AWS host.

## S6 — Docs ✅
- [x] S6.1 CLAUDE.md: new "Remote-MCP OAuth Credentials" section (failure mode + `doctor`/`mcp login` recovery).
- [x] S6.2 `config-taxonomy.md`: note on the per-container `mcp-oauth/` blob (currently unmanaged) + recovery + Phase 2 pointer.
- [x] S6.3 `conga json-schema` entries for `mcp.login` + `doctor`.

## S4/S5/S7 — Phase 2 (persist/restore) + release  *(DEFERRED → next session)*
- [ ] S4 `common.CaptureMCPOAuth` (blob → secret) + wire into `mcp login` success & `RefreshAgent`.
- [ ] S5 `common.RestoreMCPOAuth` (cold-only) + wire pre-container-start on all three providers.
- [ ] S7.1 Data-persistence integration test (cold-restore byte-identical; warm refresh leaves on-disk untouched).
- [ ] S7.4 `terraform-provider-conga` release (S1/S4/S5 touch `pkg/`) per `reference_provider_release_flow`.

---

### Phase 1 verification (this session)
- `go build ./...`, `go vet ./...`, `gofmt -l internal/` clean; `go test ./...` all pass.
- Live on AWS (`--provider aws --profile openclaw`): `conga doctor` (fleet + `--agent`, text + JSON) and
  `conga mcp login` (auto-detect + leg-1 + idempotent) both verified.
- **Known limitation (documented)**: `doctor`'s log-scan can't distinguish a just-fixed credential from a
  live failure until the stale error ages out of the window; the last-error timestamp + note make it
  honest. Phase 2 can cross-check the on-disk blob mtime for a definitive signal.

### Cross-cutting reminders (Phase 2)
- **Data Safety (MUST)**: restore cold-only/non-destructive; capture read-only; only `mcp-oauth/*.json` touched, only when absent.
- **Security (MUST)**: blobs never logged, never in TF state; restored 0600 uid 1000; `mcp-oauth/` excluded from env file.
- **Not in this feature**: GitHub PAT→OAuth migration for `team-b` (post-feature TODO, `spec.md` appendix).
