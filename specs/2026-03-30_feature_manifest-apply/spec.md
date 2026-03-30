# Specification: Manifest Apply

## Overview

New `conga apply <manifest.yaml>` command and backing `cli/internal/manifest/` package. Parses a YAML manifest describing an environment's desired state (setup, agents, secrets, channels, bindings, policy), validates it, expands environment variable references in secrets, and executes provisioning steps sequentially through the existing `Provider` interface.

---

## 1. YAML Schema

### 1.1 Top-Level Structure

```yaml
apiVersion: conga.dev/v1alpha1   # required, must match
kind: Environment                 # required, only "Environment" supported in v1
setup: { ... }                    # optional — server bootstrap config
agents: [ ... ]                   # optional — agent definitions
channels: [ ... ]                 # optional — messaging platform config + bindings
policy: { ... }                   # optional — inline policy (egress, routing, posture)
```

All sections are optional. A manifest with only `agents:` is valid — useful for adding agents to an existing environment. An empty manifest (only `apiVersion` + `kind`) is valid but a no-op.

### 1.2 `setup` Section

Maps directly to `provider.SetupConfig`. Fields are provider-specific — irrelevant fields are silently ignored (e.g., `ssh_host` on local provider).

```yaml
setup:
  image: "ghcr.io/openclaw/openclaw:2026.3.11"  # Docker image (all providers)
  repo_path: "/path/to/congaline"                # local repo path for file uploads
  ssh_host: "demo.example.com"                   # remote provider only
  ssh_port: 22                                    # remote provider only (default: 22)
  ssh_user: "ubuntu"                              # remote provider only
  ssh_key_path: "~/.ssh/id_ed25519"              # remote provider only
  install_docker: true                            # skip Docker install prompt
```

**Env var expansion**: NOT applied to setup fields. Setup values are infrastructure config, not secrets. If users need env vars in setup (e.g., `ssh_host: "$DEMO_HOST"`), this can be added later.

### 1.3 `agents` Section

```yaml
agents:
  - name: "aaron"                    # required, lowercase alphanumeric + hyphens
    type: "user"                     # required, "user" or "team"
    iam_identity: "aaron@co.com"     # optional, AWS provider only
    secrets:                         # optional, map of secret-name → value/$VAR
      anthropic-api-key: "$ANTHROPIC_API_KEY"
      custom-secret: "literal-value"
```

**Validation rules**:
- `name`: must pass `common.ValidateAgentName()` (lowercase alphanumeric + hyphens, non-empty)
- `type`: must be `"user"` or `"team"`
- `name` must be unique across all agents in the manifest
- `secrets` keys: must be valid secret names (lowercase alphanumeric + hyphens)

**Gateway ports** are auto-assigned via `common.NextAvailablePort()` during apply — not declared in the manifest.

### 1.4 `channels` Section

```yaml
channels:
  - platform: "slack"                # required, must be a registered channel name
    secrets:                         # required for initial setup, map of secret-name → $VAR
      slack-bot-token: "$SLACK_BOT_TOKEN"
      slack-signing-secret: "$SLACK_SIGNING_SECRET"
      slack-app-token: "$SLACK_APP_TOKEN"
    bindings:                        # optional, list of agent→channel bindings
      - agent: "aaron"              # must reference an agent in the agents list
        id: "U0ANSPZPG9X"          # platform-specific ID
        label: "Aaron DM"          # optional human label
      - agent: "team"
        id: "C0ANFAD41GB"
```

**Validation rules**:
- `platform`: must be registered in `channels.Get()` (currently only `"slack"`)
- `bindings[].agent`: must reference a name from the `agents` list in the same manifest
- `bindings[].id`: validated by the channel's `ValidateBinding()` method at apply time
- Duplicate platforms are not allowed

### 1.5 `policy` Section

Reuses the exact types from `cli/internal/policy/`. The manifest's `policy` section maps directly to a `PolicyFile` (with `apiVersion` inherited from the manifest's top-level field).

```yaml
policy:
  egress:
    mode: enforce                    # "enforce" or "validate"
    allowed_domains:
      - "api.anthropic.com"
      - "*.slack.com"
      - "*.slack-edge.com"
    blocked_domains: []
  routing:
    default_model: "claude-sonnet-4-20250514"
    fallback_chain:
      - "claude-sonnet-4-20250514"
      - "claude-haiku-4-5-20251001"
  posture:
    isolation_level: standard
  agents:                            # per-agent policy overrides
    team:
      egress:
        allowed_domains:
          - "api.anthropic.com"
```

**Validation**: The policy section is validated by `policy.PolicyFile.Validate()` before saving — same validation as `conga policy validate`.

### 1.6 Environment Variable Expansion

**Scope**: Only applied to values in `agents[].secrets` and `channels[].secrets` maps.

**Syntax**: `$VAR` and `${VAR}` (via Go's `os.Expand`).

**Rules**:
- Only strings starting with `$` are expanded. Literal values pass through unchanged.
- If a referenced env var is empty or unset, `ExpandSecrets` returns an error naming the var and the context (agent/channel name).
- Expansion happens during the validate phase, before any provider calls.
- Expanded values are held in memory only — never written back to the manifest file.

**Error example**:
```
expanding secrets for agent "aaron": environment variable ANTHROPIC_API_KEY is not set
```

---

## 2. Data Models

### 2.1 Manifest Structs (`cli/internal/manifest/manifest.go`)

```go
package manifest

import "github.com/cruxdigital-llc/conga-line/cli/internal/policy"

type Manifest struct {
    APIVersion string            `yaml:"apiVersion"`
    Kind       string            `yaml:"kind"`
    Setup      *ManifestSetup    `yaml:"setup,omitempty"`
    Agents     []ManifestAgent   `yaml:"agents,omitempty"`
    Channels   []ManifestChannel `yaml:"channels,omitempty"`
    Policy     *ManifestPolicy   `yaml:"policy,omitempty"`
}

type ManifestSetup struct {
    Image         string `yaml:"image,omitempty"`
    RepoPath      string `yaml:"repo_path,omitempty"`
    SSHHost       string `yaml:"ssh_host,omitempty"`
    SSHPort       int    `yaml:"ssh_port,omitempty"`
    SSHUser       string `yaml:"ssh_user,omitempty"`
    SSHKeyPath    string `yaml:"ssh_key_path,omitempty"`
    InstallDocker bool   `yaml:"install_docker,omitempty"`
}

type ManifestAgent struct {
    Name        string            `yaml:"name"`
    Type        string            `yaml:"type"`
    IAMIdentity string            `yaml:"iam_identity,omitempty"`
    Secrets     map[string]string `yaml:"secrets,omitempty"`
}

type ManifestChannel struct {
    Platform string            `yaml:"platform"`
    Secrets  map[string]string `yaml:"secrets,omitempty"`
    Bindings []ManifestBinding `yaml:"bindings,omitempty"`
}

type ManifestBinding struct {
    Agent string `yaml:"agent"`
    ID    string `yaml:"id"`
    Label string `yaml:"label,omitempty"`
}

type ManifestPolicy struct {
    Egress  *policy.EgressPolicy             `yaml:"egress,omitempty"`
    Routing *policy.RoutingPolicy            `yaml:"routing,omitempty"`
    Posture *policy.PostureDeclarations      `yaml:"posture,omitempty"`
    Agents  map[string]*policy.AgentOverride `yaml:"agents,omitempty"`
}
```

### 2.2 Apply Result Structs (`cli/internal/manifest/apply.go`)

```go
type ApplyResult struct {
    Steps []StepResult `json:"steps"`
}

type StepResult struct {
    Name    string   `json:"name"`
    Status  string   `json:"status"`   // "done", "skipped", "error"
    Details []string `json:"details,omitempty"`
}
```

---

## 3. API Interface — `cli/internal/manifest` Package

### 3.1 `Load(path string) (*Manifest, error)`

Reads the file at `path`, unmarshals YAML into `Manifest`. Returns wrapped errors for file-not-found and parse errors.

### 3.2 `Validate(m *Manifest) error`

Structural validation — no provider calls. Checks:

| Check | Error |
|---|---|
| `apiVersion` != `conga.dev/v1alpha1` | `unsupported apiVersion %q` |
| `kind` != `Environment` | `unsupported kind %q` |
| Agent name fails `common.ValidateAgentName()` | passthrough from common |
| Agent type not `"user"` or `"team"` | `agent %q: invalid type %q` |
| Duplicate agent names | `duplicate agent name %q` |
| Channel platform not registered | `unknown channel platform %q` |
| Duplicate channel platforms | `duplicate channel platform %q` |
| Binding references non-existent agent | `channel %q binding: agent %q not in agents list` |
| Binding missing `id` | `channel %q binding for agent %q: id is required` |
| Policy fails `PolicyFile.Validate()` | passthrough from policy package |

### 3.3 `ExpandSecrets(m *Manifest) error`

Walks `agents[].secrets` and `channels[].secrets`. For each value starting with `$`, expands via `os.Expand`. Returns error if any referenced var is empty/unset.

Implementation:

```go
func ExpandSecrets(m *Manifest) error {
    for i := range m.Agents {
        if err := expandMap(m.Agents[i].Secrets, "agent", m.Agents[i].Name); err != nil {
            return err
        }
    }
    for i := range m.Channels {
        if err := expandMap(m.Channels[i].Secrets, "channel", m.Channels[i].Platform); err != nil {
            return err
        }
    }
    return nil
}

func expandMap(secrets map[string]string, kind, name string) error {
    for k, v := range secrets {
        if !strings.HasPrefix(v, "$") {
            continue
        }
        var missing string
        expanded := os.Expand(v, func(key string) string {
            val := os.Getenv(key)
            if val == "" {
                missing = key
            }
            return val
        })
        if missing != "" {
            return fmt.Errorf("expanding secrets for %s %q: environment variable %s is not set", kind, name, missing)
        }
        secrets[k] = expanded
    }
    return nil
}
```

### 3.4 `Apply(ctx, prov, m, policyPath) (*ApplyResult, error)`

Orchestrator. Executes steps sequentially. Each step is a function that returns `StepResult`. Steps with no manifest content are skipped entirely.

```go
func Apply(ctx context.Context, prov provider.Provider, m *Manifest, policyPath string) (*ApplyResult, error) {
    result := &ApplyResult{}

    // Build step list based on manifest content
    type step struct {
        name string
        fn   func() (StepResult, error)
    }
    var steps []step

    if m.Setup != nil {
        steps = append(steps, step{"Setting up environment", func() (StepResult, error) { return applySetup(ctx, prov, m.Setup) }})
    }
    if len(m.Agents) > 0 {
        steps = append(steps, step{"Provisioning agents", func() (StepResult, error) { return applyAgents(ctx, prov, m.Agents) }})
        steps = append(steps, step{"Setting agent secrets", func() (StepResult, error) { return applySecrets(ctx, prov, m.Agents) }})
    }
    if len(m.Channels) > 0 {
        steps = append(steps, step{"Adding channels", func() (StepResult, error) { return applyChannels(ctx, prov, m.Channels) }})
        steps = append(steps, step{"Binding channels", func() (StepResult, error) { return applyBindings(ctx, prov, m.Channels) }})
    }
    if m.Policy != nil {
        steps = append(steps, step{"Deploying policy", func() (StepResult, error) { return applyPolicy(m.Policy, policyPath) }})
    }
    if len(steps) > 0 {
        steps = append(steps, step{"Refreshing agents", func() (StepResult, error) { return applyRefresh(ctx, prov) }})
    }

    total := len(steps)
    for i, s := range steps {
        ui.Infoln(fmt.Sprintf("[%d/%d] %s...", i+1, total, s.name))
        sr, err := s.fn()
        result.Steps = append(result.Steps, sr)
        if err != nil {
            return result, err
        }
    }

    return result, nil
}
```

---

## 4. Step Functions — Idempotency Logic

### 4.1 `applySetup`

```go
func applySetup(ctx context.Context, prov provider.Provider, setup *ManifestSetup) (StepResult, error) {
    // Check if already configured
    _, err := prov.ListAgents(ctx)
    if err == nil {
        return StepResult{Name: "setup", Status: "skipped", Details: []string{"already configured"}}, nil
    }

    cfg := &provider.SetupConfig{
        Image:         setup.Image,
        RepoPath:      setup.RepoPath,
        SSHHost:       setup.SSHHost,
        SSHPort:       setup.SSHPort,
        SSHUser:       setup.SSHUser,
        SSHKeyPath:    setup.SSHKeyPath,
        InstallDocker: setup.InstallDocker,
    }
    if err := prov.Setup(ctx, cfg); err != nil {
        return StepResult{Name: "setup", Status: "error"}, fmt.Errorf("setup: %w", err)
    }
    return StepResult{Name: "setup", Status: "done"}, nil
}
```

### 4.2 `applyAgents`

```go
func applyAgents(ctx context.Context, prov provider.Provider, agents []ManifestAgent) (StepResult, error) {
    sr := StepResult{Name: "agents", Status: "done"}
    for _, a := range agents {
        // Check if already exists
        if _, err := prov.GetAgent(ctx, a.Name); err == nil {
            sr.Details = append(sr.Details, a.Name+": already exists")
            continue
        }

        // Resolve gateway port
        existing, err := prov.ListAgents(ctx)
        if err != nil {
            return sr, fmt.Errorf("listing agents for port assignment: %w", err)
        }
        port := common.NextAvailablePort(existing)

        agentType := provider.AgentTypeUser
        if a.Type == "team" {
            agentType = provider.AgentTypeTeam
        }

        cfg := provider.AgentConfig{
            Name:        a.Name,
            Type:        agentType,
            GatewayPort: port,
            IAMIdentity: a.IAMIdentity,
        }
        if err := prov.ProvisionAgent(ctx, cfg); err != nil {
            return sr, fmt.Errorf("provisioning agent %q: %w", a.Name, err)
        }
        sr.Details = append(sr.Details, a.Name+": provisioned (port "+fmt.Sprint(port)+")")
    }
    if len(sr.Details) == 0 {
        sr.Status = "skipped"
    }
    return sr, nil
}
```

Note: Agents are provisioned **without** channel bindings. Bindings are applied in step 5 after channels are added. This avoids a dependency ordering issue.

### 4.3 `applySecrets`

```go
func applySecrets(ctx context.Context, prov provider.Provider, agents []ManifestAgent) (StepResult, error) {
    sr := StepResult{Name: "secrets", Status: "done"}
    count := 0
    for _, a := range agents {
        for name, value := range a.Secrets {
            if err := prov.SetSecret(ctx, a.Name, name, value); err != nil {
                return sr, fmt.Errorf("setting secret %q for agent %q: %w", name, a.Name, err)
            }
            count++
        }
        if len(a.Secrets) > 0 {
            sr.Details = append(sr.Details, fmt.Sprintf("%s: %d secrets", a.Name, len(a.Secrets)))
        }
    }
    if count == 0 {
        sr.Status = "skipped"
        sr.Details = []string{"no secrets defined"}
    }
    return sr, nil
}
```

Secrets are always overwritten — `SetSecret` is inherently idempotent.

### 4.4 `applyChannels`

```go
func applyChannels(ctx context.Context, prov provider.Provider, chans []ManifestChannel) (StepResult, error) {
    sr := StepResult{Name: "channels", Status: "done"}

    // Check existing channels
    existing, err := prov.ListChannels(ctx)
    if err != nil {
        return sr, fmt.Errorf("listing channels: %w", err)
    }
    configured := make(map[string]bool)
    for _, ch := range existing {
        if ch.Configured {
            configured[ch.Platform] = true
        }
    }

    for _, ch := range chans {
        if configured[ch.Platform] {
            sr.Details = append(sr.Details, ch.Platform+": already configured")
            continue
        }
        if err := prov.AddChannel(ctx, ch.Platform, ch.Secrets); err != nil {
            return sr, fmt.Errorf("adding channel %q: %w", ch.Platform, err)
        }
        sr.Details = append(sr.Details, ch.Platform+": added")
    }
    return sr, nil
}
```

### 4.5 `applyBindings`

```go
func applyBindings(ctx context.Context, prov provider.Provider, chans []ManifestChannel) (StepResult, error) {
    sr := StepResult{Name: "bindings", Status: "done"}
    count := 0

    for _, ch := range chans {
        for _, b := range ch.Bindings {
            // Check if already bound
            agent, err := prov.GetAgent(ctx, b.Agent)
            if err != nil {
                return sr, fmt.Errorf("getting agent %q for binding: %w", b.Agent, err)
            }
            alreadyBound := false
            for _, existing := range agent.Channels {
                if existing.Platform == ch.Platform && existing.ID == b.ID {
                    alreadyBound = true
                    break
                }
            }
            if alreadyBound {
                sr.Details = append(sr.Details, fmt.Sprintf("%s → %s:%s: already bound", b.Agent, ch.Platform, b.ID))
                continue
            }

            binding := channels.ChannelBinding{
                Platform: ch.Platform,
                ID:       b.ID,
                Label:    b.Label,
            }
            if err := prov.BindChannel(ctx, b.Agent, binding); err != nil {
                return sr, fmt.Errorf("binding agent %q to %s:%s: %w", b.Agent, ch.Platform, b.ID, err)
            }
            sr.Details = append(sr.Details, fmt.Sprintf("%s → %s:%s: bound", b.Agent, ch.Platform, b.ID))
            count++
        }
    }
    if count == 0 && len(sr.Details) > 0 {
        // All were already bound
    }
    return sr, nil
}
```

### 4.6 `applyPolicy`

```go
func applyPolicy(mp *ManifestPolicy, policyPath string) (StepResult, error) {
    pf := &policy.PolicyFile{
        APIVersion: policy.CurrentAPIVersion,
        Egress:     mp.Egress,
        Routing:    mp.Routing,
        Posture:    mp.Posture,
        Agents:     mp.Agents,
    }
    if err := policy.Save(pf, policyPath); err != nil {
        return StepResult{Name: "policy", Status: "error"}, fmt.Errorf("saving policy: %w", err)
    }
    return StepResult{Name: "policy", Status: "done", Details: []string{"saved to " + policyPath}}, nil
}
```

Policy is always overwritten. `policy.Save` handles validation and atomic write.

### 4.7 `applyRefresh`

```go
func applyRefresh(ctx context.Context, prov provider.Provider) (StepResult, error) {
    if err := prov.RefreshAll(ctx); err != nil {
        return StepResult{Name: "refresh", Status: "error"}, fmt.Errorf("refreshing agents: %w", err)
    }
    return StepResult{Name: "refresh", Status: "done"}, nil
}
```

Single `RefreshAll` at the end. Each provider's refresh logic handles:
- Regenerating `openclaw.json` with channel config
- Writing env files with secrets
- Restarting agent containers
- Reconnecting router networks
- Deploying egress proxy config from policy file

---

## 5. CLI Command (`cli/cmd/apply.go`)

```go
package cmd

import (
    "fmt"
    "path/filepath"

    "github.com/cruxdigital-llc/conga-line/cli/internal/manifest"
    "github.com/cruxdigital-llc/conga-line/cli/internal/ui"
    "github.com/spf13/cobra"
)

var applyFile string

func init() {
    applyCmd := &cobra.Command{
        Use:   "apply [manifest.yaml]",
        Short: "Apply a manifest to provision an environment",
        Long:  "Read a YAML manifest and execute all provisioning steps: setup, agents, secrets, channels, bindings, policy, and refresh.",
        Args:  cobra.MaximumNArgs(1),
        RunE:  applyRun,
    }
    applyCmd.Flags().StringVarP(&applyFile, "file", "f", "", "Path to manifest file")
    rootCmd.AddCommand(applyCmd)
}

func applyRun(cmd *cobra.Command, args []string) error {
    // Resolve file path: positional arg > -f flag
    path := applyFile
    if len(args) > 0 {
        path = args[0]
    }
    if path == "" {
        return fmt.Errorf("manifest file required: conga apply <manifest.yaml> or conga apply -f <file>")
    }

    // Load and validate
    m, err := manifest.Load(path)
    if err != nil {
        return fmt.Errorf("loading manifest: %w", err)
    }
    if err := manifest.Validate(m); err != nil {
        return fmt.Errorf("validating manifest: %w", err)
    }
    if err := manifest.ExpandSecrets(m); err != nil {
        return err
    }

    // Resolve policy path
    policyPath, err := defaultPolicyPath()
    if err != nil {
        return err
    }

    ctx, cancel := commandContext()
    defer cancel()

    result, err := manifest.Apply(ctx, prov, m, policyPath)
    if err != nil {
        if ui.OutputJSON && result != nil {
            ui.EmitJSON(result)
        }
        return err
    }

    if ui.OutputJSON {
        ui.EmitJSON(result)
        return nil
    }

    // Text summary
    fmt.Println()
    for _, s := range result.Steps {
        icon := "done"
        if s.Status == "skipped" {
            icon = "skipped"
        }
        fmt.Printf("  %s: %s\n", s.Name, icon)
    }
    fmt.Printf("\nEnvironment applied from %s\n", filepath.Base(path))
    return nil
}
```

Reuses `defaultPolicyPath()` from `cli/cmd/policy.go` (already exported within the `cmd` package).

### 5.1 JSON Output Mode

When `--output json` or `--json` is active:
- Step progress messages go to stderr via `ui.Infoln()`
- Final `ApplyResult` emitted to stdout via `ui.EmitJSON()`
- On error, partial result is emitted before returning the error

### 5.2 MCP Tool

**Deferred to a follow-up.** The `apply` command takes a file path, which doesn't map cleanly to MCP tool semantics (no filesystem access from LLM). The MCP server already has individual tools for each operation. A future `conga_apply` MCP tool could accept inline YAML content.

---

## 6. Edge Cases & Error Handling

| Scenario | Behavior |
|---|---|
| Missing manifest file | `loading manifest: open demo.yaml: no such file or directory` |
| Invalid YAML syntax | `loading manifest: yaml: ...` (passthrough from yaml.v3) |
| Unknown `apiVersion` | `validating manifest: unsupported apiVersion "v2"` |
| Unknown `kind` | `validating manifest: unsupported kind "Cluster"` |
| Missing env var for secret | `expanding secrets for agent "aaron": environment variable ANTHROPIC_API_KEY is not set` |
| Agent name with uppercase | `validating manifest: invalid agent name "Aaron": must be lowercase...` |
| Duplicate agent names | `validating manifest: duplicate agent name "aaron"` |
| Binding references missing agent | `validating manifest: channel "slack" binding: agent "bob" not in agents list` |
| Provider not configured | Setup step runs; if setup section missing, `prov.ListAgents()` errors propagate |
| Step fails mid-apply | Error returned with partial `ApplyResult`. Already-applied steps are idempotent on re-run. |
| Empty manifest (apiVersion + kind only) | Valid, no steps executed, prints "Environment applied" |
| Manifest with only `policy:` | Only policy save + refresh steps run |
| `$VAR` where VAR contains `$` | `os.Expand` does not recurse — single expansion only |
| Secret value is literal `$` | Must escape as `$$` (Go `os.Expand` convention) |

---

## 7. Data Safety

This feature **does not** touch agent data directories. All operations use existing Provider interface methods which already preserve agent data:

- `ProvisionAgent` creates data directories (first time only)
- `SetSecret` writes to secrets directory, not data
- `BindChannel` updates agent config, not data
- `RefreshAll` regenerates config and restarts containers, preserving volume mounts

No new filesystem operations introduced outside of the existing provider contract.

---

## 8. File Inventory

### New Files (5)

| File | Lines (est.) | Purpose |
|---|---|---|
| `cli/internal/manifest/manifest.go` | ~120 | Structs, Load, Validate, ExpandSecrets |
| `cli/internal/manifest/apply.go` | ~200 | Apply orchestrator + 7 step functions |
| `cli/cmd/apply.go` | ~70 | Cobra command |
| `cli/internal/manifest/manifest_test.go` | ~200 | Unit tests |
| `demo.yaml.example` | ~35 | Example manifest |

### Modified Files (1)

| File | Change |
|---|---|
| `DEMO.md` | Add "Fast Path" section describing `conga apply` alternative |

---

## 9. Test Plan

### 9.1 Unit Tests (`cli/internal/manifest/manifest_test.go`)

| Test | What it verifies |
|---|---|
| `TestLoad_ValidManifest` | Full manifest parses correctly, all fields populated |
| `TestLoad_MinimalManifest` | Only apiVersion + kind, everything else nil/empty |
| `TestLoad_FileNotFound` | Returns error (not panic) |
| `TestLoad_InvalidYAML` | Returns yaml parse error |
| `TestValidate_BadAPIVersion` | Rejects non-matching apiVersion |
| `TestValidate_BadKind` | Rejects non-"Environment" kind |
| `TestValidate_InvalidAgentName` | Rejects uppercase, special chars |
| `TestValidate_DuplicateAgentNames` | Rejects two agents with same name |
| `TestValidate_InvalidAgentType` | Rejects type other than user/team |
| `TestValidate_UnknownPlatform` | Rejects unregistered channel platform |
| `TestValidate_DuplicatePlatform` | Rejects two channels with same platform |
| `TestValidate_BindingMissingAgent` | Rejects binding referencing non-existent agent |
| `TestValidate_BindingMissingID` | Rejects binding with empty id |
| `TestValidate_EmptyManifest` | Valid (no agents, channels, policy) |
| `TestExpandSecrets_EnvVar` | `$VAR` expanded from environment |
| `TestExpandSecrets_BracketSyntax` | `${VAR}` expanded |
| `TestExpandSecrets_MissingVar` | Returns error naming the missing var |
| `TestExpandSecrets_LiteralValue` | Non-`$` strings pass through unchanged |
| `TestExpandSecrets_MultipleVars` | Multiple secrets expanded in one agent |
| `TestExpandSecrets_ChannelSecrets` | Channel secrets expanded correctly |
| `TestManifestPolicy_ToPolicyFile` | ManifestPolicy converts to valid PolicyFile |

### 9.2 Integration Verification (Manual)

1. `go build ./cli/...` — compiles
2. `go test ./cli/...` — all tests pass (existing + new)
3. `conga apply demo.yaml --provider local` — full provisioning
4. `conga apply demo.yaml --provider local` — idempotent re-apply
5. `conga status --agent aaron` + `conga status --agent team` — running
6. `conga policy get` — matches manifest policy
7. `conga apply demo.yaml --output json` — valid JSON output
