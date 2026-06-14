# Plan — Managed-Host Provisioning Engine

High-level approach. Detailed design (interfaces, signatures, exact file moves, test matrix,
security gate) is deferred to `spec.md` via `/glados:spec-feature`.

## Approach

**Thesis:** AWS already has everything it needs to stop using bash — the canonical generators
(`pkg/common`, `pkg/policy`), the transport primitives (`uploadFile`/`runOnInstance` over SSM), and
a proven seam pattern (`pkg/provider/iptables`'s `ExecFunc`). The work is to *generalize the seam*
and *route AWS through the shared logic*, then delete the bash it replaces.

### 0. Three orthogonal seams (the architecture)
"Managed-host" is any combination of three independent abstractions; the engine talks only to these,
never to a concrete init/transport/store. Today they correlate (AWS = SSM+systemd, remote = SSH+systemd)
but must **not** be fused.

| Seam | Backends now | Future |
|---|---|---|
| **Transport** (`{PutFile, RunCommand}`) | SSH, SSM | — |
| **HostSupervisor** (`ServiceSpec` → init artifacts) | **systemd** | OpenRC/runit/s6 (reserved stub) |
| **Secrets/discovery** | files, SSM+Secrets Manager | — |

See [`extension-host-supervisor.md`](./extension-host-supervisor.md) for the supervisor seam contract.

### 1. Define the transport contract (seam #1)
A minimal contract both managed-host transports already satisfy:
- `PutFile(path string, content []byte, mode os.FileMode) error` — remote: SFTP `Upload`; AWS: `uploadFile` (SSM).
- `RunCommand(ctx, cmd string) (stdout string, err error)` — remote: SSH `Run`; AWS: `runOnInstance` (SSM, ≥30s).
- `ReadFile(path string) ([]byte, error)` — **core (resolved 2026-06-13)** — remote: `Download`; AWS:
  SSM-backed read. Needed for integrity/verify, the provenance view (read admin-drift layer), and any
  future `pull`. (Was deferred; promoted to core so the engine doesn't foreclose visibility/pull.)

This is the generalization of `iptables.ExecFunc`. Whether it's a Go `interface` or a struct of
funcs is a spec decision; the shape stays small so SSM's constraints don't leak upward.

### 1b. Define the HostSupervisor seam (seam #2) — systemd backend + reserved stub
The engine emits a provider-agnostic `ServiceSpec` (name, run argv, env, deps, restart, hooks
{preStart/postStart/postStop}, log target); a `HostSupervisor` backend renders it. **Build
`systemdSupervisor` only**; reserve `openrcSupervisor` as a stub returning `ErrUnsupportedSupervisor`
+ a selection switch. The egress hook reuses `pkg/provider/iptables` at the backend's post-start
point. Boundary rule: no systemd-ism in the engine or `ServiceSpec`. Bonus: emitting `ServiceSpec`
(not unit text) makes the engine unit-testable now, independent of the future backend.

### 2. Extract a shared managed-host package
Pull the host-orchestration that remote implements in Go (and AWS implements in bash) into a shared
location (working name `pkg/provider/managedhost`, or extend `pkg/common`). It composes the existing
generators (`GenerateOpenClawConfig`, `GenerateRoutingJSON`, `BuildConfigLayers`,
`GenerateProxyConf`, `iptables.AddRules`) and drives them through the transport contract:
"generate artifact in Go → `PutFile` → `RunCommand` for systemctl/iptables/daemon-reload." Remote's
existing flow becomes the reference implementation; AWS gets a transport adapter and stops rendering bash.

### 3. Migrate AWS slice by slice (each independently shippable + verifiable)
Each slice: move one artifact/concern from bash to the shared Go path, add Go tests, verify on an
isolated AWS agent, then delete the superseded bash. Proposed ordering (spec confirms):

1. **routing.json (proof slice + live bug fix).** AWS add/refresh writes Go-generated *loopback*
   routing.json via `PutFile`; drop bash `node -e`, `docker network connect conga-router`, and the
   `ExecStartPost` connect line. Test: produced artifact has `127.0.0.1`, not `conga-router`.
   Fixes `audit` #1. Smallest end-to-end exercise of the whole pattern.
2. **openclaw.json + `$include` layers.** Replace the 4 bash heredocs and the 3 bash `$include`
   self-heal copies with `common.*` generation + `PutFile`, root:root `0444` re-protect via
   `RunCommand`. Retires `audit` #2, #4, #12; fixes the stale `opus-4-7` pin.
3. **egress: Envoy config + iptables.** Use `policy.GenerateProxyConf` + `iptables.AddRules` with an
   SSM `ExecFunc`/transport; address the fail-closed/race hardening (`audit` #7) deterministically
   (static/subnet-based source IP). Retires the 4× inline iptables duplication.
4. **systemd unit text (shared).** Generate unit bodies in Go templates (tested) rather than bash
   heredocs; the generator is shared by remote + AWS. Retires `audit` #8 (in-place `sed` patching)
   and fixes the add-path egress-drift (one generator → unit always has the correct `ExecStartPost`).
5. **Boot-path reduction.** Move first-provisioning to a post-boot Go-over-SSM pass (mirroring
   remote's post-SSH provisioning), shrinking `user-data.sh.tftpl` toward install+handoff. Scope of
   reduction set by the integrity decision.
6. **Remote systemd adoption (the upgrade).** Switch remote from bare `docker run` to the shared
   systemd unit (per-agent unit via `PutFile` + `systemctl` over SSH); remove the lazy
   `GetStatus`→iptables self-heal. Migration for existing remote deployments + reboot re-verification
   on the RPi/VPS target. Delivers unattended remote-VPS production.

### 4. Retire the bash and centralize literals
Delete superseded `scripts/*.sh.tmpl`; collapse the image-tag/model-pin duplication (`audit` #6, #2)
to single Go/locals sources as their consumers move to Go.

## Open Decisions (to resolve in `spec.md`)

1. ~~Agent lifecycle on AWS.~~ **RESOLVED (2026-06-13):** systemd is THE managed-host lifecycle for
   **all non-local providers** (remote + AWS). Generate the unit *text* in shared Go (tested
   templates); systemd owns reboot-start, crash-restart, ordered start, and the
   `ExecStartPost`/`StopPost` egress lifecycle, host-resident and unattended. Remote already requires
   systemd (`installDocker` uses `systemctl`), so this adds no host dependency and **closes remote's
   current unattended gap** (no `--restart`, lazy `GetStatus` egress self-heal). Remote's lazy
   self-heal side-effect is removed once the unit owns enforcement. Pair with audit #7 (static/known
   agent IP) so the egress `ExecStartPost` is a deterministic generated string, no runtime IP race.
   Local stays Docker-only (no systemd). *Open sub-question for spec:* migration of existing remote
   deployments from `docker run` to systemd units, and the reboot re-verification protocol.
2. ~~Config-integrity model.~~ **RESOLVED (2026-06-13) — prevention-first.** The reserved-key guard
   becomes a **fail-closed `PreStart` hook** (refuse to start if any include layer declares
   `$include`/`channels`/`gateway`/`plugins`; host-resident, catches in-place `agent-custom.json`
   edits), with its key list **generated from `common.ReservedCustomConfigKeys`** (no host/Go drift).
   File perms stay. The periodic SHA256 check is kept as the Principle-6 backstop but **slimmed**
   (drop the `audit` #4 dual-baseline coupling; one baseline per managed file). The preventive guard
   **converges** across managed-host providers (remote gains it); detective/alerting stays
   provider-appropriate (AWS = CloudWatch/SNS, slimmed; remote = host timer/log). The guard naturally
   expresses as a `ServiceSpec.Hooks.PreStart` entry — same shared engine, any supervisor backend.
   *Remaining slimming detail for spec:* keep vs. drop AWS CloudWatch/SNS alerting (lean: keep, slimmed).
3. **Boot-script reduction depth.** Does first-provisioning move entirely to post-boot Go-over-SSM,
   or does the boot path retain a minimal agent bring-up? Affects `user-data.sh.tftpl` end size.
4. **Transport contract form.** Go `interface` implemented by each provider vs. a struct of funcs
   (direct generalization of `iptables.ExecFunc`). Include `ReadFile` in the core contract or not.
5. **Shared package location/name.** New `pkg/provider/managedhost` vs. extending `pkg/common`;
   how much of remote moves vs. is wrapped.
6. **Migration/verification protocol.** Isolated-agent probe steps per slice; backup/restore;
   fleet rollout order; rollback per slice.

## Risks & Mitigations

- **SSM impedance** (no streaming/SFTP, async, truncation). *Mitigation:* the design issues few
  small file-puts + short commands; logic runs client-side. Validate the round-trip in slice 1.
- **Live-fleet regression** (egress, delivery, config validity). *Mitigation:* per-slice isolated
  verification before rollout; security re-audit; behavior-preserving extraction of remote's code.
- **Two-repo release tax** per `pkg/` increment. *Mitigation:* batch slices into release checkpoints;
  consider the `audit` #10 process improvement (single release target) as a parallel track.
- **Scope creep into a provider merge.** *Mitigation:* the non-goals in `requirements.md` are
  load-bearing; the contract stays minimal and transports stay separate.
- **Hidden AWS-only host assumptions** surfacing during extraction (umask/chown, ECR login, fck-nat,
  staggered boot). *Mitigation:* inventory these in the spec's current-state deep-read before coding.

## Related follow-on (NOT this feature — recorded so the engine doesn't foreclose it)

**Config ownership = Model C (resolved 2026-06-13).** `$include` deep-merge + admin-survival stays;
code is the authoritative *record*, not a clobbering owner. No bidirectional reconcile engine (that
would reintroduce #30's clobber problem + a pull/push race).

**Effective-config visibility (super-admin) — a follow-on feature in the #30/#31 lineage.** The
"what's actually running" view sources the **effective config from in-container `openclaw config get`**
(OpenClaw is ground truth; Conga must NOT re-derive the `$include` merge) and overlays Conga's
**provenance** (which layer set each key). Delivered by enhancing `conga agent show-config`. Needs
only `RunCommand` (the `openclaw config get` fetch) + `ReadFile` (read the admin-drift layer) — both
in this feature's core transport, so the engine enables it without implementing it here.

**`conga agent pull` — optional remediation, build only on demand.** Promote host admin-drift into
version-controlled code once the provenance view shows drift. One-directional, operator-driven; no
race, no clobber. Out of scope here; `ReadFile`-core keeps the door open.

## Definition of Done (this feature, across slices)

`requirements.md` Success Criteria 1–8 met; `audit` findings #1, #2, #4, #12 retired and #3, #7, #8
materially reduced; security gate PASS; provider released; deployed-path verified on the live fleet.
