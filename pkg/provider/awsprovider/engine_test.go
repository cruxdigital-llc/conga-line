package awsprovider

import (
	"strings"
	"testing"

	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/provider/managedhost"
)

// TestBuildAgentServiceSpec_UnitEquivalence locks the B-2 swap: the Go-built
// systemd unit must carry every directive the retired refresh-user.sh unit did
// (so the production unit shape doesn't silently regress), plus the B-1
// deterministic static-IP egress, and must NOT reintroduce the in-place sed
// concerns (EnvironmentFile + -e KEY) or the discovery loop / router bridge.
func TestBuildAgentServiceSpec_UnitEquivalence(t *testing.T) {
	agent := provider.AgentConfig{
		Name:        "demo",
		Type:        provider.AgentTypeUser,
		GatewayPort: 18791, // -> subnet idx 2 -> 10.99.2.0/24, agent .2
	}
	const image = "ghcr.io/openclaw/openclaw:2026.6.5"

	spec, net, err := buildAgentServiceSpec(agent, image, "us-east-2")
	if err != nil {
		t.Fatalf("buildAgentServiceSpec: %v", err)
	}
	if net.AgentIP != "10.99.2.2" {
		t.Fatalf("unexpected agent IP %q (want 10.99.2.2)", net.AgentIP)
	}
	unit := managedhost.RenderSystemdUnit(spec)

	mustContain := []string{
		"Description=Conga Gateway (demo)",
		"After=docker.service conga-router.service conga-image-refresh.service",
		"Requires=docker.service",
		"Type=simple",
		// PreStart order: behavior sync, fail-closed reserved-key guard, stale-rm,
		// plugin seed.
		"ExecStartPre=/opt/conga/bin/pre-start.sh demo us-east-2",
		"ExecStartPre=/opt/conga/bin/reserved-key-guard-demo.sh",
		"ExecStartPre=-/usr/bin/docker rm -f conga-demo",
		"openclaw plugins install @openclaw/slack",
		// ExecStart: static IP, host->container port map, --env-file, egress proxy,
		// and the NODE_OPTIONS override double-quoted as one systemd arg.
		"ExecStart=/usr/bin/docker run --name conga-demo --network conga-demo --ip 10.99.2.2 -p 127.0.0.1:18791:18789",
		"--env-file /opt/conga/config/demo.env",
		"-e HTTPS_PROXY=http://conga-egress-demo:3128",
		`-e "NODE_OPTIONS=--max-old-space-size=1536 --require /opt/proxy-bootstrap.js"`,
		image,
		// Deterministic egress iptables (audit #7), literal IP, no discovery loop.
		"ExecStartPost=-/bin/bash -c 'iptables -C DOCKER-USER -s 10.99.2.2 -j DROP",
		"ExecStopPost=-/bin/bash -c 'iptables -D DOCKER-USER -s 10.99.2.2",
		"ExecStop=/usr/bin/docker stop conga-demo",
		"StandardOutput=append:/var/log/conga-demo.log",
		"Restart=always",
		"RestartSec=10",
		// Bounded restart so a fail-closed-guard rejection lands in `failed` rather
		// than looping forever (PR #67 review #3).
		"StartLimitIntervalSec=300",
		"StartLimitBurst=5",
		// R4: generous start timeout so a flock-serialized pre-start.sh under a
		// simultaneous fleet start doesn't trip the unit's start timeout.
		"TimeoutStartSec=300",
		"WantedBy=multi-user.target",
	}
	for _, want := range mustContain {
		if !strings.Contains(unit, want) {
			t.Errorf("rendered unit missing directive:\n  %s\n--- unit ---\n%s", want, unit)
		}
	}

	mustNotContain := map[string]string{
		// Secrets travel via --env-file, not EnvironmentFile= or -e KEY (audit/#9627).
		"EnvironmentFile=": "EnvironmentFile=",
		// The 10-retry discovery loop + runtime IP inspection are retired (audit #7).
		"discovery loop (seq)":     "seq 1 10",
		"runtime IP inspect":       "NetworkSettings.Networks",
		"in-place sed on the unit": "sed -i",
		"router bridge attach":     "docker network connect",
		"inlined slack secret":     "SLACK_BOT_TOKEN=",
	}
	for desc, marker := range mustNotContain {
		if strings.Contains(unit, marker) {
			t.Errorf("rendered unit must not contain %s (%q)\n--- unit ---\n%s", desc, marker, unit)
		}
	}
}

// NOTE: the shell-string agentNetworkMigrationCmd was replaced in R2 by the
// prepare-then-commit Go orchestration managedhost.ReconcileAgentNetwork, which is
// unit-tested (no-op / fail-safe-abort / happy-path ordering) in
// pkg/provider/managedhost/network_reconcile_test.go against the fake transport.

// TestBuildAgentServiceSpec_PortOutOfRange ensures the network planner's range
// guard surfaces as a build error rather than a bad unit.
func TestBuildAgentServiceSpec_PortOutOfRange(t *testing.T) {
	agent := provider.AgentConfig{Name: "x", Type: provider.AgentTypeUser, GatewayPort: 99999}
	if _, _, err := buildAgentServiceSpec(agent, "img", "us-east-2"); err == nil {
		t.Error("expected an error for an out-of-range gateway port")
	}
}

// TestBuildAgentServiceSpec_ReservedKeyGuardWiring locks the B-3 integrity gate:
// the reserved-key guard runs as a PreStart AFTER pre-start.sh (so it inspects the
// just-synced $include layers) and BEFORE the container is created, and it is
// fail-closed (no leading "-", so a non-zero exit aborts the start).
func TestBuildAgentServiceSpec_ReservedKeyGuardWiring(t *testing.T) {
	agent := provider.AgentConfig{Name: "demo", Type: provider.AgentTypeUser, GatewayPort: 18791}
	spec, _, err := buildAgentServiceSpec(agent, "img", "us-east-2")
	if err != nil {
		t.Fatalf("buildAgentServiceSpec: %v", err)
	}

	var guardIdx, syncIdx, rmIdx = -1, -1, -1
	for i, h := range spec.Hooks.PreStart {
		switch {
		case strings.Contains(h, "reserved-key-guard-demo.sh"):
			guardIdx = i
			// Fail-closed: the guard hook must NOT be best-effort ("-" prefix).
			if strings.HasPrefix(h, "-") {
				t.Errorf("reserved-key guard must be fail-closed (no leading '-'): %q", h)
			}
		case strings.Contains(h, "pre-start.sh"):
			syncIdx = i
		case strings.Contains(h, "docker rm -f"):
			rmIdx = i
		}
	}
	if guardIdx < 0 {
		t.Fatal("reserved-key guard not wired as a PreStart hook")
	}
	if !(syncIdx >= 0 && syncIdx < guardIdx) {
		t.Errorf("guard (idx %d) must run AFTER pre-start.sh (idx %d) so it checks synced includes", guardIdx, syncIdx)
	}
	if !(rmIdx >= 0 && guardIdx < rmIdx) {
		t.Errorf("guard (idx %d) must run BEFORE the container is created (docker rm -f, idx %d)", guardIdx, rmIdx)
	}
}
