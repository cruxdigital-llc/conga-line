package managedhost

import "fmt"

// AgentNetwork is the deterministic per-agent Docker bridge network plan. Assigning
// a known subnet + static IPs (instead of Docker's auto-assigned pool + runtime IP
// discovery) makes the egress iptables rule deterministic: it can be generated
// before the container starts, with no 10-retry `docker inspect` discovery loop and
// no race window where the agent runs with unfiltered egress (audit #7).
type AgentNetwork struct {
	SubnetCIDR string // e.g. "10.99.3.0/24"
	GatewayIP  string // .1 — the Docker bridge gateway
	AgentIP    string // .2 — the agent container; the egress iptables DROP source (pinned via `docker run --ip`)
	// ProxyIP (.3) is an ADVISORY reservation, not an enforced assignment: the
	// egress proxy is started with --network (no --ip) and is reached by the agent
	// through Docker DNS (conga-egress-<name>:3128), so it auto-assigns from the
	// subnet — landing on .3 in practice because it's (re)created after the agent
	// has already claimed .2. Reserved here to document the scheme and keep .3 free
	// should a future change pin it.
	ProxyIP string // .3 — reserved for the per-agent Envoy egress proxy (advisory)
}

// PlanAgentNetwork derives a collision-free per-agent network from the agent's
// unique host gateway port.
//
// The 10.99.<idx>.0/24 range is chosen to avoid collisions with BOTH:
//   - the AWS VPC CIDR (10.0.0.0/24 — only 10.0.0.x is in use), and
//   - Docker's default auto-assignment pool (172.16.0.0/12).
//
// Because we create the network with an explicit --subnet in this range, it cannot
// overlap the VPC or any network Docker auto-assigns from its 172.x pool. idx is the
// offset of the agent's unique host port from BaseGatewayPort, so each agent gets a
// distinct /24 for as long as its port is unique.
func PlanAgentNetwork(hostPort, baseGatewayPort int) (AgentNetwork, error) {
	idx := hostPort - baseGatewayPort
	if idx < 0 || idx > 255 {
		return AgentNetwork{}, fmt.Errorf(
			"host port %d yields subnet index %d, out of range 0..255 (base %d) — too many agents for the 10.99.x.0/24 scheme",
			hostPort, idx, baseGatewayPort)
	}
	return AgentNetwork{
		SubnetCIDR: fmt.Sprintf("10.99.%d.0/24", idx),
		GatewayIP:  fmt.Sprintf("10.99.%d.1", idx),
		AgentIP:    fmt.Sprintf("10.99.%d.2", idx),
		ProxyIP:    fmt.Sprintf("10.99.%d.3", idx),
	}, nil
}
