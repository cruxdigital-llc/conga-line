# Technical Specification — Agent Device-Token Provisioning

- **Created**: 2026-06-14
- **Status**: Specified (pre-implementation) — **mechanism spike required before build (see §3)**
- **Reads first**: `requirements.md`, `plan.md`, `audit/openclaw-channel-subagent-spawn-regression.md`
- **Restores**: `specs/2026-05-22_feature_delegation-routing/` for chat channels

## 1. Summary

Restore channel-originated `sessions_spawn` (delegation to a cheaper model) by putting the agent
runtime's in-process gateway call onto an OpenClaw-**preserved** auth path, after the 2026.6.x security
fix (#72418) removed the shared-token backend self-pairing bypass we relied on. Phase 0 discovery against
the deployed **v2026.6.5** shows the design is subtler than "provision a device-token," so this spec
**mandates a focused mechanism spike (§3) before committing to an implementation**.

## 2. Phase 0 findings (verified on the deployed v2026.6.5)

1. **Headless device-token CLI exists:**
   `openclaw devices rotate --role <role> --scope <scope…> [--device <id>] --token <gw-token> --url <ws> --json`
   mints/rotates a role-scoped device-token (authed by the gateway token; no interactive step).
   `openclaw devices {list,approve,reject,remove,revoke,clear}` manage pairings/tokens.
2. **A paired operator device already exists** (e.g. on `aaron`): `clientId: openclaw-control-ui`,
   `clientMode: webchat`, `role: operator`, `scopes: [operator.admin, …]`, from the host IP. **This is
   why operator-surface spawn works** (Control UI / `conga connect` ride this device's token). The agent
   **runtime's own in-process call does not use it.**
3. **`approvalRuntimeToken` is a real internal mechanism** — dedicated module
   `operator-approval-runtime-token-*.js`, referenced in `call`/`gateway`/`message-handler`. Per #86192
   it is the **device-less path that remains allowed** for "approval-runtime scoped backend Gateway
   calls."
4. **The obstacle (key):** `shouldOmitDeviceIdentityForGatewayCall` (confirmed identical 5.26↔6.5) returns
   true for `mode == BACKEND && clientName == GATEWAY_CLIENT && hasSharedAuth && loopback` — i.e. the
   runtime's in-process self-call **omits its device identity whenever the shared gateway token is
   present** (which it always is, from our config). Post-#90188, a device-less shared-token backend
   connection has its scopes cleared. **Therefore a minted device-token alone may never be consumed by
   the spawn call** — the runtime would have to either (a) carry the `approvalRuntimeToken`, or (b) make
   the self-call *without* shared auth so its device identity is used.

## 3. Mechanism spike — REQUIRED before implementation (Task 0)

Pick the real path on a **live, Slack-bound agent** (the throwaway harness proved unreliable; use a
controlled test on a held/real agent with operator-in-the-loop for the Slack message). Resolve:

- **Candidate A — `approvalRuntimeToken` (preferred if viable):** determine how the runtime obtains/uses
  it, and why our runtime's spawn isn't already on it. If it's a config/wiring gap, the fix may be small
  (no per-agent credential to manage) and is the most upstream-native path. Investigate
  `operator-approval-runtime-token-*.js` + how `callGateway` selects it vs the shared token.
- **Candidate B — device-token + suppress shared auth on the self-call:** pair/mint an operator
  device-token (scope **`operator.write`**, not admin) for the runtime AND ensure the in-process spawn
  call presents the device-token rather than the shared token (so `shouldOmitDeviceIdentityForGatewayCall`
  doesn't omit it). Requires controlling how the runtime authenticates its self-call.
- **Candidate C — fallback:** if neither is feasible via config, document the limitation and the minimal
  upstream ask.

**Spike exit criterion:** a `sessions_spawn` from Slack succeeds on one agent, with the chosen mechanism,
no shared-token bypass, scope limited to `operator.write`. Record the as-built mechanism in the README.
**No provisioning code is written until the spike picks A, B, or C.**

## 4. Design (conditional on the spike outcome)

> Written as branches; the spike collapses this to one path.

**If A (`approvalRuntimeToken`):** likely a runtime/config change so the agent's in-process gateway calls
carry the approval-runtime token. Probably **no new per-agent secret** — smaller, provider-agnostic. Wire
in config generation (`pkg/runtime/openclaw`) / the managed-host engine.

**If B (device-token):** at provision/refresh, after the gateway is up, mint an `operator`/`operator.write`
device-token via `openclaw devices rotate` (authed with the gateway token, over loopback), store it in
the secrets store (AWS Secrets Manager / SSM; remote/local file `0400`) keyed per agent, and inject it so
the runtime uses it for self-calls (and stops sending shared auth on those calls). Persisted in the 6.x
device store (SQLite `device_identities`) → survives reboot (verify in spike).

**Shared to both:** integrate via `pkg/provider/managedhost` so AWS + remote share the path; idempotent
across refresh + reboot; `pkg/` change → `terraform-provider-conga` release.

## 5. Security (R5 — gating, must)

- **No re-opening #72418:** must not restore the shared-token backend self-pairing bypass, must not set
  `auth.mode:"none"` on a non-loopback bind, must not widen origins. Use only the genuinely-preserved path.
- **Least privilege:** the runtime credential gets **`operator.write`**, not `operator.admin`, unless the
  spike proves a narrower scope insufficient (then justify).
- **Credential at rest:** if B, the device-token is a secret — secrets store, `0400`/Secrets Manager,
  never in `openclaw.json`, never logged. If A, no new persisted secret (preferred on this axis).
- Security review against `security.md` + the #72418 boundary is a blocking gate before rollout.

## 6. Edge cases

- Gateway not yet running when minting (B) → sequence after gateway-ready in refresh.
- Token rotation/expiry → idempotent re-mint on refresh; `devices rotate` supports rotation.
- Reboot → credential/identity must persist (6.x SQLite device store; verify).
- Agents without a `subagents` config → no-op (only provision for agents that can delegate, or universally
  — decide in §4).
- Multiple paired devices / scope-upgrade pending entries → don't disturb the existing Control UI device.

## 7. Testing strategy

- **Spike (Task 0):** live single-agent Slack spawn succeeds with the chosen mechanism.
- **Unit:** config/credential generation (whichever path) in `pkg/`; idempotency.
- **Live acceptance (operator-in-the-loop):** provision on one agent → Slack delegation request → Qwen
  subagent actually spawns; operator-surface spawn still works; survives `refresh` + reboot.
- No automated Slack harness (proven fragile) — acceptance is operator-driven.

## 8. Interface parity

No new CLI/JSON/MCP user surface expected (internal provisioning behavior). If B introduces a per-agent
secret, it follows the existing secrets model (no new command). Confirm in §4 once the path is chosen.

## 9. Rollout / sequencing

Spike → implement chosen path → unit tests → provider release (if `pkg/`) → live-verify on one agent →
staged fleet rollout (managed-host discipline) → confirm refresh + reboot durability.

## 10. Open items carried to implementation

- The §3 spike outcome (A/B/C) — **blocks everything else**.
- Provider coverage decision (does local need it?).
- Universal vs subagents-only provisioning.
