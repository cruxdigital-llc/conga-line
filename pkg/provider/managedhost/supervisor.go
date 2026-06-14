package managedhost

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrUnsupportedSupervisor is returned by reserved-but-unimplemented HostSupervisor
// backends (e.g. OpenRC). See
// specs/2026-06-13_feature_managed-host-provisioning-engine/extension-host-supervisor.md.
var ErrUnsupportedSupervisor = errors.New("host supervisor backend not implemented — see specs/2026-06-13_feature_managed-host-provisioning-engine/extension-host-supervisor.md")

// RestartPolicy describes how the supervisor restarts a crashed service.
type RestartPolicy struct {
	Mode     string // "always" | "on-failure" (default "always")
	DelaySec int    // seconds between restarts (0 = backend default)
	// StartLimitIntervalSec + StartLimitBurst bound the restart loop: more than
	// Burst starts within Interval seconds drives the unit to a terminal failed
	// state instead of restarting forever. Both must be > 0 to take effect; 0
	// leaves the backend default (unbounded). A backend maps these to its native
	// rate-limit (systemd StartLimitIntervalSec/StartLimitBurst in [Unit]).
	StartLimitIntervalSec int
	StartLimitBurst       int
}

// LifecycleHooks are commands run around the main process. The engine populates
// these provider-agnostically; each backend maps them to its native mechanism
// (systemd ExecStartPre/Post/StopPost; OpenRC start_pre/start_post/stop_post).
type LifecycleHooks struct {
	PreStart  []string // before start: reserved-key guard, plugin seed, `docker rm -f`
	PostStart []string // after start: apply egress iptables (deterministic static IP)
	PostStop  []string // after stop: remove egress iptables
}

// ServiceSpec is the init-system-agnostic description of one managed agent service.
// The engine emits a ServiceSpec; a HostSupervisor renders it. The boundary rule:
// no systemd-ism (ExecStartPost, WantedBy, …) may appear in ServiceSpec — those
// live only inside a backend.
type ServiceSpec struct {
	Name        string // e.g. "conga-aaron"
	Description string
	RunCmd      string // the full foreground run command (e.g. `docker run … <image>`)
	StopCmd     string // optional stop command (e.g. `docker stop conga-aaron`)
	EnvFile     string // optional env file path
	After       []string
	Requires    []string
	Restart     RestartPolicy
	Hooks       LifecycleHooks
	LogTarget   string // file path for stdout/stderr (empty = backend default)
	// StartTimeoutSec bounds how long the backend waits for start (incl. PreStart).
	// 0 = backend default. Set generously when PreStart does slow/serialized work
	// (e.g. a flock'd S3 sync under a simultaneous fleet start — R4).
	StartTimeoutSec int
}

// ServiceState is a backend-agnostic service status.
type ServiceState struct {
	Enabled bool
	Active  bool
	Raw     string
}

// HostSupervisor renders + manages a ServiceSpec on a host via a Transport. systemd
// is the only built backend; OpenRC is reserved (ErrUnsupportedSupervisor) so a
// future lightweight-host (Alpine) scenario is additive (extension-host-supervisor.md).
type HostSupervisor interface {
	DefineService(ctx context.Context, t Transport, spec ServiceSpec) error
	Start(ctx context.Context, t Transport, name string) error
	Stop(ctx context.Context, t Transport, name string) error
	Restart(ctx context.Context, t Transport, name string) error
	RemoveService(ctx context.Context, t Transport, name string) error
	Status(ctx context.Context, t Transport, name string) (ServiceState, error)
}

const systemdUnitDir = "/etc/systemd/system"

type systemdSupervisor struct{}

// NewSystemdSupervisor returns the systemd HostSupervisor backend.
func NewSystemdSupervisor() HostSupervisor { return &systemdSupervisor{} }

// RenderSystemdUnit renders a ServiceSpec to systemd unit text without a host or
// Transport. Exported so callers can assert the exact unit a spec produces (e.g.
// equivalence against the bash unit it replaced) and for effective-config views.
func RenderSystemdUnit(spec ServiceSpec) string {
	return (&systemdSupervisor{}).RenderUnit(spec)
}

func (s *systemdSupervisor) unitPath(name string) string {
	return fmt.Sprintf("%s/%s.service", systemdUnitDir, name)
}

// RenderUnit renders a ServiceSpec to systemd unit text. Exported so tests can
// assert the unit shape (and equivalence against the refresh-user.sh unit it
// replaces) without a host.
func (s *systemdSupervisor) RenderUnit(spec ServiceSpec) string {
	var b strings.Builder
	b.WriteString("[Unit]\n")
	fmt.Fprintf(&b, "Description=%s\n", spec.Description)
	if len(spec.After) > 0 {
		fmt.Fprintf(&b, "After=%s\n", strings.Join(spec.After, " "))
	}
	if len(spec.Requires) > 0 {
		fmt.Fprintf(&b, "Requires=%s\n", strings.Join(spec.Requires, " "))
	}
	// Start-rate limit lives in [Unit]: exceeding it drives the unit to `failed`
	// rather than looping (e.g. a persistent fail-closed-guard rejection).
	if spec.Restart.StartLimitIntervalSec > 0 {
		fmt.Fprintf(&b, "StartLimitIntervalSec=%d\n", spec.Restart.StartLimitIntervalSec)
	}
	if spec.Restart.StartLimitBurst > 0 {
		fmt.Fprintf(&b, "StartLimitBurst=%d\n", spec.Restart.StartLimitBurst)
	}
	b.WriteString("\n[Service]\nType=simple\n")
	if spec.EnvFile != "" {
		fmt.Fprintf(&b, "EnvironmentFile=%s\n", spec.EnvFile)
	}
	for _, c := range spec.Hooks.PreStart {
		fmt.Fprintf(&b, "ExecStartPre=%s\n", c)
	}
	fmt.Fprintf(&b, "ExecStart=%s\n", spec.RunCmd)
	for _, c := range spec.Hooks.PostStart {
		fmt.Fprintf(&b, "ExecStartPost=%s\n", c)
	}
	for _, c := range spec.Hooks.PostStop {
		fmt.Fprintf(&b, "ExecStopPost=%s\n", c)
	}
	if spec.StopCmd != "" {
		fmt.Fprintf(&b, "ExecStop=%s\n", spec.StopCmd)
	}
	if spec.LogTarget != "" {
		fmt.Fprintf(&b, "StandardOutput=append:%s\nStandardError=append:%s\n", spec.LogTarget, spec.LogTarget)
	}
	mode := spec.Restart.Mode
	if mode == "" {
		mode = "always"
	}
	fmt.Fprintf(&b, "Restart=%s\n", mode)
	if spec.Restart.DelaySec > 0 {
		fmt.Fprintf(&b, "RestartSec=%d\n", spec.Restart.DelaySec)
	}
	startTimeout := spec.StartTimeoutSec
	if startTimeout <= 0 {
		startTimeout = 120 // backend default
	}
	fmt.Fprintf(&b, "TimeoutStartSec=%d\nTimeoutStopSec=30\n", startTimeout)
	b.WriteString("\n[Install]\nWantedBy=multi-user.target\n")
	return b.String()
}

func (s *systemdSupervisor) DefineService(ctx context.Context, t Transport, spec ServiceSpec) error {
	if err := t.PutFile(ctx, s.unitPath(spec.Name), []byte(s.RenderUnit(spec)), 0o644); err != nil {
		return fmt.Errorf("write unit %s: %w", spec.Name, err)
	}
	// daemon-reload + enable so the unit survives a host reboot. (Slice 2b's live
	// test caught a real regression when enable was missing — see refresh-user.sh.)
	if _, err := t.RunCommand(ctx, fmt.Sprintf("systemctl daemon-reload && systemctl enable %s", spec.Name)); err != nil {
		return fmt.Errorf("daemon-reload + enable %s: %w", spec.Name, err)
	}
	return nil
}

func (s *systemdSupervisor) Start(ctx context.Context, t Transport, name string) error {
	_, err := t.RunCommand(ctx, "systemctl start "+name)
	return err
}

func (s *systemdSupervisor) Stop(ctx context.Context, t Transport, name string) error {
	_, err := t.RunCommand(ctx, "systemctl stop "+name)
	return err
}

func (s *systemdSupervisor) Restart(ctx context.Context, t Transport, name string) error {
	_, err := t.RunCommand(ctx, "systemctl restart "+name)
	return err
}

func (s *systemdSupervisor) RemoveService(ctx context.Context, t Transport, name string) error {
	_, err := t.RunCommand(ctx, fmt.Sprintf(
		"systemctl disable --now %s 2>/dev/null || true; rm -f %s; systemctl daemon-reload",
		name, s.unitPath(name)))
	return err
}

func (s *systemdSupervisor) Status(ctx context.Context, t Transport, name string) (ServiceState, error) {
	out, err := t.RunCommand(ctx, fmt.Sprintf(
		"printf 'enabled=%%s active=%%s' \"$(systemctl is-enabled %s 2>/dev/null)\" \"$(systemctl is-active %s 2>/dev/null)\"",
		name, name))
	if err != nil {
		return ServiceState{}, err
	}
	return ServiceState{
		Enabled: strings.Contains(out, "enabled=enabled"),
		Active:  strings.Contains(out, "active=active"),
		Raw:     strings.TrimSpace(out),
	}, nil
}

// openrcSupervisor is the reserved (unimplemented) OpenRC backend. Building it out
// is the documented path to supporting non-systemd lightweight hosts (Alpine).
type openrcSupervisor struct{}

// NewOpenRCSupervisor returns the reserved OpenRC backend stub. All methods return
// ErrUnsupportedSupervisor until implemented (extension-host-supervisor.md).
func NewOpenRCSupervisor() HostSupervisor { return &openrcSupervisor{} }

func (o *openrcSupervisor) DefineService(context.Context, Transport, ServiceSpec) error {
	return ErrUnsupportedSupervisor
}
func (o *openrcSupervisor) Start(context.Context, Transport, string) error {
	return ErrUnsupportedSupervisor
}
func (o *openrcSupervisor) Stop(context.Context, Transport, string) error {
	return ErrUnsupportedSupervisor
}
func (o *openrcSupervisor) Restart(context.Context, Transport, string) error {
	return ErrUnsupportedSupervisor
}
func (o *openrcSupervisor) RemoveService(context.Context, Transport, string) error {
	return ErrUnsupportedSupervisor
}
func (o *openrcSupervisor) Status(context.Context, Transport, string) (ServiceState, error) {
	return ServiceState{}, ErrUnsupportedSupervisor
}
