// Package localprovider implements the Provider interface using local Docker.
package localprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/cruxdigital-llc/conga-line/cli/internal/policy"
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
// When internal is true, the network is created with --internal which removes
// the default gateway and blocks all traffic to/from external networks.
// Gateway access is provided by a forwarder container (see startPortForwarder).
func createNetwork(ctx context.Context, name string, internal bool) error {
	args := []string{"network", "create", name, "--driver", "bridge"}
	if internal {
		args = append(args, "--internal")
	}
	_, err := dockerRun(ctx, args...)
	return err
}

// removeNetwork removes a Docker network.
func removeNetwork(ctx context.Context, name string) error {
	_, err := dockerRun(ctx, "network", "rm", name)
	return err
}

// connectNetwork connects a container to a network.
func connectNetwork(ctx context.Context, network, container string) error {
	_, err := dockerRun(ctx, "network", "connect", network, container)
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
		"-e", "NODE_OPTIONS=--max-old-space-size=1536",
		"-v", fmt.Sprintf("%s:/home/node/.openclaw:rw", opts.DataDir),
	}

	// When egress is enforced, the network is --internal and -p doesn't work.
	// Gateway access is provided by a forwarder container (see startPortForwarder).
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

	_, err := dockerRun(ctx, args...)
	return err
}

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
	return err
}

// removeContainer removes a container.
func removeContainer(ctx context.Context, name string) error {
	_, err := dockerRun(ctx, "rm", "-f", name)
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

// forwarderName returns the Docker container name for a gateway port forwarder.
func forwarderName(agentName string) string {
	return "conga-fwd-" + agentName
}

// startPortForwarder starts a socat container that forwards gateway traffic
// from a published port to the agent container on the internal network.
// On macOS Docker Desktop, the host can't route to container IPs directly,
// so we use a container-based forwarder instead of host socat.
// The forwarder starts on the default bridge (with -p) then connects to the
// agent's internal network where it resolves the agent by Docker DNS.
func startPortForwarder(ctx context.Context, agentName string, port int) error {
	fwdName := forwarderName(agentName)
	target := containerName(agentName)

	if containerExists(ctx, fwdName) {
		removeContainer(ctx, fwdName)
	}

	args := []string{"run", "-d",
		"--name", fwdName,
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--memory", "32m",
		"--read-only",
		"-p", fmt.Sprintf("127.0.0.1:%d:%d", port, port),
		policy.EgressProxyImage,
		"socat",
		fmt.Sprintf("TCP-LISTEN:%d,fork,reuseaddr", port),
		fmt.Sprintf("TCP:%s:%d", target, port),
	}

	if _, err := dockerRun(ctx, args...); err != nil {
		return fmt.Errorf("starting port forwarder: %w", err)
	}

	// Connect to agent's internal network so socat can resolve the agent hostname
	return connectNetwork(ctx, networkName(agentName), fwdName)
}

// stopPortForwarder removes the gateway port forwarder container.
func stopPortForwarder(ctx context.Context, agentName string) {
	fwdName := forwarderName(agentName)
	if containerExists(ctx, fwdName) {
		removeContainer(ctx, fwdName)
	}
}
