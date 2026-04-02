// Package localprovider implements the Provider interface using local Docker.
package localprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/cruxdigital-llc/conga-line/pkg/provider/iptables"
)

// dockerRun executes a docker command and returns stdout.
func dockerRun(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker %s: %s (%w)", args[0], strings.TrimSpace(stderr.String()), err)
	}
	return stdout.String(), nil
}

// dockerCheck verifies Docker is available and running.
func dockerCheck(ctx context.Context) error {
	_, err := dockerRun(ctx, "info", "--format", "{{.ServerVersion}}")
	if err != nil {
		return fmt.Errorf("Docker is not running. Please install Docker Desktop and start it.\n%w", err)
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

// createNetwork creates a Docker bridge network for agent isolation.
// Each agent gets its own network to prevent inter-container communication.
// Egress restriction is enforced via per-agent Envoy proxy (HTTPS_PROXY env vars)
// and iptables DROP rules in the DOCKER-USER chain.
func createNetwork(ctx context.Context, name string) error {
	_, err := dockerRun(ctx, "network", "create", name, "--driver", "bridge")
	if err != nil && strings.Contains(err.Error(), "already exists") {
		return nil
	}
	return err
}

// removeNetwork removes a Docker network.
func removeNetwork(ctx context.Context, name string) error {
	_, err := dockerRun(ctx, "network", "rm", name)
	if err != nil && strings.Contains(err.Error(), "not found") {
		return nil
	}
	return err
}

// connectNetwork connects a container to a network.
func connectNetwork(ctx context.Context, network, container string) error {
	_, err := dockerRun(ctx, "network", "connect", network, container)
	if err != nil && strings.Contains(err.Error(), "already exists") {
		return nil
	}
	return err
}

// disconnectNetwork disconnects a container from a network (best-effort).
func disconnectNetwork(ctx context.Context, network, container string) {
	dockerRun(ctx, "network", "disconnect", network, container)
}

// runAgentContainer starts an agent container with full isolation.
// Matches the AWS bootstrap: data mounted to /home/node/.openclaw, gateway on port 18789.
func runAgentContainer(ctx context.Context, opts agentContainerOpts) error {
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

	_, err := dockerRun(ctx, args...)
	return err
}

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

// runRouterContainer starts the router container.
func runRouterContainer(ctx context.Context, opts routerContainerOpts) error {
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

	_, err := dockerRun(ctx, args...)
	return err
}

type routerContainerOpts struct {
	EnvFile     string
	RouterDir   string
	RoutingJSON string
}

// stopContainer stops a container.
func stopContainer(ctx context.Context, name string) error {
	_, err := dockerRun(ctx, "stop", name)
	if err != nil && strings.Contains(err.Error(), "No such container") {
		return nil
	}
	return err
}

// removeContainer removes a container.
func removeContainer(ctx context.Context, name string) error {
	_, err := dockerRun(ctx, "rm", "-f", name)
	if err != nil && (strings.Contains(err.Error(), "No such container") || strings.Contains(err.Error(), "already in progress")) {
		return nil
	}
	return err
}

// restartContainer restarts a container.
func restartContainer(ctx context.Context, name string) error {
	_, err := dockerRun(ctx, "restart", name)
	return err
}

// containerLogs returns the last N lines of container logs.
func containerLogs(ctx context.Context, name string, lines int) (string, error) {
	return dockerRun(ctx, "logs", name, "--tail", fmt.Sprintf("%d", lines), "--timestamps")
}

// DockerState is the JSON structure from docker inspect .State.
type DockerState struct {
	Status     string `json:"Status"`
	Running    bool   `json:"Running"`
	StartedAt  string `json:"StartedAt"`
	FinishedAt string `json:"FinishedAt"`
}

// inspectState returns the container state.
func inspectState(ctx context.Context, name string) (*DockerState, error) {
	output, err := dockerRun(ctx, "inspect", name, "--format", "{{json .State}}")
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

// containerStats returns resource usage.
func containerStats(ctx context.Context, name string) (*DockerStats, error) {
	output, err := dockerRun(ctx, "stats", name, "--no-stream", "--format", "{{.CPUPerc}}|{{.MemUsage}}|{{.PIDs}}")
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

// containerExists checks if a container exists (running or stopped).
func containerExists(ctx context.Context, name string) bool {
	_, err := dockerRun(ctx, "inspect", name, "--format", "{{.Id}}")
	return err == nil
}

// networkExists checks if a network exists.
func networkExists(ctx context.Context, name string) bool {
	_, err := dockerRun(ctx, "network", "inspect", name, "--format", "{{.Id}}")
	return err == nil
}

// pullImage pulls a Docker image.
func pullImage(ctx context.Context, image string) error {
	_, err := dockerRun(ctx, "pull", image)
	return err
}

// buildImage builds a Docker image from a directory.
func buildImage(ctx context.Context, dir, tag string) error {
	_, err := dockerRun(ctx, "build", "-t", tag, dir)
	return err
}

// imageExists checks if a Docker image exists locally.
func imageExists(ctx context.Context, image string) bool {
	_, err := dockerRun(ctx, "image", "inspect", image)
	return err == nil
}

// imageHasBinary checks whether a Docker image contains a specific binary.
func imageHasBinary(ctx context.Context, image string, binary string) bool {
	_, err := dockerRun(ctx, "run", "--rm", image, "which", binary)
	return err == nil
}

// containerIPOnNetwork returns the IP address of a container on a specific Docker network.
// Retries briefly to handle the race between container start and IP assignment.
func containerIPOnNetwork(ctx context.Context, container, network string) (string, error) {
	format := fmt.Sprintf("{{(index .NetworkSettings.Networks %q).IPAddress}}", network)
	for attempt := 0; attempt < 10; attempt++ {
		output, err := dockerRun(ctx, "inspect", "--format", format, container)
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
func networkSubnetCIDR(ctx context.Context, network string) (string, error) {
	output, err := dockerRun(ctx, "network", "inspect", network, "--format", "{{(index .IPAM.Config 0).Subnet}}")
	if err != nil {
		return "", fmt.Errorf("inspecting subnet for network %s: %w", network, err)
	}
	cidr := strings.TrimSpace(output)
	if cidr == "" {
		return "", fmt.Errorf("no subnet found for network %s", network)
	}
	return cidr, nil
}

// iptablesRun executes an iptables command on the Docker host.
// On macOS (Docker Desktop), iptables runs inside the LinuxKit VM via nsenter.
// On Linux, it runs directly via sh.
func iptablesRun(ctx context.Context, iptablesCmd string) error {
	if runtime.GOOS == "darwin" {
		_, err := dockerRun(ctx, "run", "--rm",
			"--cap-add", "NET_ADMIN", "--cap-add", "NET_RAW",
			"--pid=host", "--network=host",
			"alpine:3.21",
			"nsenter", "-t", "1", "-m", "-u", "-n", "-i",
			"sh", "-c", iptablesCmd)
		return err
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", iptablesCmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("iptables: %s (%w)", strings.TrimSpace(stderr.String()), err)
	}
	return nil
}

// iptablesExec returns an iptables.ExecFunc that runs commands on the Docker host.
func iptablesExec(ctx context.Context) iptables.ExecFunc {
	return func(cmd string) error {
		return iptablesRun(ctx, cmd)
	}
}

// addEgressIptablesRules adds iptables DROP rules to DOCKER-USER that restrict
// outbound traffic from the container to only the bridge subnet.
func addEgressIptablesRules(ctx context.Context, containerIP, subnetCIDR string) error {
	return iptables.AddRules(containerIP, subnetCIDR, iptablesExec(ctx))
}

// removeEgressIptablesRules removes iptables egress rules for a container IP.
func removeEgressIptablesRules(ctx context.Context, containerIP, subnetCIDR string) {
	iptables.RemoveRules(containerIP, subnetCIDR, iptablesExec(ctx))
}

// checkEgressIptablesRules checks whether all egress iptables rules exist for a container IP.
func checkEgressIptablesRules(ctx context.Context, containerIP, subnetCIDR string) bool {
	return iptables.CheckRules(containerIP, subnetCIDR, iptablesExec(ctx))
}
