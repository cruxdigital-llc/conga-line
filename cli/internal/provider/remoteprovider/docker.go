package remoteprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cruxdigital-llc/conga-line/cli/internal/provider/iptables"
)

// dockerRun executes a docker command on the remote host via SSH.
func (p *RemoteProvider) dockerRun(ctx context.Context, args ...string) (string, error) {
	cmd := "docker " + shelljoin(args...)
	return p.ssh.Run(ctx, cmd)
}

// dockerCheck verifies Docker is available and running on the remote host.
func (p *RemoteProvider) dockerCheck(ctx context.Context) error {
	_, err := p.dockerRun(ctx, "info", "--format", "{{.ServerVersion}}")
	if err != nil {
		return fmt.Errorf("Docker is not available on the remote host. Run 'conga admin setup --provider remote' to install it.\n%w", err)
	}
	return nil
}

// containerName returns the Docker container name for an agent.
func containerName(agentName string) string {
	return "conga-" + agentName
}

// networkName returns the Docker network name for an agent.
func networkName(agentName string) string {
	return "conga-" + agentName
}

// createNetwork creates a Docker bridge network on the remote host.
// Egress restriction is enforced via per-agent Envoy proxy (HTTPS_PROXY env vars)
// and iptables DROP rules in the DOCKER-USER chain.
func (p *RemoteProvider) createNetwork(ctx context.Context, name string) error {
	_, err := p.ssh.Run(ctx, "docker network create "+shellQuote(name)+" --driver bridge")
	return err
}

// removeNetwork removes a Docker network on the remote host.
func (p *RemoteProvider) removeNetwork(ctx context.Context, name string) error {
	_, err := p.dockerRun(ctx, "network", "rm", name)
	return err
}

// connectNetwork connects a container to a network on the remote host.
func (p *RemoteProvider) connectNetwork(ctx context.Context, network, container string) error {
	_, err := p.dockerRun(ctx, "network", "connect", network, container)
	return err
}

// disconnectNetwork disconnects a container from a network (best-effort).
func (p *RemoteProvider) disconnectNetwork(ctx context.Context, network, container string) {
	p.dockerRun(ctx, "network", "disconnect", network, container)
}

// agentContainerOpts holds options for starting an agent container.
type agentContainerOpts struct {
	Name               string
	AgentName          string
	Network            string
	EnvFile            string
	DataDir            string
	GatewayPort        int
	Image              string
	EgressProxyName    string
	ProxyBootstrapPath string // Host path to proxy-bootstrap.js (mounted read-only)
}

// runAgentContainer starts an agent container with full isolation on the remote host.
func (p *RemoteProvider) runAgentContainer(ctx context.Context, opts agentContainerOpts) error {
	args := []string{
		"run", "-d",
		"--name", opts.Name,
		"--network", opts.Network,
		"--env-file", opts.EnvFile,
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--memory", "2g",
		"--cpus", "0.75",
		"--pids-limit", "256",
		"--user", "1000:1000",
		"-v", fmt.Sprintf("%s:/home/node/.openclaw:rw", opts.DataDir),
	}

	args = append(args, "-p", fmt.Sprintf("127.0.0.1:%d:%d", opts.GatewayPort, opts.GatewayPort))

	nodeOpts := "--max-old-space-size=1536"
	if opts.EgressProxyName != "" {
		// Proxy is on the same Docker network — Docker DNS resolves the container name.
		args = append(args,
			"-e", fmt.Sprintf("HTTPS_PROXY=http://%s:3128", opts.EgressProxyName),
			"-e", fmt.Sprintf("HTTP_PROXY=http://%s:3128", opts.EgressProxyName),
			"-e", "NO_PROXY=localhost,127.0.0.1",
		)
		// Mount the proxy bootstrap script and inject via --require so Node.js
		// routes all HTTP(S) traffic through the CONNECT tunnel proxy.
		if opts.ProxyBootstrapPath != "" {
			args = append(args, "-v", fmt.Sprintf("%s:/opt/proxy-bootstrap.js:ro", opts.ProxyBootstrapPath))
			nodeOpts += " --require /opt/proxy-bootstrap.js"
		}
	}
	args = append(args, "-e", "NODE_OPTIONS="+nodeOpts)

	args = append(args, opts.Image)

	_, err := p.dockerRun(ctx, args...)
	return err
}

// routerContainerOpts holds options for starting the router container.
type routerContainerOpts struct {
	EnvFile     string
	RouterDir   string
	RoutingJSON string
}

// runRouterContainer starts the router container on the remote host.
func (p *RemoteProvider) runRouterContainer(ctx context.Context, opts routerContainerOpts) error {
	args := []string{
		"run", "-d",
		"--name", "conga-router",
		"--env-file", opts.EnvFile,
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--memory", "128m",
		"--read-only",
		"--tmpfs", "/tmp:rw,noexec,nosuid",
		"--user", "1000:1000",
		"-v", fmt.Sprintf("%s:/app:ro", opts.RouterDir),
		"-v", fmt.Sprintf("%s:/opt/conga/config/routing.json:ro", opts.RoutingJSON),
	}
	args = append(args, "node:22-alpine", "node", "/app/src/index.js")

	_, err := p.dockerRun(ctx, args...)
	return err
}

// stopContainer stops a container on the remote host.
func (p *RemoteProvider) stopContainer(ctx context.Context, name string) error {
	_, err := p.dockerRun(ctx, "stop", name)
	return err
}

// removeContainer removes a container on the remote host.
func (p *RemoteProvider) removeContainer(ctx context.Context, name string) error {
	_, err := p.dockerRun(ctx, "rm", "-f", name)
	return err
}

// restartContainer restarts a container on the remote host.
func (p *RemoteProvider) restartContainer(ctx context.Context, name string) error {
	_, err := p.dockerRun(ctx, "restart", name)
	return err
}

// containerLogs returns the last N lines of container logs from the remote host.
func (p *RemoteProvider) containerLogs(ctx context.Context, name string, lines int) (string, error) {
	return p.dockerRun(ctx, "logs", name, "--tail", fmt.Sprintf("%d", lines), "--timestamps")
}

// DockerState is the JSON structure from docker inspect .State.
type DockerState struct {
	Status     string `json:"Status"`
	Running    bool   `json:"Running"`
	StartedAt  string `json:"StartedAt"`
	FinishedAt string `json:"FinishedAt"`
}

// inspectState returns the container state from the remote host.
func (p *RemoteProvider) inspectState(ctx context.Context, name string) (*DockerState, error) {
	output, err := p.dockerRun(ctx, "inspect", name, "--format", "{{json .State}}")
	if err != nil {
		return nil, err
	}
	var state DockerState
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &state); err != nil {
		return nil, fmt.Errorf("failed to parse container state: %w", err)
	}
	return &state, nil
}

// DockerStats holds resource usage from docker stats.
type DockerStats struct {
	CPUPercent  string
	MemoryUsage string
	PIDs        string
}

// containerStats returns resource usage from the remote host.
func (p *RemoteProvider) containerStats(ctx context.Context, name string) (*DockerStats, error) {
	output, err := p.dockerRun(ctx, "stats", name, "--no-stream", "--format", "{{.CPUPerc}}|{{.MemUsage}}|{{.PIDs}}")
	if err != nil {
		return nil, err
	}
	parts := strings.SplitN(strings.TrimSpace(output), "|", 3)
	stats := &DockerStats{}
	if len(parts) >= 1 {
		stats.CPUPercent = strings.TrimSpace(parts[0])
	}
	if len(parts) >= 2 {
		stats.MemoryUsage = strings.TrimSpace(parts[1])
	}
	if len(parts) >= 3 {
		stats.PIDs = strings.TrimSpace(parts[2])
	}
	return stats, nil
}

// containerExists checks if a container exists on the remote host.
func (p *RemoteProvider) containerExists(ctx context.Context, name string) bool {
	_, err := p.dockerRun(ctx, "inspect", name, "--format", "{{.Id}}")
	return err == nil
}

// networkExists checks if a network exists on the remote host.
func (p *RemoteProvider) networkExists(ctx context.Context, name string) bool {
	_, err := p.dockerRun(ctx, "network", "inspect", name, "--format", "{{.Id}}")
	return err == nil
}

// pullImage pulls a Docker image on the remote host.
func (p *RemoteProvider) pullImage(ctx context.Context, image string) error {
	_, err := p.dockerRun(ctx, "pull", image)
	return err
}

// buildImage builds a Docker image on the remote host.
func (p *RemoteProvider) buildImage(ctx context.Context, dir, tag string) error {
	_, err := p.dockerRun(ctx, "build", "-t", tag, dir)
	return err
}

// imageExists checks if a Docker image exists on the remote host.
func (p *RemoteProvider) imageExists(ctx context.Context, image string) bool {
	_, err := p.dockerRun(ctx, "image", "inspect", image)
	return err == nil
}

// containerIPOnNetwork returns the IP address of a container on a specific Docker network.
// Retries briefly to handle the race between container start and IP assignment.
func (p *RemoteProvider) containerIPOnNetwork(ctx context.Context, container, network string) (string, error) {
	tpl := fmt.Sprintf("{{(index .NetworkSettings.Networks %q).IPAddress}}", network)
	for attempt := 0; attempt < 10; attempt++ {
		output, err := p.dockerRun(ctx, "inspect", "-f", tpl, container)
		if err != nil {
			return "", fmt.Errorf("inspecting %s on network %s: %w", container, network, err)
		}
		ip := strings.TrimSpace(output)
		if ip != "" {
			return ip, nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return "", fmt.Errorf("no IP found for %s on network %s after retries", container, network)
}

// networkSubnetCIDR returns the CIDR of a Docker network's subnet.
func (p *RemoteProvider) networkSubnetCIDR(ctx context.Context, network string) (string, error) {
	output, err := p.dockerRun(ctx, "network", "inspect", network, "--format", "{{(index .IPAM.Config 0).Subnet}}")
	if err != nil {
		return "", fmt.Errorf("inspecting subnet for network %s: %w", network, err)
	}
	cidr := strings.TrimSpace(output)
	if cidr == "" {
		return "", fmt.Errorf("no subnet found for network %s", network)
	}
	return cidr, nil
}

// sshIptablesExec returns an iptables.ExecFunc that runs commands on the remote host via SSH.
func (p *RemoteProvider) sshIptablesExec(ctx context.Context) iptables.ExecFunc {
	return func(cmd string) error {
		_, err := p.ssh.Run(ctx, cmd)
		if err != nil {
			return fmt.Errorf("iptables via SSH: %w", err)
		}
		return nil
	}
}

// addEgressIptablesRules adds iptables DROP rules to DOCKER-USER that restrict
// outbound traffic from the container to only the bridge subnet (where the proxy
// and Docker DNS live). Uses DROP (not REJECT) so the app sees ETIMEDOUT instead
// of ENETUNREACH which would crash Node.js.
func (p *RemoteProvider) addEgressIptablesRules(ctx context.Context, containerIP, subnetCIDR string) error {
	return iptables.AddRules(containerIP, subnetCIDR, p.sshIptablesExec(ctx))
}

// removeEgressIptablesRules removes iptables egress rules for a container IP.
// Idempotent — ignores errors from missing rules.
func (p *RemoteProvider) removeEgressIptablesRules(ctx context.Context, containerIP, subnetCIDR string) {
	iptables.RemoveRules(containerIP, subnetCIDR, p.sshIptablesExec(ctx))
}

// checkEgressIptablesRules checks whether all egress iptables rules exist for a container IP.
func (p *RemoteProvider) checkEgressIptablesRules(ctx context.Context, containerIP, subnetCIDR string) bool {
	return iptables.CheckRules(containerIP, subnetCIDR, p.sshIptablesExec(ctx))
}
