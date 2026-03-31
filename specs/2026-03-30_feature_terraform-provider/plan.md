# Plan: Terraform Provider

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    User Interfaces                       │
├──────────────┬──────────────┬───────────────────────────┤
│ conga CLI    │ MCP Server   │ Terraform Provider        │
│ (interactive)│ (AI-driven)  │ (declarative lifecycle)   │
└──────┬───────┴──────┬───────┴───────────┬───────────────┘
       │              │                   │
       ▼              ▼                   ▼
┌─────────────────────────────────────────────────────────┐
│              Provider Interface (Go)                     │
│  Setup, ProvisionAgent, SetSecret, AddChannel,          │
│  BindChannel, RefreshAgent, GetStatus, ...              │
├──────────────┬──────────────┬───────────────────────────┤
│ AWS Provider │Remote Provider│ Local Provider            │
│ (EC2 + SSM)  │ (SSH + Docker)│ (Docker CLI)             │
└──────────────┴──────────────┴───────────────────────────┘
```

The Terraform provider is a new interface layer — same as the CLI and MCP server. All three call the same Go `Provider` interface. No business logic is duplicated.

## Provider Configuration

```hcl
provider "conga" {
  provider_type = "remote"  # "local", "remote", "aws"

  # Remote provider
  ssh_host     = "demo.example.com"
  ssh_user     = "ubuntu"
  ssh_key_path = "~/.ssh/id_ed25519"

  # AWS provider
  # region  = "us-east-2"
  # profile = "my-aws-profile"
}
```

Maps to `provider.Get(providerType, cfg)` — identical to CLI's `PersistentPreRunE`.

## Resource Model

### Resources

| Resource | Create | Read | Update | Delete |
|---|---|---|---|---|
| `conga_environment` | `prov.Setup()` | `prov.ListAgents()` (existence) | `prov.Setup()` (idempotent) | `prov.Teardown()` |
| `conga_agent` | `prov.ProvisionAgent()` | `prov.GetAgent()` | N/A (immutable, recreate) | `prov.RemoveAgent()` |
| `conga_secret` | `prov.SetSecret()` | `prov.ListSecrets()` | `prov.SetSecret()` | `prov.DeleteSecret()` |
| `conga_channel` | `prov.AddChannel()` | `prov.ListChannels()` | `prov.AddChannel()` (idempotent) | `prov.RemoveChannel()` |
| `conga_channel_binding` | `prov.BindChannel()` | `prov.GetAgent().Channels` | Recreate | `prov.UnbindChannel()` |
| `conga_policy` | `policy.Save()` + `prov.RefreshAll()` | `policy.Load()` | `policy.Save()` + `prov.RefreshAll()` | `os.Remove()` |

### Data Sources

| Data Source | Method |
|---|---|
| `conga_agent_status` | `prov.GetAgent()` + `prov.GetStatus()` |
| `conga_policy` | `policy.Load()` |
| `conga_channels` | `prov.ListChannels()` |

### Example HCL

```hcl
resource "conga_environment" "main" {
  image          = "ghcr.io/openclaw/openclaw:2026.3.11"
  install_docker = true
}

resource "conga_agent" "aaron" {
  name       = "aaron"
  type       = "user"
  depends_on = [conga_environment.main]
}

resource "conga_agent" "team" {
  name       = "team"
  type       = "team"
  depends_on = [conga_environment.main]
}

resource "conga_secret" "aaron_api_key" {
  agent = conga_agent.aaron.name
  name  = "anthropic-api-key"
  value = var.anthropic_api_key
}

resource "conga_channel" "slack" {
  platform       = "slack"
  bot_token      = var.slack_bot_token
  signing_secret = var.slack_signing_secret
  app_token      = var.slack_app_token
  depends_on     = [conga_environment.main]
}

resource "conga_channel_binding" "aaron_slack" {
  agent   = conga_agent.aaron.name
  channel = conga_channel.slack.platform
  id      = "U0ANSPZPG9X"
}

resource "conga_policy" "main" {
  egress {
    mode            = "enforce"
    allowed_domains = ["api.anthropic.com", "*.slack.com", "*.slack-edge.com"]
  }
}
```

## What Terraform Gives Us (Not Reimplemented)

| Capability | How Terraform handles it |
|---|---|
| **State management** | `terraform.tfstate` tracks what's deployed |
| **Drift detection** | `terraform plan` compares state to live infrastructure |
| **Dependency resolution** | `depends_on` + implicit references |
| **Diff preview** | `terraform plan` shows what will change before applying |
| **Destroy** | `terraform destroy` removes everything in reverse dependency order |
| **Import** | `terraform import` adopts existing resources into state |
| **Partial apply** | `-target` flag applies specific resources |
| **Modules** | Reusable agent topology templates |

## Relationship to conga bootstrap

| | `conga bootstrap` | `terraform-provider-conga` |
|---|---|---|
| **Audience** | Demos, personal use, quick setup | Teams, production, enterprise |
| **State** | None — additive only, no removals | Full Terraform state management |
| **Drift** | No detection | `terraform plan` detects drift |
| **Destroy** | `conga admin teardown` (separate) | `terraform destroy` (integrated) |
| **Config format** | YAML manifest | HCL (`.tf` files) |
| **Dependencies** | Just the `conga` CLI | Terraform + provider binary |
| **Use case** | "Get running in 60 seconds" | "Manage a fleet in production" |

Both share the same underlying Go provider packages.

## Implementation Approach

### Separate Go Module

```
terraform-provider-conga/
├── main.go                         # Plugin server entry point
├── internal/
│   └── provider/
│       ├── provider.go             # terraform-plugin-framework provider
│       ├── environment_resource.go # conga_environment
│       ├── agent_resource.go       # conga_agent
│       ├── secret_resource.go      # conga_secret
│       ├── channel_resource.go     # conga_channel
│       ├── binding_resource.go     # conga_channel_binding
│       └── policy_resource.go      # conga_policy
├── go.mod                          # imports github.com/cruxdigital-llc/conga-line/cli
└── go.sum
```

Each resource file follows the `terraform-plugin-framework` pattern:
- Schema definition (attributes, types, sensitivity)
- CRUD methods that call `Provider` interface
- Import state function

### Key Design Decisions

1. **One provider instance per plan**: The Terraform provider creates a single `provider.Provider` (local/remote/AWS) during `Configure()` and reuses it for all resource operations. Same lifecycle as the CLI's `prov` variable.

2. **RefreshAll after policy changes**: The `conga_policy` resource calls `prov.RefreshAll()` in Create/Update to deploy the policy to agents — matching `conga policy deploy` behavior.

3. **Agent is immutable**: Changing `type` forces recreate (Terraform's `RequiresReplace`). Name changes also force recreate.

4. **Secret sensitivity**: The `value` attribute on `conga_secret` uses `Sensitive: true` — Terraform redacts it in plan output and state is encrypted at rest (if using remote backend).

5. **Import by name**: `terraform import conga_agent.aaron aaron` imports by agent name. `terraform import conga_channel.slack slack` imports by platform name. `terraform import conga_policy.main policy` imports the singleton policy.

6. **Channel secrets in state**: `conga_channel` stores token names (not values) in state for drift detection. Actual values are only sent during Create/Update. This matches Terraform's pattern for provider credentials.

## Phases (When Implemented)

1. **Provider skeleton** — plugin-framework boilerplate, provider config, `provider.Get()` wiring
2. **Core resources** — `conga_environment`, `conga_agent`, `conga_secret` (covers the minimal viable setup)
3. **Channel resources** — `conga_channel`, `conga_channel_binding`
4. **Policy resource** — `conga_policy` with egress, routing, posture blocks
5. **Data sources** — `conga_agent_status`, `conga_policy`, `conga_channels`
6. **Import support** — import functions for all resources
7. **Acceptance tests** — using local provider (no external dependencies)
8. **Registry publishing** — Terraform Registry under `cruxdigital-llc/conga`

## Persona Review Checklist

### Architect
- [ ] No business logic duplication — provider calls existing `Provider` interface
- [ ] Separate Go module, clean import boundary
- [ ] Resource model maps 1:1 to Provider methods
- [ ] Consistent with existing CLI/MCP pattern (three interfaces, one engine)

### Product Manager
- [ ] Clear audience separation: bootstrap (personal) vs Terraform (enterprise)
- [ ] Familiar UX for Terraform users — standard resource patterns
- [ ] Import path for existing environments provisioned via CLI
- [ ] No forced migration — both paths coexist indefinitely

### QA
- [ ] Acceptance test strategy using local provider
- [ ] Drift detection tested (modify live state, verify plan detects it)
- [ ] Destroy order tested (reverse dependencies)
- [ ] Import tested for each resource type
- [ ] Sensitive values never appear in plan output
