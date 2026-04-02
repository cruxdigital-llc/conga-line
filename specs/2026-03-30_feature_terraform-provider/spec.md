# Spec: Terraform Provider

## Overview

Build `terraform-provider-conga` — a Terraform provider that wraps the existing Go `Provider` interface for declarative lifecycle management of CongaLine environments.

## Architecture

The provider is a new interface layer alongside the CLI and MCP server. All three call the same `Provider` interface. No business logic is duplicated.

```
User Interfaces:  CLI  |  MCP Server  |  Terraform Provider
                   |         |                |
                   v         v                v
              Provider Interface (Go)
              Setup, ProvisionAgent, SetSecret, ...
                   |         |                |
              AWS Provider | Remote Provider | Local Provider
```

## Module Structure

Separate Go module at `terraform-provider-conga/` importing `github.com/cruxdigital-llc/conga-line/cli`.

```
terraform-provider-conga/
  main.go
  internal/provider/
    provider.go              # terraform-plugin-framework provider
    environment_resource.go  # conga_environment
    agent_resource.go        # conga_agent
    secret_resource.go       # conga_secret
    channel_resource.go      # conga_channel
    binding_resource.go      # conga_channel_binding
    policy_resource.go       # conga_policy
  go.mod
  go.sum
```

## Provider Configuration

```hcl
provider "conga" {
  provider_type = "local"  # "local", "remote", "aws"
  ssh_host      = ""       # remote only
  ssh_user      = ""       # remote only
  ssh_key_path  = ""       # remote only
  region        = ""       # aws only
  profile       = ""       # aws only
}
```

Maps to `provider.Get(providerType, cfg)`.

## Resource CRUD Mapping

| Resource | Create | Read | Update | Delete |
|---|---|---|---|---|
| `conga_environment` | `Setup()` | `ListAgents()` (existence) | `Setup()` (idempotent) | `Teardown()` |
| `conga_agent` | `ProvisionAgent()` | `GetAgent()` | N/A (immutable) | `RemoveAgent()` |
| `conga_secret` | `SetSecret()` | `ListSecrets()` | `SetSecret()` | `DeleteSecret()` |
| `conga_channel` | `AddChannel()` | `ListChannels()` | `AddChannel()` | `RemoveChannel()` |
| `conga_channel_binding` | `BindChannel()` | `GetAgent().Channels` | Recreate | `UnbindChannel()` |
| `conga_policy` | `Save()+RefreshAll()` | `Load()` | `Save()+RefreshAll()` | `os.Remove()` |

## Data Sources

| Data Source | Method |
|---|---|
| `conga_agent_status` | `GetAgent()` + `GetStatus()` |
| `conga_policy` | `policy.Load()` |
| `conga_channels` | `ListChannels()` |

## Key Design Decisions

1. **terraform-plugin-framework** (not deprecated SDK)
2. **One provider instance per plan** — `Configure()` creates a single `Provider`
3. **Agent is immutable** — name/type changes force recreate (`RequiresReplace`)
4. **Secret sensitivity** — `Sensitive: true` on value attribute
5. **RefreshAll after policy** — matches `conga policy deploy` behavior
6. **Import by name** — `terraform import conga_agent.aaron aaron`
7. **Channel secrets in state** — token names (not values) for drift detection

## Key Import Paths

```go
import "github.com/cruxdigital-llc/conga-line/cli/internal/provider"
import "github.com/cruxdigital-llc/conga-line/cli/internal/policy"
import "github.com/cruxdigital-llc/conga-line/cli/internal/channels"
import "github.com/cruxdigital-llc/conga-line/cli/internal/common"
```

## Reference: Provider Interface Methods Used

- `Name() string`
- `Setup(ctx, *SetupConfig) error`
- `Teardown(ctx) error`
- `ProvisionAgent(ctx, AgentConfig) error`
- `GetAgent(ctx, name) (*AgentConfig, error)`
- `ListAgents(ctx) ([]AgentConfig, error)`
- `RemoveAgent(ctx, name, deleteSecrets) error`
- `GetStatus(ctx, agentName) (*AgentStatus, error)`
- `RefreshAgent(ctx, agentName) error`
- `RefreshAll(ctx) error`
- `SetSecret(ctx, agentName, secretName, value) error`
- `ListSecrets(ctx, agentName) ([]SecretEntry, error)`
- `DeleteSecret(ctx, agentName, secretName) error`
- `AddChannel(ctx, platform, secrets) error`
- `RemoveChannel(ctx, platform) error`
- `ListChannels(ctx) ([]ChannelStatus, error)`
- `BindChannel(ctx, agentName, ChannelBinding) error`
- `UnbindChannel(ctx, agentName, platform) error`
