# Requirements — Managed-Host Provisioning Engine

## Goal

Provision and maintain agents on **managed hosts** by executing Conga's **shared Go provisioning
logic** over a minimal host transport, eliminating the parallel bash implementation. The infra layer
becomes something the operator can rely on without heavy maintenance, because the logic lives in Go
(one place), is unit-tested in Go, and is reused consistently across **all managed-host providers**
instead of being hand-copied into bash heredocs/templates.

## Deployment taxonomy (load-bearing framing)

Two deployment classes, with deliberately different lifecycle postures:

| Class | Providers | Lifecycle | Posture |
|---|---|---|---|
| **Managed-host** | **remote (SSH) + AWS (SSM)** | **systemd units** (generated once in shared Go) + iptables `ExecStartPost`/`StopPost` egress lifecycle | unattended, reboot-survivable, host-resident enforcement |
| **Local** | local | Docker only, no systemd | attended, dev/personal — the genuinely different case |

This is the central architectural decision: **systemd is THE managed-host lifecycle for all
non-local providers**, not an AWS-only concern. A production VPS (remote/SSH) must run unattended —
survive reboot and re-enforce egress without a human running a command — exactly like AWS. The unit
generation is written **once** in shared Go; the only per-provider seams are the **transport**
(SSH vs SSM) and the **secrets/discovery backend** (files vs SSM Parameter Store + Secrets Manager).

**This closes a real gap in the remote provider, which today has no unattended story.**

## Problem

Agent provisioning is implemented **three times** — the boot `user-data.sh.tftpl` (bash), the
SSM-pushed `scripts/*.sh.tmpl` (bash), and the Go providers — and the copies have drifted, with at
least one live correctness bug. The bash is untestable (it isn't valid bash until Terraform/Go
renders it, so shellcheck can't see it; only runs inside cloud-init on a real boot), carries a
large typo blast radius (a bad edit fails a host boot, discovered via a 15-min SSM poll timeout),
and re-derives artifacts that Go already produces canonically. This is the surface of most recently
chased bugs (router host-networking, `$include` self-heal drift, chown ownership). See
`audit/terraform-bootstrap-audit.md` for the 12-finding inventory and `audit/README.md` Theme 3.

## Current State (verified 2026-06-13)

The duplication splits into two kinds, and the parity below was confirmed by reading the code, not
assumed.

### The keystone pattern already ships
`pkg/provider/iptables/rules.go` separates **pure logic** (`AddRulesCmd(ip, cidr) → cmdString`,
`RemoveRulesCmd`, `CheckRulesCmd`) from a **transport seam**: `type ExecFunc func(cmd string) error`.
- Remote injects an SSH-backed `ExecFunc` (`remoteprovider/docker.go:sshIptablesExec`) →
  `iptables.AddRules(ip, cidr, run)`.
- AWS does **not** use this package — it emits the iptables sequence as a bash `ExecStartPost`
  one-liner duplicated 4× across the boot template and provision scripts.

**Generalizing this `ExecFunc` seam to the rest of host orchestration is the strategy.**

### Parity: AWS is the bash outlier

| Concern | Shared Go today | Remote | AWS |
|---|---|---|---|
| `openclaw.json` generation | ✅ `common.GenerateOpenClawConfig` | Go | **bash heredoc ×4** (stale model pin `opus-4-7`) |
| `routing.json` generation | ✅ `common.GenerateRoutingJSON` + `LoopbackWebhookResolver` | Go | **bash `node -e`** (bridge-form — live bug) |
| `$include` layers | ✅ `common.BuildConfigLayers` / `EffectiveConfigSpecs` / `ResolveCustomConfigSources` | Go | **bash ×3** |
| Envoy egress config | ✅ `policy.GenerateProxyConf` | Go | Go (provider path) / **bash** (boot path) |
| iptables egress (DOCKER-USER) | ✅ `pkg/provider/iptables` (`ExecFunc` seam) | Go (SSH ExecFunc) | **bash** — not using the shared pkg |
| Agent lifecycle | ❌ divergent today → **converge on systemd** | `docker run`, **no `--restart`** (no reboot survival); egress re-applied **lazily** as a `GetStatus` side-effect | per-agent **systemd units** (bash-generated); egress via unit `ExecStartPost`/`StopPost` |
| Config integrity | ❌ divergent (decision deferred) | client-side on-demand sha256 (`remoteprovider/integrity.go`) | host **timer + CloudWatch + SNS** (bash) |
| Secrets / discovery | ❌ divergent by design (stays per-provider) | files on host | SSM Parameter Store + Secrets Manager |

### Lifecycle facts verified (2026-06-13) — the basis for the managed-host taxonomy
- **AWS systemd unit binds, host-resident and unattended:** reboot-start (`WantedBy=multi-user.target`
  + `enable`), crash-restart (`Restart=always`), ordered start (`After=docker.service
  conga-router.service`), pre-start setup (`ExecStartPre=pre-start.sh` + plugin seed), **egress
  applied on every start** (`ExecStartPost` iptables), **egress cleaned on stop** (`ExecStopPost`).
- **Remote has no unattended story today:** `docker run` sets **no `--restart`** policy → containers
  do not return after a host reboot; egress iptables is re-applied **lazily** as a side effect of
  `conga status` (`GetStatus` → `ensureEgressIptables`; the code comment calls this intentional
  self-healing). Acceptable for an attended VPS/RPi; **not** acceptable for unattended production.
- **Remote already requires systemd:** `installDocker` runs `systemctl enable/start docker`
  unconditionally and supports only apt/dnf/yum/pacman (systemd) distros — so per-agent systemd units
  add **no new host dependency**; they make remote honest about what it already assumes.
- **Latent security drift between the two AWS bash copies:** the *boot-path* unit applies egress
  iptables via `ExecStartPost` (survives restart); the *add-path* unit (`add-user.sh.tmpl`, run by
  `conga admin add-user`) applies iptables imperatively **once** after `systemctl start` and its only
  `ExecStartPost` is the deprecated `docker network connect conga-router`. **An agent added post-boot
  loses egress enforcement on its next restart/reboot.** One Go generator → one correct unit fixes this.

### Transport primitives AWS needs already exist
`awsprovider` has `uploadFile` (file → host) and `runOnInstance` (command → host) over SSM
(`awsprovider/channels.go`), all using the SSM ≥30s timeout minimum. The bash isn't there for an
architectural reason — it's historical.

### The live bug this retires (and the ideal first slice)
`audit/terraform-bootstrap-audit.md` #1: AWS `add-user`/`add-team`/`refresh-*` scripts still write
**bridge-form** routing.json (`http://conga-<name>:18789/...`), run `docker network connect
conga-router`, and emit the `ExecStartPost` connect line — contradicting the v0.1.8 host-networking
router migration (`specs/2026-06-11_bugfix_router-host-networking/`). Agents added after first boot
are wired for a router model the host no longer uses.

## Success Criteria

1. **Single source of provisioning logic.** No agent-provisioning logic is duplicated between bash
   and Go. The `scripts/*.sh.tmpl` provisioning family (add-user, add-team, refresh-user,
   refresh-all, and the codegen blocks in `deploy-*`) is removed or reduced to thin transport glue.
2. **Artifact equivalence.** AWS add/refresh produce artifacts (`openclaw.json`, `routing.json`,
   `$include` layers, Envoy config) that are byte- or schema-equivalent to the Go canonical the
   remote provider already produces — verified by tests, not by inspection.
3. **Router bug fixed.** AWS-added agents are wired loopback (`127.0.0.1:<hostPort>`), with no
   `docker network connect conga-router` and no bridge-form webhook URLs. A test asserts produced
   artifacts contain loopback URLs and do **not** contain `conga-router`.
4. **Testability.** Host-orchestration logic is exercised by Go unit tests. No logic lives in
   untestable templated bash; remaining bash is install/bootstrap glue only.
5. **Consistent reuse.** The converged concerns flow through one shared managed-host code path used
   by both remote and AWS, with the transport (`{PutFile, RunCommand}` — SSM for AWS, SFTP+SSH for
   remote) as the only provider-specific seam.
5b. **Unattended parity across managed hosts.** Both remote and AWS survive a host reboot
   (agents return running) and re-enforce egress automatically, host-resident, with no operator
   command — via the shared systemd unit (`enable` + `ExecStartPost`). Remote's lazy
   `GetStatus`→iptables self-heal side-effect is removed once the unit owns enforcement. Verified by
   a reboot test on the remote (RPi/VPS) target and on an isolated AWS agent.
5c. **Init system is a reserved seam (future-proofing).** The engine emits a provider-agnostic
   `ServiceSpec`; the init system is abstracted behind a `HostSupervisor` interface with **systemd as
   the only built backend**. A stubbed `openrcSupervisor` (returns `ErrUnsupportedSupervisor`) +
   selection switch reserve the extension point so a future lightweight-host scenario (Alpine/OpenRC)
   is additive, not an engine refactor. **No systemd-ism may leak past the supervisor boundary into
   the engine or `ServiceSpec`.** Full design: [`extension-host-supervisor.md`](./extension-host-supervisor.md).
   *Scope guard:* build the seam + systemd backend + stub + doc only; do NOT build OpenRC/Alpine now (YAGNI).
6. **Security boundaries preserved (no regression).** Channel allowlist, `$include` reserved-key
   guard, egress iptables enforcement (fail-closed), secrets-as-env (not config — #9627), and
   root:root `0444` on Conga-managed files all hold under the new path. Re-audited at the gate.
6b. **Config integrity = prevention-first (decision #2, resolved 2026-06-13).** The reserved-key
   guard (no include layer may declare `$include`/`channels`/`gateway`/`plugins`) moves from a
   periodic *detective* alarm to a **fail-closed `PreStart` hook**: an agent whose include layers
   declare a reserved key refuses to start (host-resident, so it catches in-place edits to the
   admin-drift `agent-custom.json` between commands). The guard's key list is **generated from
   `common.ReservedCustomConfigKeys`** (single source of truth — the host check cannot drift from the
   Go validator). File perms stay (Principle 2). The periodic SHA256 check is retained as the
   Principle-6 backstop but **slimmed** (drop the audit-#4 dual-baseline ordering coupling; one
   baseline per managed file). Provider split: the **preventive** guard converges across managed-host
   providers (remote gains it); the **detective/alerting** layer stays provider-appropriate (AWS keeps
   CloudWatch/SNS — slimmed; remote logs via a host timer / on-demand). *Open slimming detail for
   spec:* keep vs. drop AWS CloudWatch/SNS alerting (lean: keep, slimmed).
7. **Boot script materially reduced.** `user-data.sh.tftpl` shrinks (target: provisioning *logic*
   removed; what remains is OS hardening + Docker install + bootstrap handoff). Exact target set in spec.
8. **Live-fleet safe.** Migration is incremental (per-artifact slices), each verifiable on an
   isolated agent before fleet rollout; no flag-day rewrite of the production path.

## Scope

### In scope
- A shared Go managed-host provisioning path (generalizing the `pkg/provider/iptables` `ExecFunc`
  pattern) covering the artifacts AWS currently re-derives in bash, **plus shared systemd unit
  generation used by both remote and AWS**.
- An SSM-backed transport adapter for AWS satisfying the minimal **`{PutFile, RunCommand, ReadFile}`**
  contract, reusing existing `uploadFile`/`runOnInstance` (+ an SSM-backed read). `ReadFile`/`Download`
  is **core, not optional** — needed to read the admin-drift layer for the provenance view and for any
  future `pull`. Remote already has `Download`.
- Migrating AWS add/refresh (and the relevant boot-path provisioning) onto that path, slice by slice.
- **Upgrading the remote provider to systemd-managed agents** (per-agent units, reboot survival,
  `ExecStartPost` egress) — closing its current unattended gap — and removing the lazy
  `GetStatus`→iptables self-heal once the unit owns enforcement. Includes a migration step for
  existing remote deployments and re-verification on the remote (RPi/VPS) target.
- Removing/retiring the superseded `scripts/*.sh.tmpl` provisioning logic and the duplicated bash
  codegen (openclaw.json heredocs, `node -e` routing, inline iptables, `$include` seeding).
- Fixing the router-drift bug as the first slice; folding in audit #7 (static/known agent IP so the
  unit's egress `ExecStartPost` is deterministic, generated Go text with no runtime IP-discovery race).

### Out of scope (explicit non-goals)
- **A literal provider merge** (`AWS` becoming an instance of `remoteprovider`). Rejected.
- **Unifying SSH and SSM behind one transport interface.** The providers keep their own transports;
  they only satisfy a minimal shared contract.
- **Reworking remote beyond the systemd lifecycle upgrade.** Remote's artifact generation is the
  reference (behavior-preserving extraction). The one intended behavior change is adopting
  systemd-managed agents (in scope above); nothing else about remote changes.
- **The local provider** (Docker-only, no managed-host concerns) beyond shared-code refactors.
- **MCP/CLI surface reduction, OpenClaw-config boundary changes, dead-code removal** — separate
  audit themes, not this feature.
- **Effective-config visibility + `conga agent pull`** — a **related follow-on feature** (config
  observability, in the #30/#31 lineage), NOT this feature. Direction set 2026-06-13 (Model C): the
  super-admin "what's actually running" view sources the **effective config from in-container
  `openclaw config get`** (OpenClaw is ground truth; Conga does NOT re-derive the merge) + a Conga
  **provenance overlay** (which layer set each key: managed-root/fleet/per-agent-code/admin-drift) —
  delivered as an enhancement to `conga agent show-config`. `conga agent pull` (promote host
  admin-drift into version-controlled code) is an optional remediation built only if version-control
  durability is wanted. This feature must not *foreclose* it → `ReadFile` is core (see scope-in).
  Reconciles the earlier CLI-audit `show-config` finding: not a removal candidate; refine it to defer
  to `openclaw config get` for the effective view.

## Constraints

- **Two-repo coupling.** `pkg/` changes require a `terraform-provider-conga` release (tag congaline
  → `go get`/`go mod tidy` → tag provider → GoReleaser → bump pin). Plan release checkpoints; expect
  one per landed `pkg/` increment.
- **SSM transport limits.** ≥30s `SendCommand` timeout, async send+poll, no persistent session, no
  native file transfer, output truncation. The engine must issue **few, small** file-puts and short
  idempotent commands — never stream large scripts. (This is *why* logic runs client-side in Go.)
- **No regression of the v0.1.8 host-networking router model.**
- **Live production fleet** (e.g. `aaron`). Isolated-agent probes first; byte-exact backup/restore
  discipline as in prior infra specs.
- **Security standards review required** (`product-knowledge/standards/security.md`) before
  implementation — egress enforcement, channel allowlist, secrets handling are all touched.
- **`umask 077` host convention** and explicit chown (uid 1000 node, uid 101 envoy) must be honored
  by whatever writes files on the host.
- **systemd is a documented managed-host requirement.** Both remote and AWS hosts must run systemd
  (already de-facto true for remote — `installDocker` uses `systemctl`). Non-systemd hosts
  (Alpine/OpenRC) are unsupported for managed-host; the local provider covers the non-systemd/dev case.
