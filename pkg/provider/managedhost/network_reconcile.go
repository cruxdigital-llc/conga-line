package managedhost

import (
	"context"
	"fmt"
	"strings"
)

// ReconcileAgentNetwork ensures the per-agent bridge network conga-<name> exists
// on net.SubnetCIDR. It replaces the old single-shell-string migration with a
// transport-driven Go orchestration that is PREPARE-THEN-COMMIT and fail-safe:
//
//   - All potentially-blocking work (force-disconnecting foreign/dangling endpoints
//     so `docker network rm` can succeed) happens in PREPARE, BEFORE the agent
//     container is touched. If a foreign endpoint cannot be cleared (a persisted
//     Docker ghost that only a daemon restart would fix), it returns an actionable
//     error WITHOUT having stopped/removed the agent — so the agent keeps running
//     on its old network instead of being left down (the failure mode that took
//     congaline-team offline during the live migration).
//   - COMMIT (stop → remove agent + proxy → remove network → create) only runs once
//     the blockers are cleared, and is step-verified (the `network rm` must succeed
//     before `network create`).
//
// It is idempotent: a no-op when the subnet already matches, a create-only when the
// network is absent, and re-runnable after an abort. It never touches the agent's
// data directory.
//
// It returns migrated=true ONLY when it ran the destructive COMMIT (removed the
// agent + egress proxy and recreated the network) — i.e. the proxy is now gone and
// MUST be recreated by the caller's egress step. The caller should treat a
// subsequent egress-redeploy failure as fatal when migrated is true (the proxy is
// known-absent, not merely stale). No-op and create-only return migrated=false.
//
// The `{{range …}}` in the inspect commands are docker --format templates (literal
// braces in the emitted command), not Go templates.
func ReconcileAgentNetwork(ctx context.Context, t Transport, name string, net AgentNetwork) (migrated bool, err error) {
	netName := "conga-" + name
	proxyName := "conga-egress-" + name

	cur, err := t.RunCommand(ctx, fmt.Sprintf(
		"docker network inspect -f '{{range .IPAM.Config}}{{.Subnet}}{{end}}' %q 2>/dev/null || true", netName))
	if err != nil {
		return false, fmt.Errorf("inspect network %s: %w", netName, err)
	}
	cur = strings.TrimSpace(cur)

	switch cur {
	case net.SubnetCIDR:
		// Steady state — already on the deterministic subnet. No churn, no egress gap.
		return false, nil
	case "":
		// Network absent (defensive — the provision scripts create it). Create only;
		// the agent isn't attached to anything, so there's nothing to tear down.
		return false, createAgentNetwork(ctx, t, netName, net)
	}

	// Subnet mismatch (legacy auto-subnet agent) → migrate.
	//
	// PREPARE (non-destructive; an abort here leaves the agent running):
	// force-disconnect every FOREIGN endpoint — anything attached that is not the
	// agent container or its egress proxy (notably a stale conga-router bridge
	// endpoint; the router is --network host now, so its bridge endpoints are dead
	// weight). The agent + proxy are removed in COMMIT, so their endpoints are not
	// blockers and are left in place here.
	attached, err := listAttached(ctx, t, netName)
	if err != nil {
		return false, fmt.Errorf("list endpoints on %s: %w", netName, err)
	}
	for _, c := range attached {
		if c == netName || c == proxyName {
			continue
		}
		_, _ = t.RunCommand(ctx, fmt.Sprintf("docker network disconnect -f %q %q 2>/dev/null || true", netName, c))
	}
	// Verify no foreign endpoint survived the disconnect (a persisted ghost would).
	remaining, err := listAttached(ctx, t, netName)
	if err != nil {
		return false, fmt.Errorf("re-inspect endpoints on %s: %w", netName, err)
	}
	for _, c := range remaining {
		if c == netName || c == proxyName {
			continue
		}
		return false, fmt.Errorf(
			"network %s has an unclearable endpoint %q (likely a persisted Docker ghost); "+
				"left agent %s running on its old subnet — clear it with `systemctl restart docker` "+
				"in a maintenance window, then re-run `conga refresh %s`", netName, c, name, name)
	}

	// Flush DOCKER-USER rules keyed on the OLD agent IP (best-effort) so the
	// auto-subnet→static migration doesn't orphan rules the new unit's ExecStopPost
	// (which only knows the new static IP) would never remove.
	_, _ = t.RunCommand(ctx, fmt.Sprintf(
		`OLD_IP=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' %q 2>/dev/null || echo ""); `+
			`if [ -n "$OLD_IP" ]; then iptables -S DOCKER-USER 2>/dev/null | grep -- "-s $OLD_IP/" | sed 's/^-A /-D /' | while read -r r; do iptables $r 2>/dev/null || true; done; fi`,
		netName))

	// COMMIT (blockers cleared) — stop the unit so Restart=always can't race us,
	// remove the agent + proxy (their endpoints go with them), then remove + recreate
	// the network. This is the one brief window the agent is down.
	_, _ = t.RunCommand(ctx, fmt.Sprintf("systemctl stop %q 2>/dev/null || true", netName))
	_, _ = t.RunCommand(ctx, fmt.Sprintf("docker rm -f %q 2>/dev/null || true", netName))
	_, _ = t.RunCommand(ctx, fmt.Sprintf("docker rm -f %q 2>/dev/null || true", proxyName))

	// network rm is step-verified (no `|| true`): after the two rm's the only
	// endpoints that could remain are foreign ones — which PREPARE cleared — so this
	// succeeds in the normal case. If it unexpectedly fails (a foreign endpoint
	// reappeared, or a transient docker fault), abort; ReconcileAgentNetwork is
	// re-runnable and the next refresh completes once docker is healthy.
	if _, err := t.RunCommand(ctx, fmt.Sprintf("docker network rm %q", netName)); err != nil {
		return false, fmt.Errorf(
			"removing old network %s failed after clearing endpoints (unexpected): %w — "+
				"re-run `conga refresh %s` once docker is healthy", netName, err, name)
	}
	// Migrated: the agent + proxy were removed and the network recreated. The proxy
	// is now ABSENT and the caller's egress step must recreate it — so a subsequent
	// egress-redeploy failure should be treated as fatal (review #2).
	return true, createAgentNetwork(ctx, t, netName, net)
}

// createAgentNetwork creates the per-agent bridge network on its static subnet.
func createAgentNetwork(ctx context.Context, t Transport, netName string, net AgentNetwork) error {
	if _, err := t.RunCommand(ctx, fmt.Sprintf(
		"docker network create --driver bridge --subnet %q --gateway %q %q",
		net.SubnetCIDR, net.GatewayIP, netName)); err != nil {
		return fmt.Errorf("create network %s (%s): %w", netName, net.SubnetCIDR, err)
	}
	return nil
}

// listAttached returns the names of containers attached to a Docker network
// (empty if the network is absent).
func listAttached(ctx context.Context, t Transport, netName string) ([]string, error) {
	out, err := t.RunCommand(ctx, fmt.Sprintf(
		"docker network inspect -f '{{range .Containers}}{{.Name}} {{end}}' %q 2>/dev/null || true", netName))
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}
