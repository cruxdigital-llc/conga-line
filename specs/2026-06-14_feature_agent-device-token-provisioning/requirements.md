# Requirements — Agent Device-Token Provisioning

- **Created**: 2026-06-14
- **Owner**: Aaron Stone
- **Status**: Planning
- **Origin**: `audit/openclaw-channel-subagent-spawn-regression.md`; restores `specs/2026-05-22_feature_delegation-routing/`

## Goal

Restore the ability for an agent to **spawn a subagent (`sessions_spawn`) from a chat channel** (Slack),
so channel-driven delegation to a cheaper model (e.g. Qwen) works again — using the **upstream-sanctioned
device-token auth path** that OpenClaw's 2026.6.x security fix (#72418) preserved, **without**
re-introducing the shared-token backend self-pairing bypass that fix removed.

## Functional requirements

### R1 — Provision an operator device identity/token per agent (HIGH)
- During agent provisioning (`add-user`/`add-team`) and `refresh`, Conga MUST establish a **device
  identity / operator device-token** for the agent's runtime such that the in-process
  `callGateway({method:"agent"})` (which `sessions_spawn` issues) authenticates on a **preserved**
  device-less-eligible path (`device-token` per #90188; or `approvalRuntimeToken`; **not** the removed
  shared-token bypass).
- **Success:** a `sessions_spawn` triggered from a Slack message succeeds (no `missing scope:
  operator.write`).

### R2 — The device credential follows our secrets posture (HIGH, security)
- Any device-token/identity material MUST be stored and injected like other secrets: never inline in
  config (`--env-file`/secrets store), protected at rest (AWS Secrets Manager / SSM / file `0400`,
  per provider), and scoped to least privilege needed for spawn (NOT blanket `operator.admin` unless the
  mechanism requires it — to be determined; prefer `operator.write`).
- **Success:** no device token value is ever written to `openclaw.json` or printed; integrity/perms
  match the existing secrets model.

### R3 — Idempotent + durable across refresh and reboot (HIGH)
- Pairing/token provisioning MUST be idempotent (re-running provision/refresh does not duplicate or
  invalidate the identity) and MUST survive a host reboot / container restart (the parent feature's C5b
  unattended-reboot model) without manual re-pairing.
- **Success:** after `conga refresh` and after a host reboot, channel-spawn still works with no operator
  action.

### R4 — Provider coverage (MED)
- AWS (managed-host engine over SSM) and remote (SSH) MUST be covered. Local SHOULD be covered if the
  same mechanism applies. (Confirm in spec which providers actually need it — local may not hit the
  channel/loopback boundary the same way.)

### R5 — Must NOT re-open #72418 (HIGH, security — blocking)
- The implementation MUST NOT restore or emulate the removed shared-token backend self-pairing bypass,
  and MUST NOT broaden gateway auth (e.g. `auth.mode:"none"` on a non-loopback bind, or `*` origins).
  It must use the genuinely-preserved path. A security review against `security.md` + the #72418 boundary
  is a gate.

## Open questions (the spec MUST resolve these against the deployed v2026.6.5 — do not assume)

1. **Mechanism:** which preserved path is actually automatable headlessly — `openclaw pairing approve`
   (device-pair) vs minting/loading a `device-token` vs the runtime `approvalRuntimeToken`? Is there an
   interactive approval step, and can it be auto-approved on loopback (cf. `autoApproveCidrs`)?
2. **Storage/injection:** what artifact does the runtime need (a device identity file? a token env var?),
   where does it live, and how is it injected per provider?
3. **Chicken-and-egg:** does pairing require the gateway to be running first? If so, sequence within
   provision/refresh.
4. **Scope:** does the spawn need `operator.write` only, or more? Grant the minimum.
5. **Per-agent vs shared:** one device identity per agent (isolation) vs a shared runtime identity.
6. **Image support:** does **v2026.6.5** actually expose a headless pairing/token mechanism? Verify on
   the deployed image (not a clone).
7. **`approvalRuntimeToken` viability:** is the runtime's own internal token the simpler/native path
   (no pairing needed), and if so why isn't our runtime already using it? (If it "just works" once
   something is configured, that may be a smaller fix than full device pairing.)

## Success criteria (acceptance)

- **Primary (live):** after provisioning, an operator sends a Slack message asking the agent to delegate
  to a subagent → the agent **spawns a Qwen subagent** (no `missing scope` error) and returns the
  subagent's result. (Requires operator-in-the-loop; plan accordingly — a throwaway-agent harness proved
  fragile.)
- Operator-surface spawn (CLI/`conga connect`) continues to work (no regression).
- Idempotent across refresh + survives a reboot.
- Security review confirms #72418 is not re-opened.

## Constraints

- Pinned image `ghcr.io/openclaw/openclaw:2026.6.5`. AWS path = Go managed-host engine over SSM
  (`pkg/provider/managedhost` + `awsprovider`). `pkg/` change → `terraform-provider-conga` release.
- Secrets via `--env-file`/secrets store; root:root `0444` on managed files; egress unchanged.

## Non-goals

- Changing OpenClaw upstream or the gateway-auth model.
- Re-enabling delegation by weakening auth (shared-token bypass, `auth.mode:"none"` broadly, wildcard
  origins) — explicitly forbidden (R5).
- The Slack `operator.write`-from-channel question for *non-spawn* operator actions (this feature targets
  the subagent-spawn path specifically).
