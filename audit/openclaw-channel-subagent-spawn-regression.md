# Channel-originated subagent spawn blocked after OpenClaw 2026.6.x security fix (#72418)

> **Status: NOT a bug to file.** Originally drafted as an "unintended regression"; investigation of the
> upstream OpenClaw repo showed it is a **deliberate security fix** for a critical (CVSS 9.3) local
> privilege-escalation vulnerability. The real gap is on the **Conga** side: we never adopted the
> device-identity auth path the fix preserved. Tracking → a Conga feature (device-token provisioning),
> not an upstream issue.

## What operators see

A subagent spawn (`sessions_spawn`, used to delegate to a cheaper model like Qwen) triggered from a
**chat channel** (Slack via the HTTP-webhook plugin) fails with:

```
missing scope: operator.write
```

The same spawn **works from operator surfaces** (CLI, Control UI, `conga connect`). Worked on
**v2026.5.26**; broke after upgrading to **v2026.6.5** (2026-06-11).

## Root cause: intentional OpenClaw security fix, not a regression

The in-process spawn calls `callGateway({ method: "agent" })`, which requires `operator.write`. Pre-fix,
that connection kept its scope via a backend "self-pairing" exemption for **shared-token** loopback
clients. OpenClaw **removed that exemption on purpose**:

| Upstream | What | State / shipped |
|---|---|---|
| **#72418** | `shouldSkipBackendSelfPairing` lets any loopback process self-declare `GATEWAY_CLIENT` identity + shared token to bypass device pairing and gain privileged scopes. **CVSS 9.3 (critical)** local priv-esc / SSRF-to-loopback. | OPEN (P1, security) |
| **#86192** | `fix(gateway): clear admin scopes for backend self-pairing` — *stop treating shared token/password as sufficient to preserve self-declared operator scopes; keep `approvalRuntimeToken` calls device-less; ordinary shared-token backend calls become device-bound.* | merged **2026-05-27** |
| **#90188** | `fix(gateway): require pairing for shared-token backend clients` — *remove the shared-token backend self-pairing bypass; **keep the device-token path and `auth.mode:"none"`**.* **Closes #72418.** | merged **2026-06-04** |
| **#90306** | `backend mode returned empty scopes and missing scope` — ≈ this exact symptom. | CLOSED/COMPLETED |

**Timeline matches our findings exactly:** 5.26 (pre-fix) worked; the fixes merged 2026-05-27 (#86192)
and 2026-06-04 (#90188), shipping in the 6.x line; 6.5 (post-fix) fails. This is also why our static
diff found the gate/exemption **function bodies** byte-identical (5.26↔6.5) yet the behavior changed —
the change is in the **auth eligibility feeding** the exemption (the `call.ts` +103 @5.28 ≈ #86192; the
6.1 change ≈ #90188), not in the gate functions themselves.

## Why Conga specifically hit it

Conga provisions agents with `gateway.auth.mode: "token"` and **never runs OpenClaw's device-pairing
flow.** So our agent runtime has **no device identity** — its in-process spawn fell back to the
shared-token backend bypass that #72418 closed. Operator surfaces still work because they authenticate
as proper operator clients. A stock OpenClaw user pairs a device during onboarding and stays on the
device-token path, so delegation keeps working for them.

## The fix is on our side (upstream-sanctioned path)

Both fixes **deliberately preserved** legitimate routes for in-process backend calls:
`approvalRuntimeToken`, a paired operator **device-token**, and `auth.mode: "none"`. The durable,
operator-facing one (per `docs/gateway/operator-scopes.md`: *"device pairing records are the durable
source of approved roles and scopes"*) is the **device-token**.

➡️ **Conga should provision an operator device pairing / device-token per agent** so the runtime's
in-process spawn authenticates on the preserved device-token path → keeps `operator.write` → restores
subagent delegation from Slack/channels, **securely** (no shared-secret bypass). Scoped as a GLaDOS
feature (`specs/…_feature_agent-device-token-provisioning/` — see PROJECT_STATUS).

## Do NOT file upstream

This is known and intentional (#72418 + merged fixes). Filing a new issue would be a duplicate and would
mischaracterize a security fix as a regression. If we want to track upstream, reference #72418 / #90188.

## How we got here (method note)

Confirmed empirically (operator anchors: worked 5.26 / broke 6.5) + static source diff (gate logic
unchanged; auth eligibility changed at 5.28 + 6.1) + the upstream issue search above. An empirical
throwaway-agent bisect was attempted but couldn't produce a clean signal (cross-version channel-plugin
incompatibility on older tags; orchestrator not reliably invoking `sessions_spawn` in the harness), so
the conclusion rests on the upstream issues + the static/anchor evidence — which is now decisive.
