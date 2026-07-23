package managedhost

import (
	"context"
	"strings"
	"testing"
)

// demoNet is the deterministic plan for hostPort 18791 (idx 2 → 10.99.2.0/24).
func demoNet(t *testing.T) AgentNetwork {
	t.Helper()
	n, err := PlanAgentNetwork(18791, 18789)
	if err != nil {
		t.Fatalf("PlanAgentNetwork: %v", err)
	}
	return n
}

// cmdIndex returns the index of the first recorded command containing substr, or -1.
func cmdIndex(cmds []string, substr string) int {
	for i, c := range cmds {
		if strings.Contains(c, substr) {
			return i
		}
	}
	return -1
}

// TestReconcile_NoOpWhenSubnetMatches: a steady-state refresh (subnet already the
// deterministic value) must be a no-op — no stop, no rm, no recreate (no egress gap).
func TestReconcile_NoOpWhenSubnetMatches(t *testing.T) {
	ft := newFakeTransport()
	ft.responder = func(cmd string) (string, error) {
		if strings.Contains(cmd, "IPAM.Config") {
			return "10.99.2.0/24\n", nil
		}
		return "", nil
	}
	migrated, err := ReconcileAgentNetwork(context.Background(), ft, "demo", demoNet(t))
	if err != nil {
		t.Fatalf("expected no-op nil, got: %v", err)
	}
	if migrated {
		t.Error("no-op must report migrated=false")
	}
	for _, forbidden := range []string{"systemctl stop", "docker rm -f", "network rm", "network create"} {
		if ft.ranCmd(forbidden) {
			t.Errorf("no-op must not run %q; cmds=%v", forbidden, ft.cmds)
		}
	}
}

// TestReconcile_CreateOnlyWhenAbsent: if the network doesn't exist, just create it —
// nothing to tear down (no stop/rm/network-rm).
func TestReconcile_CreateOnlyWhenAbsent(t *testing.T) {
	ft := newFakeTransport()
	ft.responder = func(cmd string) (string, error) { return "", nil } // inspect → "" (absent)
	migrated, err := ReconcileAgentNetwork(context.Background(), ft, "demo", demoNet(t))
	if err != nil {
		t.Fatalf("create-only: %v", err)
	}
	if migrated {
		t.Error("create-only (no proxy torn down) must report migrated=false")
	}
	if !ft.ranCmd(`docker network create --driver bridge --subnet "10.99.2.0/24" --gateway "10.99.2.1" "conga-demo"`) {
		t.Errorf("expected the network to be created; cmds=%v", ft.cmds)
	}
	for _, forbidden := range []string{"systemctl stop", "docker rm -f", "network rm"} {
		if ft.ranCmd(forbidden) {
			t.Errorf("create-only must not run %q; cmds=%v", forbidden, ft.cmds)
		}
	}
}

// TestReconcile_FailSafeAbortOnGhost: a foreign endpoint (conga-router) that survives
// the disconnect (a persisted ghost) must ABORT — returning an actionable error and,
// critically, WITHOUT having stopped or removed the agent (the team-b failure
// mode: container removed before a blocked network rm → agent left down).
func TestReconcile_FailSafeAbortOnGhost(t *testing.T) {
	ft := newFakeTransport()
	ft.responder = func(cmd string) (string, error) {
		switch {
		case strings.Contains(cmd, "IPAM.Config"):
			return "172.18.0.0/16\n", nil // mismatch → migrate
		case strings.Contains(cmd, ".Containers"):
			return "conga-router conga-demo conga-egress-demo\n", nil // router persists on every list
		default:
			return "", nil
		}
	}
	migrated, err := ReconcileAgentNetwork(context.Background(), ft, "demo", demoNet(t))
	if err == nil {
		t.Fatal("expected an abort error when a foreign endpoint can't be cleared")
	}
	if migrated {
		t.Error("fail-safe abort must report migrated=false (no COMMIT ran)")
	}
	if !strings.Contains(err.Error(), "conga-router") || !strings.Contains(err.Error(), "unclearable") {
		t.Errorf("error should name the unclearable foreign endpoint; got: %v", err)
	}
	// It must have TRIED to disconnect the router before giving up.
	if !ft.ranCmd(`docker network disconnect -f "conga-demo" "conga-router"`) {
		t.Errorf("expected a force-disconnect attempt on the foreign endpoint; cmds=%v", ft.cmds)
	}
	// FAIL-SAFE: the agent must be untouched (still running on its old net).
	for _, destructive := range []string{"systemctl stop", `docker rm -f "conga-demo"`, "network rm", "network create"} {
		if ft.ranCmd(destructive) {
			t.Errorf("fail-safe abort must NOT run %q (agent must stay up); cmds=%v", destructive, ft.cmds)
		}
	}
}

// TestReconcile_HappyPathOrdering: subnet mismatch with a clearable foreign endpoint →
// PREPARE (disconnect router) then COMMIT (stop → rm agent → rm proxy → network rm →
// create), in that order, and the foreign disconnect precedes the destructive stop.
func TestReconcile_HappyPathOrdering(t *testing.T) {
	ft := newFakeTransport()
	listCalls := 0
	ft.responder = func(cmd string) (string, error) {
		switch {
		case strings.Contains(cmd, "IPAM.Config"):
			return "172.18.0.0/16\n", nil // mismatch
		case strings.Contains(cmd, ".Containers"):
			listCalls++
			if listCalls == 1 {
				return "conga-router conga-demo conga-egress-demo\n", nil // foreign present
			}
			return "conga-demo conga-egress-demo\n", nil // verify after disconnect → cleared
		default:
			return "", nil
		}
	}
	migrated, err := ReconcileAgentNetwork(context.Background(), ft, "demo", demoNet(t))
	if err != nil {
		t.Fatalf("happy path: %v", err)
	}
	if !migrated {
		t.Error("a successful migration (COMMIT ran, proxy torn down) must report migrated=true so the caller makes egress redeploy fatal (review #2)")
	}

	disc := cmdIndex(ft.cmds, `docker network disconnect -f "conga-demo" "conga-router"`)
	stop := cmdIndex(ft.cmds, `systemctl stop "conga-demo"`)
	rmAgent := cmdIndex(ft.cmds, `docker rm -f "conga-demo"`)
	rmProxy := cmdIndex(ft.cmds, `docker rm -f "conga-egress-demo"`)
	netRm := cmdIndex(ft.cmds, `docker network rm "conga-demo"`)
	netCreate := cmdIndex(ft.cmds, `docker network create --driver bridge --subnet "10.99.2.0/24"`)

	for name, idx := range map[string]int{"disconnect": disc, "stop": stop, "rm-agent": rmAgent, "rm-proxy": rmProxy, "network-rm": netRm, "network-create": netCreate} {
		if idx < 0 {
			t.Fatalf("missing expected command %q; cmds=%v", name, ft.cmds)
		}
	}
	// PREPARE (disconnect) strictly before COMMIT (stop), and COMMIT in order.
	if !(disc < stop && stop < rmAgent && rmAgent < netRm && netRm < netCreate) {
		t.Errorf("commands out of order: disconnect=%d stop=%d rmAgent=%d rmProxy=%d netRm=%d netCreate=%d\ncmds=%v",
			disc, stop, rmAgent, rmProxy, netRm, netCreate, ft.cmds)
	}
	// network rm must be step-verified (no `|| true` swallowing its failure).
	if ft.ranCmd(`docker network rm "conga-demo" 2>/dev/null`) || ft.ranCmd(`docker network rm "conga-demo" || true`) {
		t.Error("network rm must not swallow its error — it is the step-verify gate before create")
	}
}
