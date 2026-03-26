package remoteprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
// When internal is true, the network is created with --internal which removes
// the default gateway and blocks all traffic to/from external networks.
// Containers on an internal network can only communicate with each other.
func (p *RemoteProvider) createNetwork(ctx context.Context, name string, internal bool) error {
	cmd := "docker network create " + shellQuote(name) + " --driver bridge"
	if internal {
		cmd += " --internal"
	}
	_, err := p.ssh.Run(ctx, cmd)
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
	Name            string
	AgentName       string
	Network         string
	EnvFile         string
	DataDir         string
	GatewayPort     int
	Image           string
	EgressEnforce   bool
	EgressProxyName string
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
		"-e", "NODE_OPTIONS=--max-old-space-size=1536",
		"-v", fmt.Sprintf("%s:/home/node/.openclaw:rw", opts.DataDir),
	}

	// When egress is enforced, the network is --internal and -p doesn't work.
	// Gateway access is provided by socat on the host instead (see startPortForwarder).
	if !opts.EgressEnforce {
		args = append(args, "-p", fmt.Sprintf("127.0.0.1:%d:%d", opts.GatewayPort, opts.GatewayPort))
	}

	if opts.EgressEnforce && opts.EgressProxyName != "" {
		// Proxy is on the same Docker network — Docker DNS resolves the container name.
		args = append(args,
			"-e", fmt.Sprintf("HTTPS_PROXY=http://%s:3128", opts.EgressProxyName),
			"-e", fmt.Sprintf("HTTP_PROXY=http://%s:3128", opts.EgressProxyName),
			"-e", "NO_PROXY=localhost,127.0.0.1",
		)
	}

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

// portForwarderPidFile returns the PID file path for an agent's socat port forwarder.
func portForwarderPidFile(cName string) string {
	return fmt.Sprintf("/run/conga-fwd-%s.pid", cName)
}

// startPortForwarder starts a socat process on the remote host that forwards
// localhost:port to the agent container's IP on the internal network.
// The host can always route to container IPs on Docker bridges (including
// --internal ones) because --internal only restricts the container's routing
// table, not the host's.
func (p *RemoteProvider) startPortForwarder(ctx context.Context, agentName string, port int) error {
	cName := containerName(agentName)
	netName := networkName(agentName)

	// Get container IP on the internal network
	tpl := fmt.Sprintf("{{(index .NetworkSettings.Networks %q).IPAddress}}", netName)
	output, err := p.dockerRun(ctx, "inspect", "-f", tpl, cName)
	if err != nil {
		return fmt.Errorf("getting container IP: %w", err)
	}
	ip := strings.TrimSpace(output)
	if ip == "" {
		return fmt.Errorf("container %s has no IP on network %s", cName, netName)
	}

	pidFile := portForwarderPidFile(cName)
	cmd := fmt.Sprintf(
		"nohup socat TCP-LISTEN:%d,bind=127.0.0.1,fork,reuseaddr TCP:%s:%d </dev/null >/dev/null 2>&1 & echo $! > %s",
		port, ip, port, pidFile)
	_, err = p.ssh.Run(ctx, cmd)
	return err
}

// stopPortForwarder kills the socat process for an agent's gateway forwarder.
func (p *RemoteProvider) stopPortForwarder(ctx context.Context, agentName string) {
	pidFile := portForwarderPidFile(containerName(agentName))
	p.ssh.Run(ctx, fmt.Sprintf(
		"if [ -f %s ]; then kill $(cat %s) 2>/dev/null; rm -f %s; fi",
		pidFile, pidFile, pidFile))
}
