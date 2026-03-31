# Plan: Manifest Bootstrap

## Approach

Add a `conga bootstrap <manifest.yaml>` command backed by a new `cli/internal/manifest/` package. The command parses a YAML manifest, expands environment variable references in secret values, validates the structure, then executes provisioning steps sequentially through the existing `Provider` interface. Each step is idempotent — re-running skips completed work.

## YAML Manifest Format

```yaml
apiVersion: conga.dev/v1alpha1
kind: Environment

setup:
  image: "ghcr.io/openclaw/openclaw:2026.3.11"
  ssh_host: "demo.example.com"
  ssh_user: "ubuntu"
  ssh_key_path: "~/.ssh/id_ed25519"
  install_docker: true

agents:
  - name: aaron
    type: user
    secrets:
      anthropic-api-key: "$ANTHROPIC_API_KEY"

  - name: team
    type: team
    secrets:
      anthropic-api-key: "$ANTHROPIC_API_KEY"

channels:
  - platform: slack
    secrets:
      slack-bot-token: "$SLACK_BOT_TOKEN"
      slack-signing-secret: "$SLACK_SIGNING_SECRET"
      slack-app-token: "$SLACK_APP_TOKEN"
    bindings:
      - agent: aaron
        id: "U0ANSPZPG9X"
      - agent: team
        id: "C0ANFAD41GB"

policy:
  egress:
    mode: enforce
    allowed_domains:
      - "api.anthropic.com"
      - "*.slack.com"
      - "*.slack-edge.com"
```

### Format Decisions

| Decision | Rationale |
|---|---|
| `$VAR` for secrets | Familiar from shell/Docker. Never stored in YAML. `os.Expand` handles both `$VAR` and `${VAR}`. |
| Bindings under `channels` | Channel must exist before bindings. Groups all Slack config in one place. |
| Auto-assigned gateway ports | Keeps manifest clean. Uses existing `common.NextAvailablePort()`. |
| Inline `policy` section | "Single file describes everything." Uses exact `PolicyFile` schema from `cli/internal/policy/`. |
| `kind: Environment` | Reserved for future manifest types (e.g., `kind: Agent` for single-agent manifests). |
| All sections optional | Manifest with only `agents:` is valid — useful for adding agents to an existing environment. |

## Execution Order

```
[1/7] Setting up environment...     → prov.Setup(ctx, &SetupConfig{...})
[2/7] Provisioning agents...        → prov.ProvisionAgent(ctx, cfg) × N
[3/7] Setting agent secrets...      → prov.SetSecret(ctx, agent, name, val) × N
[4/7] Adding channels...            → prov.AddChannel(ctx, platform, secrets) × N
[5/7] Binding channels...           → prov.BindChannel(ctx, agent, binding) × N
[6/7] Deploying policy...           → policy.Save() to ~/.conga/conga-policy.yaml
[7/7] Refreshing all agents...      → prov.RefreshAll(ctx)
```

### Idempotency

| Step | Check | If exists |
|---|---|---|
| Setup | `prov.ListAgents(ctx)` succeeds | Skip, print "already configured" |
| Provision | `prov.GetAgent(ctx, name)` succeeds | Skip, print "already exists" |
| Secrets | — | Always overwrite (inherently idempotent) |
| Add channel | `prov.ListChannels(ctx)` has platform with `Configured: true` | Skip, print "already configured" |
| Bind channel | `GetAgent` shows matching binding | Skip, print "already bound" |
| Policy | — | Always overwrite (idempotent) |
| Refresh | — | Always run |

### Step Skipping

Steps without manifest content are skipped entirely (not counted):
- No `setup:` section → skip step 1
- No `agents:` → skip steps 2-3
- No `channels:` → skip steps 4-5
- No `policy:` → skip step 6
- No agents provisioned and nothing changed → skip step 7

The step counter adjusts to show only active steps (e.g., `[1/4]` if setup and policy are omitted).

## Implementation Phases

### Phase 1: Manifest Package — Parse & Validate (`cli/internal/manifest/manifest.go`)

**New file.** Structs + parsing + validation + env var expansion.

```go
type Manifest struct {
    APIVersion string            `yaml:"apiVersion"`
    Kind       string            `yaml:"kind"`
    Setup      *ManifestSetup    `yaml:"setup,omitempty"`
    Agents     []ManifestAgent   `yaml:"agents,omitempty"`
    Channels   []ManifestChannel `yaml:"channels,omitempty"`
    Policy     *ManifestPolicy   `yaml:"policy,omitempty"`
}

type ManifestSetup struct {
    Image        string `yaml:"image,omitempty"`
    SSHHost      string `yaml:"ssh_host,omitempty"`
    SSHPort      int    `yaml:"ssh_port,omitempty"`
    SSHUser      string `yaml:"ssh_user,omitempty"`
    SSHKeyPath   string `yaml:"ssh_key_path,omitempty"`
    RepoPath     string `yaml:"repo_path,omitempty"`
    InstallDocker bool  `yaml:"install_docker,omitempty"`
}

type ManifestAgent struct {
    Name        string            `yaml:"name"`
    Type        string            `yaml:"type"`  // "user" or "team"
    IAMIdentity string            `yaml:"iam_identity,omitempty"`
    Secrets     map[string]string `yaml:"secrets,omitempty"`
}

type ManifestChannel struct {
    Platform string              `yaml:"platform"`
    Secrets  map[string]string   `yaml:"secrets,omitempty"`
    Bindings []ManifestBinding   `yaml:"bindings,omitempty"`
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

Functions:
- `Load(path string) (*Manifest, error)` — read file, unmarshal YAML
- `Validate(m *Manifest) error` — check apiVersion, kind, agent names (via `common.ValidateAgentName`), agent types, binding references (agent names must exist in `agents` list), channel platforms
- `ExpandSecrets(m *Manifest) error` — walk all secret maps, expand `$VAR`/`${VAR}` via `os.Expand`, error if any referenced var is empty/unset

### Phase 2: Apply Engine (`cli/internal/manifest/apply.go`)

**New file.** Orchestrates the execution sequence.

```go
type ApplyResult struct {
    Steps []StepResult `json:"steps"`
}

type StepResult struct {
    Name    string   `json:"name"`
    Status  string   `json:"status"`  // "done", "skipped", "error"
    Details []string `json:"details,omitempty"`
}

func Apply(ctx context.Context, prov provider.Provider, m *Manifest, policyPath string) (*ApplyResult, error)
```

Step functions (unexported):
- `applySetup(ctx, prov, m.Setup) → StepResult`
- `applyAgents(ctx, prov, m.Agents) → StepResult`
- `applySecrets(ctx, prov, m.Agents) → StepResult`
- `applyChannels(ctx, prov, m.Channels) → StepResult`
- `applyBindings(ctx, prov, m.Channels) → StepResult`
- `applyPolicy(m.Policy, policyPath) → StepResult`
- `applyRefresh(ctx, prov) → StepResult`

Each returns a `StepResult`. On error, `Apply` returns immediately with the partial result + error.

### Phase 3: CLI Command (`cli/cmd/apply.go`)

**New file.** Cobra command registration.

```go
conga bootstrap [manifest.yaml]
conga bootstrap -f manifest.yaml
```

- Resolves file path (positional arg > `-f` flag)
- Calls `manifest.Load` → `manifest.Validate` → `manifest.ExpandSecrets` → `manifest.Apply`
- Text mode: prints step progress with spinners (reusing `ui.NewSpinner`)
- JSON mode: emits `ApplyResult`

No changes to `root.go` needed — the command self-registers via `init()` like all other commands. The `prov` variable is already initialized by `PersistentPreRunE`.

### Phase 4: Tests (`cli/internal/manifest/manifest_test.go`)

**New file.** Unit tests for:
- YAML parsing (valid manifest, minimal manifest, empty sections)
- Validation (bad apiVersion, bad kind, invalid agent name, duplicate agent names, binding references non-existent agent, unknown agent type)
- Env var expansion (single var, multiple vars, missing var → error, literal value passthrough, `${VAR}` syntax)
- `ManifestPolicy` → `PolicyFile` mapping

### Phase 5: Demo Manifest + Documentation

- Create `demo.yaml.example` in project root — the example manifest matching the DEMO.md flow
- Update DEMO.md with a `conga bootstrap` fast-path section

## Files Inventory

### New Files (5)

| File | Purpose |
|---|---|
| `cli/internal/manifest/manifest.go` | Structs, Load, Validate, ExpandSecrets |
| `cli/internal/manifest/apply.go` | Apply orchestrator + step functions |
| `cli/cmd/apply.go` | Cobra command |
| `cli/internal/manifest/manifest_test.go` | Unit tests |
| `demo.yaml.example` | Example manifest for demos |

### Modified Files (1)

| File | Change |
|---|---|
| `DEMO.md` | Add fast-path section referencing `conga bootstrap` |

### Reused Existing Code

| What | Where |
|---|---|
| `provider.Provider` interface (17 methods) | `cli/internal/provider/provider.go` |
| `provider.SetupConfig` | `cli/internal/provider/setup_config.go` |
| `provider.AgentConfig`, `provider.AgentType` | `cli/internal/provider/provider.go` |
| `channels.ChannelBinding` | `cli/internal/channels/channels.go` |
| `policy.PolicyFile`, `policy.Save` | `cli/internal/policy/policy.go`, `mutate.go` |
| `common.ValidateAgentName` | `cli/internal/common/validate.go` |
| `common.NextAvailablePort` | `cli/internal/common/ports.go` |
| `ui.NewSpinner`, `ui.EmitJSON` | `cli/internal/ui/` |
| `gopkg.in/yaml.v3` | Already in go.mod |

## Persona Review Checklist

### Architect
- [ ] No new dependencies (reuses `gopkg.in/yaml.v3`)
- [ ] No new Provider interface methods
- [ ] Manifest package is self-contained — no imports from `cmd/`
- [ ] Policy section reuses existing `policy.*` types directly
- [ ] Consistent with existing patterns (Cobra init, provider abstraction)

### Product Manager
- [ ] Solves the "demo takes too long" problem
- [ ] YAML format is human-readable and demo-friendly
- [ ] Secret safety: `$VAR` references, never literals in YAML
- [ ] Extensible for future production use (kind field, optional sections)
- [ ] Success measurable: time-to-provision, re-apply without errors

### QA
- [ ] Unhappy paths covered: missing env var, bad YAML, invalid agent name, duplicate agents
- [ ] Idempotency verified: each step checks before acting
- [ ] Partial failure: error mid-apply leaves environment in consistent state (already-applied steps are idempotent)
- [ ] Binding validation: agent must exist in manifest's agents list
- [ ] Empty manifest: valid but no-op

## Verification Plan

1. `go build ./cli/...` — compiles
2. `go test ./cli/internal/manifest/...` — unit tests pass
3. `go test ./cli/...` — all existing tests still pass
4. Create `demo.yaml` with real env vars, run `conga bootstrap demo.yaml --provider local`
5. Run `conga bootstrap demo.yaml` again — verify idempotent (steps show "skipped")
6. `conga status --agent aaron` + `conga status --agent team` — both running
7. `conga policy get` — matches manifest policy
8. `conga bootstrap demo.yaml --output json` — valid JSON output
