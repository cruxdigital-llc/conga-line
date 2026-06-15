# Feature: Agent Device-Token Provisioning

**Trace Log** — GLaDOS `plan-feature` workflow

- **Created**: 2026-06-14
- **Owner**: Aaron Stone
- **Status**: Planning (requirements + high-level plan)
- **Spec dir**: `specs/2026-06-14_feature_agent-device-token-provisioning/`
- **Parent / restores**: `specs/2026-05-22_feature_delegation-routing/` (the subagents/delegation feature this re-enables for chat channels)
- **Origin analysis**: `audit/openclaw-channel-subagent-spawn-regression.md`

## One-line

Provision an operator **device identity / device-token per agent** so the agent runtime's in-process
`sessions_spawn` authenticates on the OpenClaw-preserved device-token path — restoring subagent
delegation (e.g. to a cheaper Qwen model) **from Slack/chat channels**, securely, after OpenClaw's
2026.6.x security fix removed the shared-token backend self-pairing bypass we were implicitly relying on.

## Why now (problem)

`sessions_spawn` from a chat channel fails with `missing scope: operator.write` on our pinned
**v2026.6.5**; it worked on **v2026.5.26** (operator-confirmed: a real Qwen subagent ran from Slack on
2026-05-27), and still works from operator surfaces (CLI / Control UI / `conga connect`).

This is **not a regression to file** — it's a deliberate OpenClaw security fix for a critical
(CVSS 9.3) local privilege-escalation vuln:

| Upstream | What | Shipped |
|---|---|---|
| openclaw/openclaw **#72418** | backend self-pairing lets any loopback process self-declare `GATEWAY_CLIENT` + shared token to gain privileged scopes (CVSS 9.3) | OPEN (P1) |
| **#86192** | clear admin scopes for backend self-pairing; keep `approvalRuntimeToken` device-less | merged 2026-05-27 |
| **#90188** | require pairing for shared-token backend clients; **keep the device-token path** + `auth.mode:"none"`. Closes #72418 | merged 2026-06-04 |

**Conga gap:** we provision with `gateway.auth.mode:"token"` and never run OpenClaw's device-pairing
flow, so the runtime has no device identity and fell back to the now-closed shared-token bypass. The fix
preserved a **device-token** path (`operator-scopes.md`: *"device pairing records are the durable source
of approved roles and scopes"*) — we should adopt it.

## Active Personas

- **Architect** — how a device identity/token is minted, stored, injected, and reconciled through the
  managed-host engine; provider parity (AWS/remote/local); no new persisted-config locus sprawl.
- **QA** — idempotency across refresh + the C5b reboot model; the live acceptance (spawn-from-Slack
  works); failure modes (gateway-not-running-at-pair-time chicken-and-egg; pairing drift).
- **Product Manager** — scope discipline (restore one capability, not re-architect auth); who needs it
  (channel-driven delegation to cheaper models); measurable success.
- **Security (cross-cutting lens)** — *no dedicated persona file exists*; applied via
  `product-knowledge/standards/security.md` + an explicit security review in the Architect/QA gates.
  **Non-negotiable: must NOT re-introduce the #72418 bypass.** The device-token is a credential and must
  follow our secrets posture (never inline; protected at rest; least privilege).

## Active Capabilities

- **conga MCP / AWS SSM** — live fleet introspection + provisioning + (operator-in-the-loop) Slack
  acceptance test on the AWS host.
- **OpenClaw source/docs** — the deployed **v2026.6.5** image (`/app/docs`, bundled `dist`) is the
  authoritative reference; **verify mechanisms against 2026.6.5, not an older clone** (lesson from the
  diagnosis: a 2026.3.14 clone gave wrong conclusions).
- **GitHub (`gh`)** — upstream issue/PR references (#72418/#86192/#90188); two-repo provider-release flow
  if `pkg/` changes.

## Key Decisions (this phase)

1. **Forward-fix, not rollback.** We upgraded to 2026.6.5 deliberately (native MCP OAuth); the security
   fix is correct. We adopt the preserved device-token path rather than downgrade or weaken auth.
2. **Don't file upstream.** Known + intentional (#72418). Tracked here as a Conga feature.
3. **Security is the gating concern**, not an afterthought — the whole point is to restore delegation
   *without* re-opening the patched vulnerability.

## Files Created

- [requirements.md](./requirements.md)
- [plan.md](./plan.md)

## Session Log

- **2026-06-14** — `/glados:plan-feature`. Feature created from the channel-subagent-spawn investigation
  (audit/openclaw-channel-subagent-spawn-regression.md). Confirmed root cause = OpenClaw security fix
  #72418 (not a regression); Conga gap = no device pairing. Personas: Architect, QA, PM + Security
  (cross-cutting, no dedicated persona file). Drafted requirements.md + plan.md. Next:
  `/glados:spec-feature` — the spec must first **verify the exact pairing/token mechanism against the
  deployed v2026.6.5** before committing to an implementation shape (7 open questions in requirements).

- **2026-06-14** — `/glados:spec-feature` started. Per plan.md, doing **Phase 0 mechanism discovery against the deployed v2026.6.5** before writing spec.md (the chosen mechanism — device-pairing vs device-token vs `approvalRuntimeToken` — determines the design). Findings recorded below.

## Phase 0 mechanism findings (verified on deployed v2026.6.5)

1. **Headless device-token CLI** exists: `openclaw devices rotate --role --scope --token --url --json` (mint/rotate, gateway-token-authed, no interactive step) + `devices {list,approve,reject,remove,revoke,clear}`.
2. **A paired operator device already exists** (`clientId: openclaw-control-ui`, `operator.admin`) — that's why operator-surface spawn works; the runtime's own in-process call doesn't use it.
3. **`approvalRuntimeToken`** is a real internal module (`operator-approval-runtime-token-*.js`) — the device-less path #86192 preserved.
4. **Obstacle:** `shouldOmitDeviceIdentityForGatewayCall` (identical 5.26↔6.5) omits the runtime's device identity for backend/gateway-client loopback calls **when shared auth is present** → device-less + shared-token → scopes cleared (post-#90188). So a minted device-token may never be consumed by the spawn call → **the native path is likely `approvalRuntimeToken`, which must be pinned by a spike before building.**

## Session Log (spec phase)

- **2026-06-14** — `/glados:spec-feature`. Did Phase 0 discovery on live v2026.6.5 (above). Drafted [spec.md](./spec.md): because Phase 0 surfaced a real obstacle (device-identity omission under shared auth), the spec **mandates a mechanism spike (§3, Task 0) on a live Slack-bound agent to pick the path — Candidate A `approvalRuntimeToken` (preferred, likely config-only, no new secret), B device-token + suppress shared-auth on self-call, or C document+upstream-ask — before any provisioning code.** Persona review + standards gate below.

## Spec Review — Personas (all APPROVE; spike is the gate)

- **Architect — APPROVE.** Correctly defers to the mechanism spike rather than building on an unverified path; managed-host integration gives AWS/remote parity. **Flag:** Candidate A may be a `pkg/runtime/openclaw` config change (no per-agent secret, no managed-host credential plumbing) — materially smaller; confirm in the spike and prefer it if viable.
- **QA — APPROVE.** Spike exit criterion (live Slack spawn succeeds) is the right acceptance; R3 idempotency + reboot durability required. **Flag:** the spike must also confirm it does NOT break operator-surface spawn, and verification is operator-in-the-loop (no automated harness — consistent with the no-AWS-CI decision).
- **Product Manager — APPROVE.** Tight scope (restore one capability), measurable success. **Flag:** if A is a small config fix, the feature shrinks well below "device-token provisioning" — keep scope flexible pending the spike; don't over-build.
- **Security (cross-cutting) — APPROVE, with a blocking post-spike review.** R5 correctly gating: no #72418 re-open, no auth broadening, least privilege (`operator.write` not admin), credential-at-rest if B. Candidate A is preferable (no new persisted secret). **The spike must run on a held/controlled agent and revert** so experimentation doesn't weaken the live fleet.

## Standards Gate (pre-implementation) — PASS (security verdict conditionally gated on the spike)

| Standard | Severity | Verdict |
|---|---|---|
| Agent Data Safety (architecture.md) | must | ✅ PASSES — credential/config only; no data-dir deletion |
| Interface Parity (architecture.md) | must | ✅ PASSES — no new user surface; secret (if B) follows existing model |
| Module Structure / boundaries (architecture.md) | must | ✅ PASSES — `pkg/runtime/openclaw` (A) or `pkg/provider/managedhost` (B); `pkg/`→provider release noted |
| Provider contract (architecture.md) | must | ✅ PASSES — no interface change expected |
| Egress fail-closed (egress-controls.md) | must | ✅ PASSES — unchanged (R5 forbids weakening) |
| Immutable config / perms (security.md P2) | must | ✅ PASSES — root:root 0444; credential 0400/Secrets Manager |
| Secrets via env / #9627 (security.md) | must | ✅ PASSES — credential never inline (R2/R5) |
| Own the box, not behavior (security.md P8) | must | ✅ PASSES — auth/infra provisioning |
| Config taxonomy (config-taxonomy.md) | should | ✅ PASSES — no new locus if A; secrets store if B |
| Security posture / #72418 boundary (security.md) | must | ⚠️ **CONDITIONAL** — intent is to *strengthen* (delegation without the bypass), but the actual verdict can't be finalized until the spike picks the mechanism. **Blocking security review required post-spike, pre-rollout** (already mandated in spec §3/§5). |

**Philosophy cross-check:** aligns with "logic in tested Go behind thin seams," "own the box," and
secure-by-default. No conflicts.

**Gate decision: PASS (pre-implementation).** All `must` standards pass at the spec level; the sole
caveat is the #72418-boundary security verdict is **conditionally gated on the spike + a blocking
security review before rollout** — which the spec already requires. Cleared to enter
`/glados:implement-feature` **starting at Task 0 (the mechanism spike)**.
