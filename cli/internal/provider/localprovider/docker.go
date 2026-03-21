// Package localprovider implements the Provider interface using local Docker.
package localprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
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
// We don't use --internal because it prevents -p port publishing to localhost.
// Isolation is enforced by: no inter-container routing (separate networks),
// egress proxy for restricted outbound, and localhost-only port bindings.
func createNetwork(ctx context.Context, name string) error {
	_, err := dockerRun(ctx, "network", "create", name, "--driver", "bridge")
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
		"-p", fmt.Sprintf("127.0.0.1:%d:%d", opts.GatewayPort, opts.GatewayPort),
	}
	args = append(args, opts.Image)

	_, err := dockerRun(ctx, args...)
	return err
}

type agentContainerOpts struct {
	Name        string
	AgentName   string
	Network     string
	EnvFile     string
	DataDir     string
	GatewayPort int
	Image       string
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
