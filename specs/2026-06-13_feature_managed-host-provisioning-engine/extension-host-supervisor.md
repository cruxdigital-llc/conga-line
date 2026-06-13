# Extension Design — Host Supervisor Seam (non-systemd backends)

**Status:** Reserved for future implementation. systemd is the only backend built in this feature;
this document defines the seam and the theoretical approach for adding others (OpenRC/runit/s6) so a
lightweight-host scenario (e.g. **Alpine Linux**) is an *additive* change, not an engine refactor.

## Why this exists

The managed-host engine must run agents **unattended** (reboot survival, crash restart, host-resident
egress enforcement). On a standard host that is systemd. But a future "super-lightweight deployment"
may want a host without systemd (Alpine + OpenRC, or a runit/s6 image). We commit now to a structure
that admits such a backend later, and we reserve the extension point in code, without building it.

Two properties already make this cheap:
1. **Logic runs client-side.** Conga's provisioning code executes in the operator's process, not on
   the host; the host only receives artifacts + short commands over the transport. So host OS/libc
   diversity (musl vs glibc, package manager) barely affects us — the host just needs Docker,
   iptables, and *some* process supervisor. (This is a consequence of the "no host binary" decision.)
2. **The egress logic is already seam'd.** `pkg/provider/iptables` exposes pure command-builders
   behind `type ExecFunc func(cmd string) error`. Any supervisor reuses it; only the *invocation
   point* (where the post-start hook runs) differs.

## The three orthogonal seams

"Managed-host" is any combination of three independent seams. The engine talks only to these
abstractions, never to a concrete init system / transport / store.

| Seam | Question it answers | Backends today | Future |
|---|---|---|---|
| **Transport** | How do I reach the host? | SSH (`remote`), SSM (`aws`) | — |
| **HostSupervisor** | How does the host run/persist the service? | **systemd** | OpenRC, runit, s6 |
| **Secrets / discovery** | Where do secrets + agent records live? | files (`remote`), SSM + Secrets Manager (`aws`) | — |

Today the supervisor correlates with the transport (both managed-host providers use systemd), but
the design must **not fuse them** — a host could be `{SSH transport + OpenRC supervisor}`.

## The contract: the engine emits a provider-agnostic `ServiceSpec`

The engine never produces "unit text." It produces intent; a backend renders it. (This also improves
testability *today* — assertions run against the spec, not templated bash.)

```go
// ServiceSpec is the init-system-agnostic description of one managed agent service.
type ServiceSpec struct {
    Name          string            // e.g. "conga-aaron"
    Description   string
    RunCmd        []string          // the `docker run ...` argv
    EnvFile       string            // path to the agent env file on the host
    After         []string          // ordering deps (docker, router, ...)
    Requires      []string
    Restart       RestartPolicy     // OnFailure | Always, with backoff
    Hooks         LifecycleHooks    // see below
    LogTarget     string            // file path or journald-equivalent
}

type LifecycleHooks struct {
    PreStart  []string // deploy config/$include, seed plugins, `docker rm -f`
    PostStart []string // apply egress iptables (built via pkg/provider/iptables)
    PostStop  []string // remove egress iptables
}

type HostSupervisor interface {
    DefineService(ctx context.Context, t Transport, spec ServiceSpec) error // render + install + enable
    Start(ctx context.Context, t Transport, name string) error
    Stop(ctx context.Context, t Transport, name string) error
    Restart(ctx context.Context, t Transport, name string) error
    Status(ctx context.Context, t Transport, name string) (ServiceState, error)
    RemoveService(ctx context.Context, t Transport, name string) error
}
```

**Boundary rule (the trap to avoid):** no systemd-ism (`ExecStartPost`, `WantedBy`, `daemon-reload`)
may appear in the engine or in `ServiceSpec`. Those live only inside `systemdSupervisor`.

## Backend mapping — systemd (built) and OpenRC (theoretical)

| `ServiceSpec` intent | systemd backend | OpenRC backend (future) |
|---|---|---|
| Service definition | `[Service] ExecStart=` unit file in `/etc/systemd/system/` | `/etc/init.d/<name>` script (`command=`, `command_args=`) |
| Start on boot | `WantedBy=multi-user.target` + `systemctl enable` | `rc-update add <name> default` |
| Crash restart | `Restart=always`, `RestartSec=` | `supervisor=supervise-daemon` + `respawn_*` (or s6/runit) |
| Ordering | `After=` / `Requires=` | `depend() { need docker; after conga-router; }` |
| PreStart hook | `ExecStartPre=` | init script `start_pre()` |
| PostStart hook (egress) | `ExecStartPost=` (runs `iptables.AddRulesCmd` output) | init script `start_post()` (same iptables cmds) |
| PostStop hook (cleanup) | `ExecStopPost=` | init script `stop_post()` |
| Logging | `StandardOutput=append:` / journald | redirect in `command_args` / `output_log=` |
| Reload after change | `systemctl daemon-reload` | none (script is read on next action) |

Most concepts map; crash-restart is the one that needs care (OpenRC needs `supervise-daemon` or an
s6/runit supervision tree — bare init scripts don't respawn). The `ServiceSpec` stays intent-level so
each backend satisfies `Restart` idiomatically; a backend that can't honor a field returns an error
rather than silently degrading a security-relevant guarantee.

## Backend selection

The provider chooses a supervisor at setup, by detection or explicit config:
- Detect: `command -v systemctl` → systemd; else `command -v rc-service` → OpenRC; else fail with an
  actionable "managed-host requires a supported init system" error.
- Or pin in the provider/remote config (`supervisor: systemd|openrc`).
Selection is recorded so refresh/restart use the same backend the host was provisioned with.

## Host bring-up differences (the small, contained part)

`installDocker` and host prep gain an Alpine branch:
- Docker: `apk add docker` + `rc-update add docker` + `rc-service docker start` (vs `apt/dnf` +
  `systemctl enable/start docker`).
- iptables: `apk add iptables` (the `DOCKER-USER` chain behaves identically once Docker is running).
These live behind the same supervisor/bootstrap seam, not in the engine.

## What this feature reserves now (the "stub")

- **Define** `HostSupervisor`, `ServiceSpec`, `LifecycleHooks`, `RestartPolicy` as real Go types.
- **Implement** exactly one backend: `systemdSupervisor`.
- **Reserve** the extension point: an `openrcSupervisor` stub whose methods return a sentinel
  `ErrUnsupportedSupervisor` ("OpenRC backend not implemented — see
  specs/2026-06-13_feature_managed-host-provisioning-engine/extension-host-supervisor.md"), plus a
  backend-selection switch that only knows systemd today but has the OpenRC case wired to the stub.
- **Do NOT** build OpenRC behavior, Alpine bring-up, or supervise-daemon logic now (YAGNI). The
  deliverable is the *seam + one backend + this doc*, so a future Alpine ask is purely additive.

## Adding a backend later (the recipe)

1. Implement `HostSupervisor` for the init system (render `ServiceSpec` → init artifacts).
2. Add the host bring-up branch (package manager + init enable) in setup.
3. Add detection/selection.
4. Map crash-restart honestly (supervision tree if needed) or return an error if unsupportable.
5. Reuse `pkg/provider/iptables` for the egress hook at the backend's post-start point.
6. Test: unit-test the `ServiceSpec`→artifact rendering; integration-test reboot survival + egress
   re-enforcement on a real host of that init system (mirror the systemd reboot test, criterion 5b).
