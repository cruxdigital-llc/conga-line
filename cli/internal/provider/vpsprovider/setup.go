package vpsprovider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cruxdigital-llc/conga-line/cli/internal/provider"
	"github.com/cruxdigital-llc/conga-line/cli/internal/ui"
)

// Setup runs the initial VPS environment setup wizard.
func (p *VPSProvider) Setup(ctx context.Context) error {
	fmt.Println("Setting up VPS Conga Line deployment...")

	// Verify SSH connection
	user, err := p.ssh.Run(ctx, "whoami")
	if err != nil {
		return fmt.Errorf("SSH connection failed: %w", err)
	}
	fmt.Printf("Connected to %s@%s as %s\n", p.ssh.user, p.ssh.host, strings.TrimSpace(user))

	// Check/install Docker
	fmt.Println("\nChecking Docker...")
	if err := p.dockerCheck(ctx); err != nil {
		fmt.Println("Docker not found. Installing...")
		if err := p.installDocker(ctx); err != nil {
			return fmt.Errorf("failed to install Docker: %w", err)
		}
		fmt.Println("Docker installed successfully.")
	} else {
		version, _ := p.dockerRun(ctx, "info", "--format", "{{.ServerVersion}}")
		fmt.Printf("Docker %s is available.\n", strings.TrimSpace(version))
	}

	// Create remote directory structure
	fmt.Println("\nCreating directory structure...")
	dirs := []struct {
		path string
		perm os.FileMode
	}{
		{filepath.Join(p.remoteDir, "agents"), 0700},
		{filepath.Join(p.remoteDir, "secrets", "shared"), 0700},
		{filepath.Join(p.remoteDir, "secrets", "agents"), 0700},
		{filepath.Join(p.remoteDir, "data"), 0755},
		{filepath.Join(p.remoteDir, "config"), 0700},
		{filepath.Join(p.remoteDir, "router", "src"), 0755},
		{filepath.Join(p.remoteDir, "behavior"), 0755},
		{filepath.Join(p.remoteDir, "egress-proxy"), 0755},
		{filepath.Join(p.remoteDir, "logs"), 0755},
	}
	for _, d := range dirs {
		if err := p.ssh.MkdirAll(d.path, d.perm); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", d.path, err)
		}
	}
	fmt.Println("  Directory structure created at /opt/conga/")

	changed := 0

	// --- Repo path ---
	repoPath := p.getConfigValue("repo_path")
	repoStatus := "set"
	if repoPath == "" {
		repoStatus = "not set"
		repoPath = detectRepoRoot()
	}
	fmt.Printf("\n[config] repo_path — Conga Line repo root for router/behavior files (%s)\n", repoStatus)
	newRepoPath, err := ui.TextPromptWithDefault("  Repo path", repoPath)
	if err != nil {
		return err
	}
	if newRepoPath != "" {
		if _, err := os.Stat(filepath.Join(newRepoPath, "router", "src", "index.js")); err != nil {
			return fmt.Errorf("invalid repo path: %s/router/src/index.js not found", newRepoPath)
		}
		p.setConfigValue("repo_path", newRepoPath)
		repoPath = newRepoPath
		changed++
	}

	// --- Docker image ---
	image := p.getConfigValue("image")
	imageStatus := "set"
	if image == "" {
		imageStatus = "not set"
	}
	fmt.Printf("\n[config] image — OpenClaw Docker image (%s)\n", imageStatus)
	if image == "" || ui.Confirm("  Update this value?") {
		defaultImage := "ghcr.io/openclaw/openclaw:2026.3.11"
		if image != "" {
			defaultImage = image
		}
		newImage, err := ui.TextPromptWithDefault("  Docker image", defaultImage)
		if err != nil {
			return err
		}
		if newImage != "" {
			p.setConfigValue("image", newImage)
			image = newImage
			changed++
		}
	}

	// --- Shared secrets (all optional) ---
	secretItems := []struct {
		name, description string
		isSecret          bool
	}{
		{"slack-bot-token", "Slack bot token (xoxb-...)", true},
		{"slack-signing-secret", "Slack signing secret", true},
		{"slack-app-token", "Slack app token (xapp-...)", true},
		{"google-client-id", "Google OAuth client ID", false},
		{"google-client-secret", "Google OAuth client secret", true},
	}

	fmt.Println("\nSlack integration is optional. Skip all Slack tokens to run in gateway-only mode (web UI).")

	for _, item := range secretItems {
		remotePath := filepath.Join(p.sharedSecretsDir(), item.name)
		current := ""
		if data, err := p.ssh.Download(remotePath); err == nil {
			current = string(data)
		}

		status := "set"
		if current == "" {
			status = "not set"
		}
		fmt.Printf("\n[secret] %s — %s (optional) (%s)\n", item.name, item.description, status)

		if current != "" {
			if !ui.Confirm("  Update this value?") {
				continue
			}
		}

		var value string
		if item.isSecret {
			value, err = ui.SecretPrompt(fmt.Sprintf("  Enter %s", item.name))
		} else {
			value, err = ui.TextPrompt(fmt.Sprintf("  Enter %s", item.name))
		}
		if err != nil {
			return err
		}
		if value == "" {
			fmt.Println("  Skipped (empty value)")
			continue
		}

		if err := p.ssh.Upload(remotePath, []byte(value), 0400); err != nil {
			return fmt.Errorf("failed to save %s: %w", item.name, err)
		}
		fmt.Println("  Saved.")
		changed++
	}

	// --- Upload source files from repo ---
	if repoPath != "" {
		fmt.Println("\nUploading router source files...")
		if err := p.ssh.UploadDir(filepath.Join(repoPath, "router"), p.remoteRouterDir()); err != nil {
			return fmt.Errorf("failed to upload router files: %w", err)
		}
		fmt.Println("  Router source uploaded to /opt/conga/router/")

		fmt.Println("Uploading behavior files...")
		if err := p.ssh.UploadDir(filepath.Join(repoPath, "behavior"), p.remoteBehaviorDir()); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to upload behavior files: %v\n", err)
		} else {
			fmt.Println("  Behavior files uploaded to /opt/conga/behavior/")
		}

		fmt.Println("Uploading egress proxy config...")
		egressSrc := filepath.Join(repoPath, "deploy", "egress-proxy")
		if _, err := os.Stat(egressSrc); err == nil {
			if err := p.ssh.UploadDir(egressSrc, p.remoteEgressProxyDir()); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to upload egress proxy files: %v\n", err)
			} else {
				fmt.Println("  Egress proxy config uploaded to /opt/conga/egress-proxy/")
			}
		}
	}

	// --- Pull images on remote ---
	if image != "" {
		fmt.Printf("\nPulling OpenClaw image %s on VPS...\n", image)
		spin := ui.NewSpinner("Pulling Docker image...")
		err := p.pullImage(ctx, image)
		spin.Stop()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to pull image: %v\nYou can pull it manually on the VPS: docker pull %s\n", err, image)
		} else {
			fmt.Println("  Image pulled.")
		}
	}

	fmt.Println("Pulling node:22-alpine on VPS...")
	spin := ui.NewSpinner("Pulling router image...")
	p.pullImage(ctx, "node:22-alpine")
	spin.Stop()

	// --- Build egress proxy on remote ---
	_, err = p.ssh.Run(ctx, fmt.Sprintf("test -f %s",
		shellQuote(filepath.Join(p.remoteEgressProxyDir(), "Dockerfile"))))
	if err == nil {
		fmt.Println("Building egress proxy image on VPS...")
		spin := ui.NewSpinner("Building egress proxy...")
		err := p.buildImage(ctx, p.remoteEgressProxyDir(), egressProxyImage)
		spin.Stop()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to build egress proxy: %v\n", err)
		} else {
			fmt.Println("  Egress proxy image built.")
		}
	}

	// --- Create initial routing.json ---
	routingPath := filepath.Join(p.remoteConfigDir(), "routing.json")
	_, err = p.ssh.Run(ctx, fmt.Sprintf("test -f %s", shellQuote(routingPath)))
	if err != nil {
		p.ssh.Upload(routingPath, []byte(`{"channels":{},"members":{}}`), 0644)
	}

	// --- Start egress proxy ---
	p.ensureEgressProxy(ctx)

	// --- Router (only if Slack configured) ---
	shared, _ := p.readSharedSecrets()
	if shared.HasSlack() {
		routerEnvPath := filepath.Join(p.remoteConfigDir(), "router.env")
		routerEnv := fmt.Sprintf("SLACK_APP_TOKEN=%s\nSLACK_SIGNING_SECRET=%s\n", shared.SlackAppToken, shared.SlackSigningSecret)
		if err := p.ssh.Upload(routerEnvPath, []byte(routerEnv), 0400); err != nil {
			return fmt.Errorf("failed to write router env file: %w", err)
		}
		p.ensureRouter(ctx)
	} else {
		fmt.Println("\nSlack not configured — router skipped. Agents will run in gateway-only mode (web UI).")
	}

	// --- Save provider config ---
	provCfg := &provider.Config{
		Provider: "vps",
		DataDir:  p.dataDir,
		SSHHost:  p.ssh.host,
		SSHPort:  p.ssh.port,
		SSHUser:  p.ssh.user,
	}
	if err := provider.SaveConfig(provider.DefaultConfigPath(), provCfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	if changed > 0 {
		fmt.Printf("\n%d value(s) configured.\n", changed)
	} else {
		fmt.Println("\nAll values already configured.")
	}
	fmt.Println("\nVPS deployment ready! Next steps:")
	fmt.Println("  conga admin add-user <name> [slack_member_id]    # Slack ID optional for gateway-only mode")
	fmt.Println("  conga admin add-team <name> [slack_channel]      # Slack channel optional for gateway-only mode")
	return nil
}

// installDocker installs Docker on the remote host by detecting the package manager.
func (p *VPSProvider) installDocker(ctx context.Context) error {
	script := `#!/bin/sh
set -e
if command -v apt-get >/dev/null 2>&1; then
    apt-get update -qq && apt-get install -y -qq docker.io >/dev/null
elif command -v dnf >/dev/null 2>&1; then
    dnf install -y -q docker
elif command -v yum >/dev/null 2>&1; then
    yum install -y -q docker
elif command -v pacman >/dev/null 2>&1; then
    pacman -S --noconfirm docker >/dev/null
else
    echo "UNSUPPORTED_OS" >&2
    exit 1
fi
systemctl enable docker >/dev/null 2>&1
systemctl start docker
docker info --format '{{.ServerVersion}}'`

	output, err := p.ssh.Run(ctx, script)
	if err != nil {
		if strings.Contains(err.Error(), "UNSUPPORTED_OS") {
			return fmt.Errorf("unsupported OS — install Docker manually and rerun setup")
		}
		return err
	}
	fmt.Printf("  Docker version: %s\n", strings.TrimSpace(output))
	return nil
}

// detectRepoRoot tries to find the conga-line repo root from the current working directory.
func detectRepoRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "router", "src", "index.js")); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}
