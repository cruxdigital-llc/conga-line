package awsprovider

import (
	"context"
	"fmt"

	awsutil "github.com/cruxdigital-llc/conga-line/pkg/aws"
	"github.com/cruxdigital-llc/conga-line/pkg/common"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/provider/iptables"
	"github.com/cruxdigital-llc/conga-line/pkg/provider/managedhost"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime/openclaw"
)

// defineAndStartAgentService is the Go managed-host engine path that replaced
// refresh-user.sh's bash unit generation (managed-host provisioning engine,
// slices 4+3 increment B-2). It builds the agent's docker-run command + systemd
// ServiceSpec entirely in Go (the shared managedhost builder + supervisor), so
// the unit can no longer drift from the run command, the egress hooks, or the
// reserved-key posture — and there is no in-place `sed` on the unit (audit #8).
//
// It assumes the per-agent config (openclaw.json + $include layers + the .env
// file) is already on the host — RefreshAgent's regenerateAgentConfigOnInstance
// writes them in Go just before this runs, so the container's --env-file target
// exists with current secrets.
//
// Ordering makes partial failure safe and the egress race impossible: the
// network is reconciled to its deterministic static subnet, the unit (carrying
// the static-IP egress iptables in its ExecStartPost/StopPost) is written +
// enabled, and only then is the service (re)started — so an agent is either
// started-with-egress or not started, never running unfiltered.
func (p *AWSProvider) defineAndStartAgentService(ctx context.Context, instanceID string, agent provider.AgentConfig) error {
	image, err := awsutil.GetParameter(ctx, p.clients.SSM, "/conga/config/image")
	if err != nil {
		return fmt.Errorf("failed to resolve container image: %w", err)
	}

	spec, net, err := buildAgentServiceSpec(agent, image, p.region)
	if err != nil {
		return err
	}
	containerName := spec.Name
	t := p.transport(instanceID)

	// Deploy the reserved-key guard the unit's PreStart runs (integrity decision
	// #2, prevention-first): a tiny generated script that fail-closes the start if
	// any $include layer declares a Conga-owned key (channels / gateway / plugins /
	// a nested $include). Generated from common.ReservedCustomConfigKeys so it can't
	// drift from the Go validator; 0755 so systemd can exec it.
	guard := managedhost.ReservedKeyGuardScript(agentIncludePaths(agent.Name))
	if err := t.PutFile(ctx, reservedKeyGuardPath(agent.Name), []byte(guard), 0o755); err != nil {
		return fmt.Errorf("failed to deploy reserved-key guard for %s: %w", agent.Name, err)
	}

	// 1) Reconcile the per-agent network to its deterministic static subnet. On a
	//    mismatch (existing auto-subnet agent, or fresh add-user auto-subnet) this
	//    stops the unit first so Restart=always can't race the network rm, then
	//    recreates the net + frees the proxy (DeployEgress re-creates it). On a
	//    steady-state refresh it's a no-op, so there's no egress gap.
	if _, err := t.RunCommand(ctx, agentNetworkMigrationCmd(containerName, net)); err != nil {
		return fmt.Errorf("failed to reconcile network for %s: %w", agent.Name, err)
	}

	// 2) Define + enable the unit (write + daemon-reload + enable — survives reboot).
	sup := managedhost.NewSystemdSupervisor()
	if err := sup.DefineService(ctx, t, spec); err != nil {
		return fmt.Errorf("failed to define service for %s: %w", agent.Name, err)
	}

	// 3) (Re)start so the new unit takes effect on the (possibly migrated) network.
	//    Restart starts a not-yet-running unit too. The unit's ExecStartPost applies
	//    egress iptables synchronously, so on return the agent is filtered.
	if err := sup.Restart(ctx, t, containerName); err != nil {
		return fmt.Errorf("failed to start service for %s: %w", agent.Name, err)
	}
	return nil
}

// buildAgentServiceSpec assembles the systemd ServiceSpec + network plan for an
// agent. Pure (no AWS calls) so the produced unit can be asserted in tests
// against the bash unit it replaced. The managed host always runs a per-agent
// Envoy egress proxy (created at provision, maintained by DeployEgress), so the
// container is always wired to route HTTP(S) through it + load the
// proxy-bootstrap shim; iptables enforces in all modes regardless of the Envoy
// mode (egress-controls.md).
func buildAgentServiceSpec(agent provider.AgentConfig, image, region string) (managedhost.ServiceSpec, managedhost.AgentNetwork, error) {
	net, err := managedhost.PlanAgentNetwork(agent.GatewayPort, common.BaseGatewayPort)
	if err != nil {
		return managedhost.ServiceSpec{}, net, fmt.Errorf("failed to plan network for %s: %w", agent.Name, err)
	}
	addCmd, err := iptables.AddRulesCmd(net.AgentIP, net.SubnetCIDR)
	if err != nil {
		return managedhost.ServiceSpec{}, net, fmt.Errorf("failed to build egress add rule for %s: %w", agent.Name, err)
	}
	removeCmd, err := iptables.RemoveRulesCmd(net.AgentIP, net.SubnetCIDR)
	if err != nil {
		return managedhost.ServiceSpec{}, net, fmt.Errorf("failed to build egress remove rule for %s: %w", agent.Name, err)
	}

	name := agent.Name
	containerName := "conga-" + name
	dataDir := fmt.Sprintf("/opt/conga/data/%s", name)

	container := managedhost.AgentContainer{
		Name:               containerName,
		Image:              image,
		Network:            containerName,
		IP:                 net.AgentIP,
		HostPort:           agent.GatewayPort,
		ContainerPort:      openclaw.ContainerPort,
		DataDir:            dataDir,
		EnvFile:            fmt.Sprintf("/opt/conga/config/%s.env", name),
		MemoryLimit:        "2g",
		CPULimit:           "0.75",
		PidsLimit:          256,
		User:               "1000:1000",
		EgressProxyName:    "conga-egress-" + name,
		ProxyBootstrapPath: "/opt/conga/config/proxy-bootstrap.js",
	}

	spec := managedhost.ServiceSpec{
		Name:        containerName,
		Description: fmt.Sprintf("Conga Gateway (%s)", name),
		RunCmd:      managedhost.SystemdExecStart(container.Args()),
		StopCmd:     fmt.Sprintf("/usr/bin/docker stop %s", containerName),
		// No EnvFile here: secrets reach the container via `docker run --env-file`
		// (in RunCmd), not via a systemd EnvironmentFile= directive.
		After:    []string{"docker.service", "conga-router.service", "conga-image-refresh.service"},
		Requires: []string{"docker.service"},
		Restart:  managedhost.RestartPolicy{Mode: "always", DelaySec: 10},
		Hooks: managedhost.LifecycleHooks{
			PreStart: []string{
				// Behavior-file sync (host helper). Must succeed before start. Runs
				// FIRST so the reserved-key guard below checks the just-synced
				// $include layers (pre-start.sh re-deploys fleet/managed layers).
				fmt.Sprintf("/opt/conga/bin/pre-start.sh %s %s", name, region),
				// Fail-closed reserved-key guard (NO leading "-"): if any $include
				// layer declares a Conga-owned key, exit non-zero → systemd aborts the
				// start, so an allowlist escalation never takes effect (integrity #2).
				reservedKeyGuardPath(name),
				// Clear any stale container so the static --ip is free to rebind.
				fmt.Sprintf("-/usr/bin/docker rm -f %s", containerName),
				// Seed the @openclaw/slack external plugin into the data dir
				// (extracted from the core image in v2026.5.x). Idempotent: the
				// install short-circuits when already on disk. Best-effort.
				fmt.Sprintf("-/usr/bin/docker run --rm --user 1000:1000 -v %s:/home/node/.openclaw:rw %s openclaw plugins install @openclaw/slack", dataDir, image),
			},
			// Deterministic static-IP egress iptables (audit #7) — generated in Go,
			// applied by systemd on every (re)start incl. reboot, before the agent
			// does any work. Best-effort so a transient iptables hiccup can't wedge
			// the unit; a re-run / the periodic backstop re-converge.
			PostStart: []string{fmt.Sprintf("-/bin/bash -c '%s'", addCmd)},
			PostStop:  []string{fmt.Sprintf("-/bin/bash -c '%s'", removeCmd)},
		},
		LogTarget: fmt.Sprintf("/var/log/conga-%s.log", name),
	}
	return spec, net, nil
}

// agentNetworkMigrationCmd returns a short shell command that ensures the
// per-agent Docker network exists with the deterministic subnet, recreating it
// (and detaching the agent + egress proxy first) only when the current subnet
// doesn't match. The `{{range .IPAM.Config}}…` is a docker --format template, not
// a Go template — these are literal braces in the emitted command.
func agentNetworkMigrationCmd(containerName string, net managedhost.AgentNetwork) string {
	proxyName := "conga-egress-" + containerName[len("conga-"):]
	return fmt.Sprintf(
		`NET=%[1]q; DESIRED=%[2]q; `+
			`CUR=$(docker network inspect -f '{{range .IPAM.Config}}{{.Subnet}}{{end}}' "$NET" 2>/dev/null || echo ""); `+
			`if [ "$CUR" != "$DESIRED" ]; then `+
			`echo "Migrating $NET subnet '${CUR:-<none>}' -> '$DESIRED' (deterministic static-IP egress)"; `+
			`systemctl stop %[1]q 2>/dev/null || true; `+
			`docker rm -f %[1]q 2>/dev/null || true; `+
			`docker rm -f %[3]q 2>/dev/null || true; `+
			`docker network rm "$NET" 2>/dev/null || true; `+
			`docker network create --driver bridge --subnet "$DESIRED" --gateway %[4]q "$NET"; `+
			`fi`,
		containerName, net.SubnetCIDR, proxyName, net.GatewayIP)
}

// reservedKeyGuardPath is where the per-agent fail-closed reserved-key guard
// script lives on the host (alongside pre-start.sh, root-owned, 0755).
func reservedKeyGuardPath(name string) string {
	return fmt.Sprintf("/opt/conga/bin/reserved-key-guard-%s.sh", name)
}

// agentIncludePaths returns the $include layer files the guard inspects: the
// admin-editable layer plus the two Conga-managed declarative layers (#31), all
// in the agent's data dir — the same files regenerateAgentConfigOnInstance writes.
func agentIncludePaths(name string) []string {
	dataDir := fmt.Sprintf("/opt/conga/data/%s", name)
	return []string{
		dataDir + "/agent-custom.json",
		dataDir + "/fleet-custom.json",
		dataDir + "/agent-managed-custom.json",
	}
}
