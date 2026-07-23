# Technical Specification — Managed-Host Provisioning Engine

- **Created**: 2026-06-13
- **Owner**: <operator>
- **Status**: Specified (pre-implementation)
- **Builds on**: `requirements.md`, `plan.md`, `extension-host-supervisor.md` (read those first)
- **Lineage**: `audit/` Theme 3; extends #30 (`$include`) + #31 (declarative layering); fixes audit #1, #2, #4, #7, #8, #12.

---

## 1. Summary

Replace the hand-maintained bash provisioning paths on AWS (the `scripts/*.sh.tmpl` family + the
codegen blocks in `user-data.sh.tftpl`) with the **shared Go logic the remote provider already
runs**, executed over a minimal transport seam. Generalize the shipped `pkg/provider/iptables`
`ExecFunc` pattern into a managed-host engine that **generates artifacts in Go** (openclaw.json,
routing.json, `$include` layers, env, Envoy config, the systemd unit, the reserved-key guard) and
ships them as data; the host only places files and runs short `systemctl`/`iptables` commands.

The engine is shared by both **managed-host** providers (remote-SSH + AWS-SSM); **local** stays
Docker-only. The result: one tested Go provisioning path, the bash drift retired, remote gains
unattended operation, and the channel-allowlist boundary moves from a periodic alarm to a
fail-closed start gate.

---

## 2. Architecture — decisions locked

### 2.1 Deployment taxonomy
- **Managed-host** (remote + AWS): systemd lifecycle, unattended, host-resident egress. Shared engine.
- **Local**: Docker-only, dev/attended. Unaffected except shared-code refactors.

### 2.2 Three orthogonal seams
The engine talks only to these abstractions; today they correlate per provider but must not fuse.

| Seam | Contract | Backends now | Future |
|---|---|---|---|
| **Transport** | `Transport` interface (§3) | SSH (remote), SSM (aws) | — |
| **HostSupervisor** | `ServiceSpec` → init artifacts (§4) | systemd | OpenRC stub (`extension-host-supervisor.md`) |
| **Secrets/discovery** | existing per-provider | files / SSM+Secrets Manager | — |

### 2.3 What stays as-is (do not touch)
`$include` deep-merge + admin-survival (#30/#31); the `Provider` interface shape (this is an
*internal* refactor of how AWS fulfills it, not an interface change); local provider behavior;
secrets-as-env (#9627); per-agent network isolation + Envoy proxy + iptables-in-all-modes
(egress-controls.md).

---

## 3. Seam #1 — Transport (resolved: Go interface)

**Decision:** a Go `interface` (not a struct of funcs). Two real implementations + a fake for tests;
an interface is cleaner than threading three closures. The single-method `iptables.ExecFunc` stays a
func type (one method); the multi-method transport is an interface.

```go
// pkg/provider/managedhost/transport.go
type Transport interface {
    PutFile(ctx context.Context, path string, content []byte, mode os.FileMode) error
    RunCommand(ctx context.Context, cmd string) (stdout string, err error)
    ReadFile(ctx context.Context, path string) ([]byte, error) // CORE — provenance view + future pull
}
```

- **remote**: `PutFile`→SFTP `Upload` (atomic tmp+chmod+rename); `RunCommand`→SSH `Run`; `ReadFile`→`Download`. Already exist.
- **aws**: `PutFile`→`uploadFile` (SSM); `RunCommand`→`runOnInstance` (SSM, ≥30s); `ReadFile`→new SSM-backed read (`base64` + `RunCommand`, or read-into-stdout). All ≥30s SSM timeouts.
- **SSM discipline (constraint):** the engine issues **few small PutFiles + short RunCommands** per agent (≈7 files + ≈3 commands, all <few KB) — never streams a large script. This is the whole point of generating client-side.
- An `iptables.ExecFunc` is derived from a `Transport` via `func(cmd string) error { _, err := t.RunCommand(ctx, cmd); return err }`, so the shipped iptables logic is reused unchanged on AWS.

---

## 4. Seam #2 — HostSupervisor (resolved: systemd backend + reserved OpenRC stub)

The engine emits a provider-agnostic `ServiceSpec`; a backend renders it. **No systemd-ism may
appear in the engine or `ServiceSpec`** (boundary rule, requirements §5c).

```go
// pkg/provider/managedhost/supervisor.go
type RestartPolicy struct { Mode string /* "always"|"on-failure" */; DelaySec int }

type LifecycleHooks struct {
    PreStart  []string // docker rm -f; plugin seed; RESERVED-KEY FAIL-CLOSED GUARD (§8)
    PostStart []string // egress iptables apply (deterministic via static IP, §6.3)
    PostStop  []string // egress iptables remove
}

type ServiceSpec struct {
    Name        string            // "conga-<agent>"
    Description string
    RunArgv     []string          // the `docker run …` argv (from the shared container-arg builder)
    EnvFile     string
    After       []string          // ordering deps: docker.service, conga-router.service
    Requires    []string
    Restart     RestartPolicy
    Hooks       LifecycleHooks
    LogTarget   string
}

type HostSupervisor interface {
    DefineService(ctx context.Context, t Transport, spec ServiceSpec) error // render+PutFile+enable
    Start(ctx, t, name) error; Stop(...); Restart(...); RemoveService(...)
    Status(ctx, t, name) (ServiceState, error)
}

var ErrUnsupportedSupervisor = errors.New("supervisor backend not implemented — see specs/2026-06-13_feature_managed-host-provisioning-engine/extension-host-supervisor.md")
```

- **systemdSupervisor** (built): renders `ServiceSpec`→unit text (the §2.1 boot unit shape), `PutFile`s to `/etc/systemd/system/conga-<name>.service`, `daemon-reload`, `enable --now`. Replaces the bash heredoc unit. **Fixes audit #8** (no in-place `sed`; always writes the whole unit) and the add-path egress drift (one generator → the unit always carries the egress hooks).
- **openrcSupervisor** (stub): all methods return `ErrUnsupportedSupervisor`. Selection switch knows only systemd today (detect `command -v systemctl`), OpenRC case wired to the stub. **YAGNI: do not implement OpenRC.**

---

## 5. Shared package boundary (resolved: new `pkg/provider/managedhost`)

Per architecture.md ("new packages preferred when the domain is distinct" — managed-host
orchestration is distinct from `common`'s pure config-gen). `pkg/provider/managedhost` depends on
`pkg/common` (generators), `pkg/policy` (`GenerateProxyConf`), `pkg/provider/iptables`. It is in
`pkg/` (importable by `terraform-provider-conga`) → **`pkg/` change → provider release**.

```
pkg/provider/managedhost/
  transport.go      Transport interface + ExecFunc adapter
  supervisor.go     HostSupervisor, ServiceSpec, systemd backend, openrc stub
  engine.go         ProvisionAgent/RefreshAgent orchestration (generate → PutFile → command)
  artifacts.go      assemble the per-agent artifact set from pkg/common + pkg/policy
  guard.go          generate the reserved-key guard script from common.ReservedCustomConfigKeys
  *_test.go
```

`remoteprovider` and `awsprovider` shrink to: a `Transport` impl, a secrets/discovery impl, and thin
calls into `managedhost`. `remoteprovider` is the behavior reference — its Go logic is the basis for
`engine.go`; extraction is behavior-preserving except the systemd upgrade (§10).

---

## 6. Code changes by area

### 6.1 Engine (`managedhost/engine.go`, `artifacts.go`)
`Provision(ctx, t, sup, agent, ...)`: build the artifact set in Go → `PutFile` each → `DefineService`
→ `Start`. `Refresh` regenerates artifacts + `DefineService` (idempotent) + `Restart`. Artifacts:
openclaw.json (`common.GenerateOpenClawConfig`), the 3 `$include` layers, routing.json
(`common.GenerateRoutingJSON` **loopback**), env (`common.GenerateEnvFile`), Envoy config
(`policy.GenerateProxyConf`), the reserved-key guard script, the systemd unit (via supervisor).

### 6.2 Routing — slice 1, the live-bug fix (audit #1, #12)
AWS add/refresh stop mutating routing.json with `node -e`; the engine generates **loopback**
routing.json (`LoopbackWebhookResolver`) and `PutFile`s it. Drop every `docker network connect
conga-router` and the `ExecStartPost` connect line. **Test:** rendered routing.json contains
`127.0.0.1:<hostPort>` and the unit contains no `network connect conga-router`.

### 6.3 Egress — static IP makes the hook deterministic (audit #7)
Assign each agent a **known IP** at network create (`docker network create --subnet …`, `docker run
--ip …`). The egress iptables source is then known before start → `Hooks.PostStart` (or PreStart) is
a static `iptables.AddRulesCmd(knownIP, subnetCIDR)` string — **no 10-retry discovery loop, no race,
fully unit-testable**. iptables stays active in all modes (egress-controls.md). Reuse
`pkg/provider/iptables` via the `Transport`-derived `ExecFunc`.

### 6.4 Removal (as slices land)
Delete the superseded `scripts/*.sh.tmpl` provisioning logic and the bash codegen (openclaw.json
heredocs ×4, `node -e` routing, the grep/sed egress YAML parser, the 4× inline iptables). Centralize
the image tag to one source (audit #6). Confirmed-dead embedded scripts already flagged
(`refresh-all.sh.tmpl`, `unpause-agent.sh.tmpl`) removed.

---

## 7. Data model — `ServiceSpec`

`ServiceSpec` is **in-memory Go only** — not a persisted config file, so it adds **no new locus** to
the config taxonomy (config-taxonomy.md). The persisted artifacts it produces (unit file, guard
script) live on the host alongside existing ones. No JSON/YAML schema change; no `agents/<name>.json`
or SSM-param shape change.

---

## 8. Integrity model (decision #2 — prevention-first)

| Layer | Control | Change |
|---|---|---|
| Structural | root:root `0444` on managed files (Principle 2) | **unchanged** |
| **Preventive** | reserved-key guard as a **fail-closed `ExecStartPre`** | **NEW** — moved from periodic alarm |
| Detective (backstop, Principle 6) | periodic SHA256 of managed files | **slimmed** — drop audit-#4 dual-baseline coupling |

- **Preventive guard:** `Hooks.PreStart` runs a tiny host-side guard script that reads every
  `$include` layer (incl. the admin-editable `agent-custom.json`) and **exits non-zero if any
  declares `$include`/`channels`/`gateway`/`plugins`** → systemd aborts the start. Catches in-place
  admin edits at next restart; prevents allowlist escalation from ever taking effect (stronger than
  detecting it minutes later). Matches OpenClaw's own fail-closed posture.
- **Single source of truth:** `guard.go` generates the script's key list from
  `common.ReservedCustomConfigKeys` — the host guard cannot drift from the Go validator. (The guard
  script is the *one* sanctioned bit of host-side script: tiny, static, shellcheck-able, generated.)
- **Unparseable (JSON5) include — resolved (QA gate):** the guard **blocks (fail-closed) only on a
  *detected* reserved key in parseable JSON**. On an unparseable include (admin wrote JSON5/comments,
  the strict parser → `ErrCustomConfigUnparseable`) it **logs a WARN and allows start**, rather than
  taking the agent down over a comment. This is safe because on managed hosts the include files are
  root:root `0444` (uid 1000 cannot inject), so an unparseable include is an admin/root artifact, not
  an agent-injection vector — consistent with the documented residual from #30's security gate
  (JSON5 key-name evasion compensated by perms). The WARN still surfaces via the detective log.
- **Detective backstop:** keep the periodic check, but one baseline per managed file (no
  cross-file ordering coupling). The reserved-key jq is removed from the timer (now preventive).
- **Provider split:** the preventive guard **converges** (remote gains it). Detective/alerting stays
  provider-appropriate: AWS keeps CloudWatch/SNS (**slimmed** — see open checkpoint); remote uses a
  host timer that logs (replacing the lazy `GetStatus`→iptables self-heal, which is **removed**).

---

## 9. Per-slice migration plan

Each slice is independently shippable, Go-tested, isolated-agent-verified, then the superseded bash deleted.

| # | Slice | Retires | Risk |
|---|---|---|---|
| 1 | **routing.json loopback via engine** (proof + live-bug fix) | audit #1, #12 | low |
| 2 | openclaw.json + `$include` layers via engine | audit #2, #4 | low–med |
| 3 | egress: Envoy config + static-IP iptables via engine | audit #7 | med |
| 4 | systemd unit text via `systemdSupervisor` + preventive guard | audit #8 + integrity | med |
| 5 | boot-script reduction (§11) | audit #3 | **high** |
| 6 | remote systemd adoption + migration (§10) | remote unattended gap | med |

Slices 1–4 are AWS-internal and low-blast-radius. Slice 5 is the hard one (§11). Slice 6 changes
remote behavior (§10). Order may interleave 6 earlier to validate the supervisor on the cheaper
(SSH) transport first — an implementation call.

---

## 10. Remote migration + reboot re-verification (slice 6)

Remote moves from bare `docker run` (no `--restart`) to systemd-managed agents.
- **Migration:** on next `RefreshAgent`/`Setup`, the engine writes + enables the unit; the existing
  bare container is replaced by the systemd-managed one (data dir preserved — §13). One-time, idempotent.
- **Removes:** the lazy `GetStatus`→`ensureEgressIptables` self-heal side-effect (egress now owned by
  the unit's hooks). `GetStatus` becomes side-effect-free (a correctness win).
- **Re-verification:** on the RPi/VPS target — reboot the host, confirm agents return running
  (systemd `enable`) and egress iptables are re-applied automatically (unit `PostStart`), no operator
  command. This is criterion 5b for remote.

---

## 11. Boot-script reduction (slice 5) — the hard tension, with a recommendation

**The tension:** today the AWS boot script (cloud-init) provisions *all* agents from SSM params with
**no operator present** — that's how a fresh or replaced host comes up. The engine runs *client-side*
(we deliberately ship **no host binary**). So "move provisioning out of boot" collides with
"unattended host replacement."

**Resolution (recommended):** distinguish reboot from replacement.
- **Reboot (same instance):** systemd units persist on the root volume + `enable` → agents return
  with **zero** provisioning. No engine needed. ✅ (This is the common unattended case.)
- **Host replacement (new instance):** the engine writes the per-agent **unit files + artifacts onto
  the persistent EBS data volume** (the `prevent_destroy` volume that survives replacement) in
  addition to their live locations. The boot script shrinks to a **dumb reconstitution loop** — copy
  persisted unit files into `/etc/systemd/system`, `daemon-reload`, `enable --now` — containing **no
  generation logic** (no heredocs, no `node -e`, no YAML parser). First-ever provisioning of a brand
  new agent remains operator/`terraform apply`-driven (as it is today via `admin add-user`).
- **Net:** `user-data.sh.tftpl` drops from ~1,384 lines (OS hardening + Docker + secret fetch +
  generation) to OS hardening + Docker install + secret fetch + the reconstitution loop. Generation
  is gone; the typo blast radius collapses.

**Why not ship a host binary instead?** It reverses the no-host-binary decision and adds a versioned
artifact fighting the two-repo coupling. Reconstitution-from-persisted-artifacts keeps logic
client-side and the host dumb. *Flagged as the highest-risk slice; gated on a host-replacement
recovery test (§14) before the old boot path is deleted.*

---

## 12. Lifecycle walk-throughs

- **Provision (operator):** engine generates artifacts → `PutFile` ×N → write+persist unit →
  `DefineService` (enable) → `Start`. Egress applied via deterministic hook. Guard validated at PreStart.
- **Refresh (operator):** regenerate artifacts → `PutFile` (idempotent) → re-`DefineService` →
  `Restart`. **Never touches the data dir** (§13). Re-baseline the slimmed integrity hash.
- **Reboot (unattended):** systemd starts enabled units → PreStart guard re-validates includes →
  container starts → PostStart re-applies egress. No operator, no engine.
- **Host replacement (unattended):** boot reconstitutes units from the persisted EBS volume → as reboot.

### 12.1 Partial-failure & idempotency (QA gate)
The engine is **idempotent and re-runnable**; a re-`Provision`/`Refresh` re-puts the full artifact
set and re-`DefineService`s. Ordering makes partial failure safe: **all artifacts + the unit are
`PutFile`'d and the service `DefineService`'d (defined, not started) *before* `Start`**. So a
mid-sequence transport failure leaves the agent **not-started** (recoverable by re-running), never a
running container with a half-applied config. **Egress is never half-applied:** the iptables rules
live in the unit's `Hooks` (applied atomically by systemd at start / removed at stop), so an agent is
either started-with-egress or not-started — there is no "running but unfiltered" window (closing the
audit-#7 race rather than reintroducing it). A failed `Start` surfaces the error (no silent `exit 0`).

---

## 13. Data Safety (required — architecture.md §Agent Data Safety, **must**)

- The engine **regenerates config/units/routing/env/proxy only**; it **never deletes, overwrites, or
  recreates** `/opt/conga/data/<name>/` (AWS/remote) contents. The `-v <data>:<ContainerDataPath>:rw`
  mount is preserved across stop/start/recreate.
- Persisting unit files to the EBS data volume (§11) writes to a **config/units subdir**, never the
  agent memory/workspace paths.
- The existing `chown -R 1000:1000 <dataDir>` (SFTP-root-owned-files fix) is preserved in the engine path.
- **Teardown** is unchanged (data preserved by default; `--delete-data` opt-in across CLI/JSON/MCP).
- **Test:** provision → refresh → (slice 4) unit-regenerate → reboot → confirm data dir contents byte-identical.

---

## 14. Testing strategy

- **Unit (the core win):** `ServiceSpec`→unit-text rendering; `artifacts.go` output **byte/schema-
  equivalent to the remote-proven `common.*`/`policy.*` generators**; `guard.go` key list ==
  `common.ReservedCustomConfigKeys`; routing.json is loopback + no `conga-router`; static-IP iptables
  command strings via a fake `Transport`. No templated-bash assertions.
- **Transport fakes:** an in-memory `Transport` records PutFiles/RunCommands; assert the exact small
  set of files + commands (SSM discipline).
- **Integration:** isolated AWS agent per slice (byte-exact backup/restore, as #30/#31); remote on RPi/VPS.
- **Security/criterion 5b:** reboot survival + automatic egress re-enforcement on both managed-host
  targets; fail-closed PreStart guard refuses an injected-`channels` include (regression test);
  guard **allows + WARNs** on an unparseable JSON5 include (§8 — does not take the agent down).
- **Partial-failure idempotency (§12.1):** fake `Transport` injects a mid-sequence error; assert the
  agent is left not-started (no half-egress), and a re-run completes cleanly.
- **Data persistence:** §13 transition test.

---

## 15. Interface Parity (architecture.md, **must**)

**No new user-facing command, flag, JSON field, or MCP tool is introduced** — this is an internal
refactor of how AWS provisions. Existing commands (`admin add-user/add-team`, `refresh`,
`rebaseline`, etc.) keep their current CLI+JSON+MCP parity; their behavior is unchanged from the
operator's view. The effective-config visibility view (`show-config` enhancement) and `conga agent
pull` are **out of scope** (follow-on); the engine only *enables* them (ReadFile core). Parity is
therefore preserved by construction; no `json_schema.go`/MCP changes required for this feature.

---

## 16. Security considerations (GATES implementation)

| Standard | Verdict (intent) | Note |
|---|---|---|
| Own the box, not the behavior (Principle 8) | ✅ on-mission | pure infra/provisioning consolidation |
| Immutable config (Principle 2) | ✅ preserved | root:root 0444 unchanged |
| Channel allowlist = boundary (Config Integrity) | ✅ **strengthened** | reserved-key guard now preventive (fail-closed) |
| Detect what you can't prevent (Principle 6) | ✅ preserved | slimmed periodic backstop retained |
| Egress: iptables in all modes, fail-closed (egress-controls.md) | ✅ preserved/strengthened | static IP removes the race window |
| Secrets via env, not config (#9627) | ✅ preserved | engine emits env file, not config secrets |
| Defense in depth (Principle 4) | ✅ preserved | perms + guard + iptables + Envoy intact |
| Module structure (architecture.md) | ✅ | engine in `pkg/`; transport-specific in providers |

---

## 17. Out of scope (non-goals)

Provider merge; SSH≡SSM interface unification; effective-config visibility + `conga agent pull`
(follow-on — engine must not foreclose: ReadFile core); OpenRC/Alpine implementation (stub only);
MCP/CLI surface reduction and other audit themes; re-architecting CloudWatch/SNS beyond the slim.

---

## 18. Open implementation checkpoints (resolve during implement)

1. **Boot reconstitution (§11)** — the highest-risk decision. Validate the persisted-unit
   reconstitution + host-replacement recovery test **before** deleting the old boot generation.
2. **Integrity slimming detail** — keep vs. drop AWS CloudWatch/SNS alerting (lean: **keep, slimmed**).
3. **SSM `ReadFile` mechanism** — base64-via-RunCommand vs. a dedicated SSM doc; confirm output-size limits.
4. **Static-IP allocation** — subnet/IP scheme per agent; ensure no collision with existing networks.
5. **Slice ordering** — whether to run slice 6 (remote systemd) early to validate the supervisor over SSH first.
6. **Provider release checkpoints** — batch slices into `terraform-provider-conga` releases.

---

## 19. Handoff

`/glados:implement-feature` — implement slice 1 first (routing.json loopback, the proof + live-bug
fix), landing the `Transport` interface + `managedhost` skeleton + a fake-transport test, then
proceed slice by slice. Reminder: `pkg/` change → `terraform-provider-conga` release.
