package managedhost

import (
	"context"
	"strings"
	"testing"

	"github.com/cruxdigital-llc/conga-line/pkg/common"
)

func TestPlanAgentNetwork(t *testing.T) {
	const base = 18789

	n, err := PlanAgentNetwork(18789, base) // idx 0
	if err != nil {
		t.Fatalf("idx0: %v", err)
	}
	if n.SubnetCIDR != "10.99.0.0/24" || n.GatewayIP != "10.99.0.1" || n.AgentIP != "10.99.0.2" || n.ProxyIP != "10.99.0.3" {
		t.Errorf("idx0 plan = %+v", n)
	}

	n3, _ := PlanAgentNetwork(18792, base) // idx 3
	if n3.SubnetCIDR != "10.99.3.0/24" || n3.AgentIP != "10.99.3.2" {
		t.Errorf("idx3 plan = %+v", n3)
	}

	// Distinct (unique) ports must yield distinct subnets (collision-free).
	a, _ := PlanAgentNetwork(18791, base)
	b, _ := PlanAgentNetwork(18795, base)
	if a.SubnetCIDR == b.SubnetCIDR {
		t.Errorf("distinct ports produced the same subnet: %s", a.SubnetCIDR)
	}

	// Never collide with the VPC range (10.0.0.0/24).
	for _, p := range []int{18789, 18900, 19044} {
		nn, _ := PlanAgentNetwork(p, base)
		if strings.HasPrefix(nn.SubnetCIDR, "10.0.0.") {
			t.Errorf("port %d subnet %s collides with VPC 10.0.0.0/24", p, nn.SubnetCIDR)
		}
	}

	if _, err := PlanAgentNetwork(18789+256, base); err == nil {
		t.Error("expected out-of-range error for idx > 255")
	}
	if _, err := PlanAgentNetwork(18788, base); err == nil {
		t.Error("expected error for a port below base")
	}
}

func TestReservedKeyGuardScript(t *testing.T) {
	script := ReservedKeyGuardScript([]string{"/data/fleet-custom.json", "/data/agent-custom.json"})

	// Every reserved key (the single source of truth) must appear in the guard's
	// grep alternation — regex-quoted for "$include".
	for _, k := range common.ReservedCustomConfigKeys {
		marker := k
		if k == "$include" {
			marker = `\$include`
		}
		if !strings.Contains(script, marker) {
			t.Errorf("guard missing reserved key %q (expected marker %q)", k, marker)
		}
	}
	for _, p := range []string{"/data/fleet-custom.json", "/data/agent-custom.json"} {
		if !strings.Contains(script, p) {
			t.Errorf("guard must check include path %q", p)
		}
	}
	if !strings.Contains(script, "exit 1") {
		t.Error("guard must fail closed (exit 1) when a reserved key is found")
	}
	if !strings.Contains(script, "WARN:") {
		t.Error("guard must WARN + allow (not fail) on an unparseable JSON5 include")
	}
}

func TestSystemdRenderUnit(t *testing.T) {
	s := &systemdSupervisor{}
	spec := ServiceSpec{
		Name:        "conga-test",
		Description: "Conga Gateway (test)",
		RunCmd:      "/usr/bin/docker run --name conga-test --ip 10.99.0.2 img",
		StopCmd:     "/usr/bin/docker stop conga-test",
		EnvFile:     "/opt/conga/config/test.env",
		After:       []string{"docker.service", "conga-router.service"},
		Requires:    []string{"docker.service"},
		Restart:     RestartPolicy{Mode: "always", DelaySec: 10},
		LogTarget:   "/var/log/conga-test.log",
		Hooks: LifecycleHooks{
			PreStart:  []string{"/opt/conga/bin/guard.sh conga-test", "-/usr/bin/docker rm -f conga-test"},
			PostStart: []string{"/sbin/iptables-apply"},
			PostStop:  []string{"/sbin/iptables-remove"},
		},
	}
	u := s.RenderUnit(spec)

	for _, w := range []string{
		"Description=Conga Gateway (test)",
		"After=docker.service conga-router.service",
		"Requires=docker.service",
		"EnvironmentFile=/opt/conga/config/test.env",
		"ExecStartPre=/opt/conga/bin/guard.sh conga-test",
		"ExecStartPre=-/usr/bin/docker rm -f conga-test",
		"ExecStart=/usr/bin/docker run --name conga-test --ip 10.99.0.2 img",
		"ExecStartPost=/sbin/iptables-apply",
		"ExecStopPost=/sbin/iptables-remove",
		"ExecStop=/usr/bin/docker stop conga-test",
		"StandardOutput=append:/var/log/conga-test.log",
		"Restart=always",
		"RestartSec=10",
		"WantedBy=multi-user.target",
	} {
		if !strings.Contains(u, w) {
			t.Errorf("rendered unit missing %q:\n%s", w, u)
		}
	}

	// Hook ordering: the guard PreStart must precede ExecStart, which precedes PostStart.
	pre := strings.Index(u, "ExecStartPre=/opt/conga/bin/guard.sh")
	start := strings.Index(u, "ExecStart=/usr/bin/docker run")
	post := strings.Index(u, "ExecStartPost=/sbin/iptables-apply")
	if !(pre < start && start < post) {
		t.Errorf("hook ordering wrong: pre=%d start=%d post=%d", pre, start, post)
	}
}

func TestSystemdDefineServiceEnablesUnit(t *testing.T) {
	ft := newFakeTransport()
	s := NewSystemdSupervisor()
	if err := s.DefineService(context.Background(), ft, ServiceSpec{Name: "conga-test", Description: "x", RunCmd: "docker run img"}); err != nil {
		t.Fatalf("DefineService: %v", err)
	}
	if _, ok := ft.files["/etc/systemd/system/conga-test.service"]; !ok {
		t.Errorf("unit file not written; files=%v", keys(ft.files))
	}
	// daemon-reload + enable — the reboot-survival guarantee (slice 2b's live test
	// caught a regression when enable was missing).
	joined := strings.Join(ft.cmds, "\n")
	if !strings.Contains(joined, "daemon-reload") || !strings.Contains(joined, "systemctl enable conga-test") {
		t.Errorf("DefineService must daemon-reload + enable the unit; cmds=%v", ft.cmds)
	}
}

func TestOpenRCSupervisorIsReserved(t *testing.T) {
	o := NewOpenRCSupervisor()
	ctx := context.Background()
	if err := o.DefineService(ctx, newFakeTransport(), ServiceSpec{Name: "x"}); err != ErrUnsupportedSupervisor {
		t.Errorf("OpenRC DefineService: want ErrUnsupportedSupervisor, got %v", err)
	}
	if _, err := o.Status(ctx, newFakeTransport(), "x"); err != ErrUnsupportedSupervisor {
		t.Errorf("OpenRC Status: want ErrUnsupportedSupervisor, got %v", err)
	}
}
