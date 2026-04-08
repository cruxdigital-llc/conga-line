package remoteprovider

import (
	"context"
	"fmt"
	"os"
	posixpath "path"
	"path/filepath"
	"strings"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/pkg/common"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/ui"
)

// Setup runs the initial remote environment setup wizard.
// When cfg is non-nil, values from it are used instead of interactive prompts.
func (p *RemoteProvider) Setup(ctx context.Context, cfg *provider.SetupConfig) error {
	fmt.Println("Setting up remote Conga Line deployment...")

	// Track SSH key path for config persistence — load from existing config if available
	var sshKeyPath string
	if existingCfg, err := provider.LoadConfig(provider.DefaultConfigPath()); err == nil {
		sshKeyPath = existingCfg.SSHKeyPath
	}
	// Config override takes precedence
	if cfg != nil && cfg.SSHKeyPath != "" {
		sshKeyPath = cfg.SSHKeyPath
	}

	// If no SSH connection yet (first-time setup), prompt for details and connect
	if p.ssh == nil {
		var host, sshUser, keyPath string
		port := 22

		if cfg != nil && cfg.SSHHost != "" {
			host = cfg.SSHHost
			if cfg.SSHPort != 0 {
				port = cfg.SSHPort
			}
			sshUser = cfg.SSHUser
			if sshUser == "" {
				sshUser = "root"
			}
			keyPath = cfg.SSHKeyPath
			sshKeyPath = keyPath
		} else {
			var err error
			host, err = ui.TextPrompt("  SSH host (IP or hostname)")
			if err != nil {
				return err
			}
			if host == "" {
				return fmt.Errorf("SSH host is required")
			}

			portStr, err := ui.TextPromptWithDefault("  SSH port", "22")
			if err != nil {
				return err
			}
			if portStr != "" && portStr != "22" {
				fmt.Sscanf(portStr, "%d", &port)
			}

			sshUser, err = ui.TextPromptWithDefault("  SSH user", "root")
			if err != nil {
				return err
			}

			keyPath, err = ui.TextPromptWithDefault("  SSH key path (leave empty to auto-detect)", "")
			if err != nil {
				return err
			}
			sshKeyPath = keyPath
		}

		fmt.Printf("\nConnecting to %s@%s:%d...\n", sshUser, host, port)
		sshClient, err := SSHConnect(host, port, sshUser, keyPath)
		if err != nil {
			return fmt.Errorf("SSH connection failed: %w", err)
		}
		p.ssh = sshClient
	}

	// Verify SSH connection
	user, err := p.ssh.Run(ctx, "whoami")
	if err != nil {
		return fmt.Errorf("SSH connection failed: %w", err)
	}
	fmt.Printf("Connected to %s@%s as %s\n", p.ssh.user, p.ssh.host, strings.TrimSpace(user))

	// Check/install Docker
	fmt.Println("\nChecking Docker...")
	if err := p.dockerCheck(ctx); err != nil {
		install := cfg != nil && cfg.InstallDocker
		if !install {
			install = ui.Confirm("Docker not found on remote host. Install it?")
		}
		if !install {
			return fmt.Errorf("Docker is required. Install it manually on the remote host and rerun setup")
		}
		fmt.Println("Installing Docker...")
		if err := p.installDocker(ctx); err != nil {
			return fmt.Errorf("failed to install Docker: %w", err)
		}
		fmt.Println("Docker installed successfully.")
	} else {
		version, _ := p.dockerRun(ctx, "info", "--format", "{{.ServerVersion}}")
		fmt.Printf("Docker %s is available.\n", strings.TrimSpace(version))
	}

	// Create remote directory structure
	// Use sudo if available and needed (non-root user writing to /opt/)
	fmt.Println("\nCreating directory structure...")
	sshUser := strings.TrimSpace(user)
	createDirsScript := fmt.Sprintf(`#!/bin/sh
set -e
SUDO=""
if [ "$(id -u)" != "0" ]; then
    if command -v sudo >/dev/null 2>&1; then
        SUDO="sudo"
    else
        echo "NOT_ROOT_NO_SUDO" >&2
        exit 1
    fi
fi
$SUDO mkdir -p %s/{agents,secrets/{shared,agents},config,data,router/{slack,telegram}/src,behavior,egress-proxy,logs}
$SUDO chown -R %s:%s %s
chmod 700 %s/secrets %s/secrets/shared %s/secrets/agents %s/config
`, p.remoteDir, sshUser, sshUser, p.remoteDir, p.remoteDir, p.remoteDir, p.remoteDir, p.remoteDir)

	_, err = p.ssh.Run(ctx, createDirsScript)
	if err != nil {
		if strings.Contains(err.Error(), "NOT_ROOT_NO_SUDO") {
			return fmt.Errorf("cannot create /opt/conga: not root and sudo not available. Log in as root or install sudo")
		}
		return fmt.Errorf("failed to create directory structure: %w", err)
	}
	fmt.Println("  Directory structure created at /opt/conga/")

	changed := 0

	// --- Repo path ---
	repoPath := p.getConfigValue("repo_path")
	if cfg != nil && cfg.RepoPath != "" {
		repoPath = cfg.RepoPath
	}
	repoStatus := "set"
	if repoPath == "" {
		repoStatus = "not set"
		repoPath = detectRepoRoot()
	}
	fmt.Printf("\n[config] repo_path — Conga Line repo root for router/behavior files (%s)\n", repoStatus)
	if cfg == nil {
		newRepoPath, err := ui.TextPromptWithDefault("  Repo path", repoPath)
		if err != nil {
			return err
		}
		if newRepoPath != "" {
			repoPath = newRepoPath
		}
	}
	if repoPath != "" {
		if _, err := os.Stat(filepath.Join(repoPath, "router", "slack", "src", "index.js")); err != nil {
			return fmt.Errorf("invalid repo path: %s/router/slack/src/index.js not found", repoPath)
		}
		p.setConfigValue("repo_path", repoPath)
		changed++
	}

	// --- Docker image ---
	image := p.getConfigValue("image")
	if cfg != nil && cfg.Image != "" {
		image = cfg.Image
	}
	imageStatus := "set"
	if image == "" {
		imageStatus = "not set"
	}
	fmt.Printf("\n[config] image — OpenClaw Docker image (%s)\n", imageStatus)
	if cfg != nil {
		// Non-interactive: use config value or existing value
		if image == "" {
			image = "ghcr.io/openclaw/openclaw:2026.3.11"
		}
	} else if image == "" || ui.Confirm("  Update this value?") {
		defaultImage := "ghcr.io/openclaw/openclaw:2026.3.11"
		if image != "" {
			defaultImage = image
		}
		newImage, err := ui.TextPromptWithDefault("  Docker image", defaultImage)
		if err != nil {
			return err
		}
		if newImage != "" {
			image = newImage
		}
	}
	if image != "" {
		p.setConfigValue("image", image)
		changed++
	}

	// --- Shared secrets (non-channel only — channel secrets are managed via `conga channels add`) ---
	for _, item := range []struct {
		name, description string
		isSecret          bool
	}{
		{"google-client-id", "Google OAuth client ID", false},
		{"google-client-secret", "Google OAuth client secret", true},
	} {
		remotePath := posixpath.Join(p.sharedSecretsDir(), item.name)
		current := ""
		if data, err := p.ssh.Download(remotePath); err == nil {
			current = string(data)
		}

		cfgValue := cfg.SecretValue(item.name)
		status := "set"
		if current == "" && cfgValue == "" {
			status = "not set"
		}
		fmt.Printf("\n[secret] %s — %s (optional) (%s)\n", item.name, item.description, status)

		var value string
		if cfgValue != "" {
			value = cfgValue
		} else if cfg != nil {
			fmt.Println("  Skipped (not in config)")
			continue
		} else {
			if current != "" {
				if !ui.Confirm("  Update this value?") {
					continue
				}
			}

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
		}

		if err := p.ssh.Upload(remotePath, []byte(value), 0400); err != nil {
			return fmt.Errorf("failed to save %s: %w", item.name, err)
		}
		fmt.Printf("  Saved (%s).\n", common.MaskSecret(value))
		changed++
	}

	// --- Upload source files from repo ---
	if repoPath != "" {
		fmt.Println("\nUploading router source files...")
		if err := p.ssh.UploadDir(filepath.Join(repoPath, "router"), p.remoteRouterDir()); err != nil {
			return fmt.Errorf("failed to upload router files: %w", err)
		}
		fmt.Println("  Router source uploaded to /opt/conga/router/")

		fmt.Println("Installing router dependencies...")
		installCmd := fmt.Sprintf("docker run --rm -v %s:/app -w /app node:22-alpine npm install --omit=dev 2>&1",
			shellQuote(p.remoteRouterDir()))
		if out, installErr := p.ssh.Run(ctx, installCmd); installErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: npm install failed: %v\n%s\n", installErr, out)
		} else {
			fmt.Println("  Router dependencies installed.")
		}

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
		fmt.Printf("\nPulling OpenClaw image %s on remote host...\n", image)
		spin := ui.NewSpinner("Pulling Docker image...")
		err := p.pullImage(ctx, image)
		spin.Stop()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to pull image: %v\nYou can pull it manually on the remote host: docker pull %s\n", err, image)
		} else {
			fmt.Println("  Image pulled.")
		}
	}

	fmt.Println("Pulling node:22-alpine on remote host...")
	spin := ui.NewSpinner("Pulling router image...")
	p.pullImage(ctx, "node:22-alpine")
	spin.Stop()

	// --- Build egress proxy on remote ---
	_, err = p.ssh.Run(ctx, fmt.Sprintf("test -f %s",
		shellQuote(posixpath.Join(p.remoteEgressProxyDir(), "Dockerfile"))))
	if err == nil {
		fmt.Println("Building egress proxy image on remote host...")
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
	routingPath := posixpath.Join(p.remoteConfigDir(), "routing.json")
	_, err = p.ssh.Run(ctx, fmt.Sprintf("test -f %s", shellQuote(routingPath)))
	if err != nil {
		p.ssh.Upload(routingPath, []byte(`{"channels":{},"members":{}}`), 0644)
	}

	// --- Auto-configure channels if secrets were provided in SetupConfig (backwards compat) ---
	if cfg != nil {
		for _, ch := range channels.All() {
			channelSecrets := map[string]string{}
			hasRequired := true
			for _, def := range ch.SharedSecrets() {
				val := cfg.SecretValue(def.Name)
				if val != "" {
					channelSecrets[def.Name] = val
				} else if def.Required {
					hasRequired = false
					break
				}
			}
			if hasRequired && len(channelSecrets) > 0 {
				fmt.Printf("\nAuto-configuring %s channel from provided secrets...\n", ch.Name())
				if err := p.AddChannel(ctx, ch.Name(), channelSecrets); err != nil {
					return fmt.Errorf("auto-configure %s channel: %w", ch.Name(), err)
				}
			}
		}
	}

	// --- Save provider config ---
	provCfg := &provider.Config{
		Provider:   provider.ProviderRemote,
		DataDir:    p.dataDir,
		SSHHost:    p.ssh.host,
		SSHPort:    p.ssh.port,
		SSHUser:    p.ssh.user,
		SSHKeyPath: sshKeyPath,
	}
	if err := provider.SaveConfig(provider.ConfigPathForDataDir(p.dataDir), provCfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	if changed > 0 {
		fmt.Printf("\n%d value(s) configured.\n", changed)
	} else {
		fmt.Println("\nAll values already configured.")
	}
	fmt.Println("\nRemote deployment ready! Next steps:")
	fmt.Println("  conga channels add slack                                     # optional: add Slack integration")
	fmt.Println("  conga admin add-user <name>                                  # provision an agent")
	fmt.Println("  conga channels bind <name> slack:<id>                        # optional: bind agent to Slack")
	return nil
}

// installDocker installs Docker on the remote host by detecting the package manager.
func (p *RemoteProvider) installDocker(ctx context.Context) error {
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
			if _, err := os.Stat(filepath.Join(dir, "router", "slack", "src", "index.js")); err == nil {
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
