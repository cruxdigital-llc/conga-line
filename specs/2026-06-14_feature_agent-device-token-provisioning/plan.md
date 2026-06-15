# High-Level Plan — Agent Device-Token Provisioning

- **Created**: 2026-06-14
- **Status**: Planning (approach only; detailed design in `/glados:spec-feature`)

## Approach in one paragraph

Adopt the OpenClaw-preserved device identity path so the agent runtime's in-process `sessions_spawn`
keeps `operator.write` without the removed shared-token bypass. Because the *exact* mechanism is
uncertain (3 candidate paths) and version-sensitive, the work is **investigation-first**: confirm the
real headless mechanism against the deployed **v2026.6.5**, pick the simplest secure one, then wire it
into the managed-host provisioning/refresh flow with the credential handled per our secrets model.

## Phase 0 — Mechanism discovery (gate before any code)

Resolve the requirements' 7 open questions against the **deployed v2026.6.5** (read `/app/docs`, the
bundled `dist`, and the `openclaw` CLI help inside a real container; cross-check upstream PRs
#86192/#90188). Specifically determine:
- Which preserved path is headless-automatable: **device pairing** (`openclaw pairing approve`, possibly
  with loopback auto-approve) vs **device-token mint/load** vs **`approvalRuntimeToken`**.
- The concrete artifact the runtime needs (identity file? token env var?), where it lives, what scope.
- Whether the gateway must be running to pair (sequencing).
- **Decision output:** one chosen mechanism + a 1-page "as-built on 2026.6.5" note. *If `approvalRuntimeToken`
  turns out to be the native path that merely needs enabling, the feature may shrink to a small config
  fix rather than full device provisioning — that's a good outcome; let the evidence decide.*

> Lesson logged: earlier diagnosis was misled by anchoring on a 2026.3.14 clone. Phase 0 verifies on the
> actual pinned image before committing.

## Phase 1 — Credential lifecycle (managed-host engine)

- Generate/obtain the device identity/token at provision time; store it in the secrets store
  (AWS Secrets Manager / SSM; remote/local file `0400`) keyed per agent.
- Inject it into the runtime the same way other secrets reach the container (`--env-file` / managed
  identity file with root:root `0444`), via `pkg/provider/managedhost` (so AWS + remote share the path).
- Idempotency: re-provision/refresh reuses the existing identity; reboot survives (no re-pair).

## Phase 2 — Provisioning + refresh wiring

- Hook into `add-user`/`add-team` + `refresh` so every agent (or those with `subagents` configured) gets
  the device identity. Decide per-agent vs shared (lean per-agent for isolation).
- Provider coverage: AWS + remote first; evaluate local.
- Handle the chicken-and-egg (pair after the gateway is up, or pre-mint a token that doesn't need a live
  gateway) per Phase 0's finding.

## Phase 3 — Security review (gate)

- Audit against `security.md` + the #72418 boundary: confirm no shared-token bypass, minimal scope, no
  auth broadening, credential protected at rest + in transit. **Blocking** before rollout.

## Phase 4 — Release + live verification

- `pkg/` changes → `terraform-provider-conga` release (two-repo flow).
- **Live acceptance (operator-in-the-loop):** provision a device identity for one agent, operator sends a
  Slack delegation request → confirm the Qwen subagent actually spawns (the exact thing failing today),
  and operator-surface spawn still works. Then roll out to the fleet (staged, per the managed-host
  rollout discipline) and confirm it survives a refresh + reboot.

## Risks / unknowns to retire in spec

- Mechanism may not be cleanly headless (interactive approval) → need an auto-approve-on-loopback story
  that is *safe* (doesn't re-create #72418).
- Provider divergence (local may not need it).
- Verification depends on the operator for the Slack message (no reliable automated harness — proven).

## Handoff

`/glados:spec-feature` — start with **Phase 0 mechanism discovery on v2026.6.5**; the chosen mechanism
determines the rest of the design. Personas: Architect, QA, PM + Security (cross-cutting).
