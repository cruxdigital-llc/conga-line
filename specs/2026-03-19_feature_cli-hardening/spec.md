# Spec: CLI Hardening — Design, Reliability & Test Coverage

## Overview

Harden the `cruxclaw` CLI by fixing silent failures, tightening input validation, refactoring for testability, and establishing baseline test coverage. No new commands or features — this is a reliability and maintainability pass on the existing 13-command CLI.

---

## 1. Bug Fixes

### 1a. Check `json.Marshal` Errors

**File:** `cli/cmd/admin.go`

**Current (line 227):**
```go
agentConfigJSON, _ := json.Marshal(map[string]interface{}{...})
```

**Fixed:**
```go
agentConfigJSON, err := json.Marshal(map[string]interface{}{
    "type":            "user",
    "slack_member_id": slackMemberID,
    "gateway_port":    gatewayPort,
    "iam_identity":    iamIdentity,
})
if err != nil {
    return fmt.Errorf("failed to serialize agent config: %w", err)
}
```

Same fix at line 308 for the team agent variant.

### 1b. Report Cleanup Errors in `remove-agent`

**File:** `cli/cmd/admin.go`

**Current (lines 442-457):** Errors silently discarded.

**Fixed:** Collect errors and report at the end:

```go
var cleanupErrs []string

if err := awsutil.DeleteParameter(ctx, clients.SSM, fmt.Sprintf("/openclaw/agents/%s", agentName)); err != nil {
    cleanupErrs = append(cleanupErrs, fmt.Sprintf("SSM parameter: %v", err))
}

if adminDeleteSecrets {
    secretPrefix := fmt.Sprintf("openclaw/agents/%s/", agentName)
    secrets, err := awsutil.ListSecrets(ctx, clients.SecretsManager, secretPrefix)
    if err != nil {
        cleanupErrs = append(cleanupErrs, fmt.Sprintf("list secrets: %v", err))
    } else {
        for _, s := range secrets {
            if err := awsutil.DeleteSecret(ctx, clients.SecretsManager, fmt.Sprintf("openclaw/agents/%s/%s", agentName, s.Name)); err != nil {
                cleanupErrs = append(cleanupErrs, fmt.Sprintf("delete secret %s: %v", s.Name, err))
            }
        }
    }
}

if _, err := awsutil.RunCommand(ctx, clients.SSM, instanceID, "/opt/openclaw/bin/update-dashboard.sh", 30*time.Second); err != nil {
    cleanupErrs = append(cleanupErrs, fmt.Sprintf("dashboard update: %v", err))
}

if len(cleanupErrs) > 0 {
    fmt.Printf("Agent %s removed, but %d cleanup operation(s) failed:\n", agentName, len(cleanupErrs))
    for _, e := range cleanupErrs {
        fmt.Printf("  - %s\n", e)
    }
} else {
    fmt.Printf("Agent %s removed.\n", agentName)
}
```

### 1c. Wrap `DeleteSecret` Error

**File:** `cli/internal/aws/secrets.go`

**Current:**
```go
func DeleteSecret(...) error {
    _, err := client.DeleteSecret(...)
    return err
}
```

**Fixed:**
```go
func DeleteSecret(ctx context.Context, client *secretsmanager.Client, name string) error {
    _, err := client.DeleteSecret(ctx, &secretsmanager.DeleteSecretInput{
        SecretId:                   aws.String(name),
        ForceDeleteWithoutRecovery: aws.Bool(true),
    })
    if err != nil {
        return fmt.Errorf("failed to delete secret %s: %w", name, err)
    }
    return nil
}
```

---

## 2. Input Validation Tightening

### 2a. Slack ID Validation

**File:** `cli/cmd/root.go`

**Current:**
```go
var validIDPattern = regexp.MustCompile(`^[A-Z0-9]+$`)
var validChannelPattern = regexp.MustCompile(`^[A-Z0-9]+$`)
```

**Fixed:**
```go
var validMemberIDPattern = regexp.MustCompile(`^U[A-Z0-9]{10}$`)
var validChannelIDPattern = regexp.MustCompile(`^C[A-Z0-9]{10}$`)
```

Update `validateMemberID`:
```go
func validateMemberID(id string) error {
    if !validMemberIDPattern.MatchString(id) {
        return fmt.Errorf("invalid Slack member ID %q: must start with 'U' followed by 10 alphanumeric characters (e.g., U0123456789)", id)
    }
    return nil
}
```

Update `validateChannelID`:
```go
func validateChannelID(id string) error {
    if !validChannelIDPattern.MatchString(id) {
        return fmt.Errorf("invalid Slack channel ID %q: must start with 'C' followed by 10 alphanumeric characters (e.g., C0123456789)", id)
    }
    return nil
}
```

---

## 3. Context Timeout

### 3a. Global Timeout Flag

**File:** `cli/cmd/root.go`

Add flag:
```go
var flagTimeout time.Duration

func init() {
    rootCmd.PersistentFlags().DurationVar(&flagTimeout, "timeout", 5*time.Minute, "Global timeout for AWS operations")
}
```

### 3b. Command Context

Replace `context.Background()` in all command handlers with:
```go
ctx, cancel := context.WithTimeout(context.Background(), flagTimeout)
defer cancel()
```

Commands that already do this (`connect` uses `WithCancel`) keep their pattern but add the timeout:
```go
ctx, cancel := context.WithTimeout(context.Background(), flagTimeout)
defer cancel()
```

---

## 4. Connect Goroutine Cleanup

**File:** `cli/cmd/connect.go`

**Current:**
```go
go pollDevicePairing(ctx, instanceID, agentName)
```

**Fixed:** Add verbose logging and respect context cancellation around `RunCommand` calls:

```go
func pollDevicePairing(ctx context.Context, instanceID, agentName string, verbose bool) {
    fmt.Println("Watching for device pairing requests...")

    for i := 0; i < 30; i++ {
        select {
        case <-time.After(5 * time.Second):
        case <-ctx.Done():
            return
        }

        listScript := fmt.Sprintf("docker exec openclaw-%s npx openclaw devices list --json 2>&1", agentName)
        result, err := awsutil.RunCommand(ctx, clients.SSM, instanceID, listScript, 30*time.Second)
        if err != nil {
            if ctx.Err() != nil {
                return // Context cancelled, exit cleanly
            }
            if verbose {
                fmt.Fprintf(os.Stderr, "[verbose] device pairing poll error: %v\n", err)
            }
            continue
        }

        if !strings.Contains(result.Stdout, "pending") && !strings.Contains(result.Stdout, "Pending") {
            continue
        }

        approveScript := fmt.Sprintf("docker exec openclaw-%s npx openclaw devices approve --latest 2>&1", agentName)
        result, err = awsutil.RunCommand(ctx, clients.SSM, instanceID, approveScript, 30*time.Second)
        if err != nil {
            if verbose {
                fmt.Fprintf(os.Stderr, "[verbose] device pairing approve error: %v\n", err)
            }
            continue
        }

        fmt.Printf("Device paired! Refresh your browser.\n")
        return
    }
}
```

Call site:
```go
go pollDevicePairing(ctx, instanceID, agentName, flagVerbose)
```

---

## 5. UX Fixes

### 5a. Env Var Preview in `secrets set`

**File:** `cli/cmd/secrets.go`

After resolving the secret name (both interactive and argument paths), before prompting for value:

```go
fmt.Printf("  -> will be injected as: %s\n", secretNameToEnvVar(name))
```

This line already exists in the interactive path (line 86). Add it to the argument path too, after line 76:

```go
if len(args) > 0 {
    name = args[0]
    fmt.Printf("  -> will be injected as: %s\n", secretNameToEnvVar(name))
} else {
    // ... existing interactive path
}
```

### 5b. Next-Steps with `--agent` Flag

**File:** `cli/cmd/admin.go`

**Current:**
```go
fmt.Printf("  1. cruxclaw secrets set anthropic-api-key\n")
fmt.Printf("  2. cruxclaw refresh\n")
fmt.Printf("  3. cruxclaw connect\n")
```

**Fixed:**
```go
fmt.Printf("  1. cruxclaw secrets set anthropic-api-key --agent %s\n", agentName)
fmt.Printf("  2. cruxclaw refresh --agent %s\n", agentName)
fmt.Printf("  3. cruxclaw connect --agent %s\n", agentName)
```

### 5c. Human-Readable Uptime in `status`

**File:** `cli/cmd/status.go`

Add helper:
```go
func formatUptime(started string) string {
    t, err := time.Parse(time.RFC3339Nano, started)
    if err != nil {
        return started // fallback to raw timestamp
    }
    d := time.Since(t)
    switch {
    case d < time.Minute:
        return fmt.Sprintf("%ds", int(d.Seconds()))
    case d < time.Hour:
        return fmt.Sprintf("%dm", int(d.Minutes()))
    case d < 24*time.Hour:
        return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
    default:
        days := int(d.Hours()) / 24
        hours := int(d.Hours()) % 24
        return fmt.Sprintf("%dd %dh", days, hours)
    }
}
```

Update display:
```go
if started := kv["CONTAINER_STARTED"]; started != "" {
    fmt.Printf("Started:    %s (up %s)\n", started, formatUptime(started))
}
```

---

## 6. Testability Refactoring

### 6a. Host Executor Interface (High-Level Abstraction)

The CLI currently couples command logic to AWS SSM for remote script execution. Every command that touches the host calls `awsutil.RunCommand(ctx, clients.SSM, instanceID, script, timeout)`. This works for the AWS deployment, but a **local mode** (managing a local Docker instance without SSM) is a planned fast-follow.

To support both backends with the same CLI commands, we introduce a `HostExecutor` interface at the boundary between "what to do" and "how to reach the host."

**New file:** `cli/internal/executor/executor.go`

```go
package executor

import (
    "context"
    "time"
)

// Result represents the output of a command executed on the host.
type Result struct {
    Status string // "Success" or "Failed"
    Stdout string
    Stderr string
}

// HostExecutor abstracts how the CLI executes scripts on the target host.
// The AWS implementation uses SSM SendCommand + polling.
// A local implementation would use exec.Command directly.
type HostExecutor interface {
    // RunScript executes a shell script on the host and returns its output.
    RunScript(ctx context.Context, script string, timeout time.Duration) (*Result, error)

    // InstanceID returns the identifier for the target host.
    // For AWS this is the EC2 instance ID; for local mode this could be "localhost".
    InstanceID() string
}
```

**New file:** `cli/internal/executor/ssm.go`

```go
package executor

import (
    "context"
    "time"

    awsutil "github.com/cruxdigital-llc/openclaw-template/cli/internal/aws"
)

// SSMExecutor executes scripts on a remote EC2 instance via AWS SSM.
type SSMExecutor struct {
    client     awsutil.SSMClient
    instanceID string
}

func NewSSMExecutor(client awsutil.SSMClient, instanceID string) *SSMExecutor {
    return &SSMExecutor{client: client, instanceID: instanceID}
}

func (e *SSMExecutor) RunScript(ctx context.Context, script string, timeout time.Duration) (*Result, error) {
    r, err := awsutil.RunCommand(ctx, e.client, e.instanceID, script, timeout)
    if err != nil {
        return nil, err
    }
    return &Result{Status: r.Status, Stdout: r.Stdout, Stderr: r.Stderr}, nil
}

func (e *SSMExecutor) InstanceID() string {
    return e.instanceID
}
```

**Future (not in this spec):** `cli/internal/executor/local.go`

```go
// LocalExecutor executes scripts directly on the local machine.
// This would be the implementation for `cruxclaw --local` mode.
type LocalExecutor struct{}

func (e *LocalExecutor) RunScript(ctx context.Context, script string, timeout time.Duration) (*Result, error) {
    ctx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()
    cmd := exec.CommandContext(ctx, "bash", "-c", script)
    var stdout, stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr
    err := cmd.Run()
    status := "Success"
    if err != nil {
        status = "Failed"
    }
    return &Result{Status: status, Stdout: stdout.String(), Stderr: stderr.String()}, nil
}

func (e *LocalExecutor) InstanceID() string { return "localhost" }
```

**Migration:** Command handlers currently call `awsutil.RunCommand(ctx, clients.SSM, instanceID, script, timeout)`. After this refactoring, they call `cliCtx.Executor.RunScript(ctx, script, timeout)`. The executor is set during initialization:
- AWS mode (default): `cliCtx.Executor = executor.NewSSMExecutor(clients.SSM, instanceID)`
- Local mode (future `--local` flag): `cliCtx.Executor = &executor.LocalExecutor{}`

This means commands like `status`, `logs`, `refresh`, `connect`, `admin add-user`, `admin remove-agent` all become backend-agnostic — they build a script string and call `RunScript`. The only commands that would need separate logic per backend are:
- `admin cycle-host` — EC2 stop/start vs. Docker restart locally
- `connect` — SSM tunnel vs. direct localhost port

### 6b. AWS Service Interfaces (Low-Level SDK Abstraction)

Below the executor, we still need interfaces for direct AWS SDK calls used by parameter store, secrets manager, EC2, and STS operations.

**New file:** `cli/internal/aws/interfaces.go`

```go
package aws

import (
    "context"

    "github.com/aws/aws-sdk-go-v2/service/ec2"
    "github.com/aws/aws-sdk-go-v2/service/secretsmanager"
    "github.com/aws/aws-sdk-go-v2/service/ssm"
    "github.com/aws/aws-sdk-go-v2/service/sts"
)

// SSMClient abstracts the SSM API calls used by the CLI.
// In AWS mode, satisfied by *ssm.Client.
// In local mode, parameter store operations could be backed by a local config file.
type SSMClient interface {
    SendCommand(ctx context.Context, params *ssm.SendCommandInput, optFns ...func(*ssm.Options)) (*ssm.SendCommandOutput, error)
    GetCommandInvocation(ctx context.Context, params *ssm.GetCommandInvocationInput, optFns ...func(*ssm.Options)) (*ssm.GetCommandInvocationOutput, error)
    GetParameter(ctx context.Context, params *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error)
    PutParameter(ctx context.Context, params *ssm.PutParameterInput, optFns ...func(*ssm.Options)) (*ssm.PutParameterOutput, error)
    DeleteParameter(ctx context.Context, params *ssm.DeleteParameterInput, optFns ...func(*ssm.Options)) (*ssm.DeleteParameterOutput, error)
    GetParametersByPath(ctx context.Context, params *ssm.GetParametersByPathInput, optFns ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error)
}

// SecretsManagerClient abstracts Secrets Manager API calls.
// In local mode, secrets could be backed by a local encrypted file or keychain.
type SecretsManagerClient interface {
    ListSecrets(ctx context.Context, params *secretsmanager.ListSecretsInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.ListSecretsOutput, error)
    CreateSecret(ctx context.Context, params *secretsmanager.CreateSecretInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.CreateSecretOutput, error)
    PutSecretValue(ctx context.Context, params *secretsmanager.PutSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.PutSecretValueOutput, error)
    GetSecretValue(ctx context.Context, params *secretsmanager.GetSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
    DeleteSecret(ctx context.Context, params *secretsmanager.DeleteSecretInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.DeleteSecretOutput, error)
}

// EC2Client abstracts EC2 API calls. Not needed in local mode.
type EC2Client interface {
    DescribeInstances(ctx context.Context, params *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
    StopInstances(ctx context.Context, params *ec2.StopInstancesInput, optFns ...func(*ec2.Options)) (*ec2.StopInstancesOutput, error)
    StartInstances(ctx context.Context, params *ec2.StartInstancesInput, optFns ...func(*ec2.Options)) (*ec2.StartInstancesOutput, error)
}

// STSClient abstracts STS API calls. Not needed in local mode.
type STSClient interface {
    GetCallerIdentity(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)
}
```

### 6c. Update `Clients` Struct

**File:** `cli/internal/aws/session.go`

```go
type Clients struct {
    SSM            SSMClient
    SecretsManager SecretsManagerClient
    EC2            EC2Client
    STS            STSClient
}
```

The concrete SDK clients already satisfy these interfaces, so `NewClients` requires no changes beyond the struct field types.

### 6d. Update Function Signatures

All functions in `internal/aws/` that currently take `*ssm.Client`, `*secretsmanager.Client`, etc. are updated to take the corresponding interface type. Example:

```go
// Before
func RunCommand(ctx context.Context, client *ssm.Client, ...) (*RunCommandResult, error)

// After
func RunCommand(ctx context.Context, client SSMClient, ...) (*RunCommandResult, error)
```

Similarly for `internal/discovery/` functions that take SDK clients.

### 6e. `CLIContext` Struct

**New file:** `cli/cmd/context.go`

```go
package cmd

import (
    "time"

    awsutil "github.com/cruxdigital-llc/openclaw-template/cli/internal/aws"
    "github.com/cruxdigital-llc/openclaw-template/cli/internal/executor"
)

type CLIContext struct {
    Clients  *awsutil.Clients
    Executor executor.HostExecutor
    Profile  string
    Region   string
    Agent    string
    Verbose  bool
    Timeout  time.Duration
}

var cliCtx CLIContext
```

Migrate global variables into this struct. Flag bindings update `cliCtx` fields. `ensureClients` populates `cliCtx.Clients` and creates `cliCtx.Executor` as an `SSMExecutor`. Command handlers reference `cliCtx` instead of individual globals.

**Migration pattern for command handlers:**

```go
// Before (tightly coupled to SSM + instanceID)
instanceID, err := findInstance(ctx)
if err != nil { return err }
result, err := awsutil.RunCommand(ctx, clients.SSM, instanceID, script, 30*time.Second)

// After (backend-agnostic)
result, err := cliCtx.Executor.RunScript(ctx, script, 30*time.Second)
```

The `findInstance` call moves into `ensureClients` (or a new `ensureExecutor`), where it runs once and the SSMExecutor is constructed with the resolved instance ID. Commands no longer need to know about instance IDs at all.

### 6e. UI Reader/Writer Injection

**File:** `cli/internal/ui/prompt.go`

```go
func ConfirmWith(r io.Reader, w io.Writer, prompt string) bool {
    fmt.Fprintf(w, "%s [y/N]: ", prompt)
    scanner := bufio.NewScanner(r)
    if scanner.Scan() {
        answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
        return answer == "y" || answer == "yes"
    }
    return false
}

// Convenience wrapper for existing call sites
func Confirm(prompt string) bool {
    return ConfirmWith(os.Stdin, os.Stdout, prompt)
}
```

Same pattern for `TextPrompt`, `TextPromptWithDefault`, `SecretPrompt`.

---

## 7. Test Plan

### 7a. Pure Function Tests

**File:** `cli/cmd/status_test.go`

```go
func TestParseKeyValues(t *testing.T) {
    tests := []struct {
        name   string
        input  string
        expect map[string]string
    }{
        {"basic", "KEY=value\nFOO=bar", map[string]string{"KEY": "value", "FOO": "bar"}},
        {"empty value", "KEY=", map[string]string{"KEY": ""}},
        {"equals in value", "KEY=a=b", map[string]string{"KEY": "a=b"}},
        {"empty input", "", map[string]string{}},
        {"trailing newline", "KEY=val\n", map[string]string{"KEY": "val"}},
    }
    // ...
}

func TestSplitStats(t *testing.T) { /* 3-part split, fewer parts, empty */ }

func TestFormatUptime(t *testing.T) { /* seconds, minutes, hours, days, invalid input */ }
```

**File:** `cli/cmd/secrets_test.go`

```go
func TestSecretNameToEnvVar(t *testing.T) {
    tests := []struct{ input, expect string }{
        {"anthropic-api-key", "ANTHROPIC_API_KEY"},
        {"google-client-id", "GOOGLE_CLIENT_ID"},
        {"simple", "SIMPLE"},
        {"multi--dash", "MULTI__DASH"},
    }
    // ...
}
```

**File:** `cli/cmd/root_test.go`

```go
func TestValidateAgentName(t *testing.T) {
    // valid: "myagent", "ml-team", "a", "a-b-c-1"
    // invalid: "Aaron", "ml_team", "a b", "", "a!b"
}

func TestValidateMemberID(t *testing.T) {
    // valid: "U0123456789"
    // invalid: "u0123456789", "U012345678", "U01234567890", "C0123456789", ""
}

func TestValidateChannelID(t *testing.T) {
    // valid: "C0123456789"
    // invalid: "c0123456789", "C012345678", "U0123456789", ""
}
```

**File:** `cli/internal/discovery/identity_test.go`

```go
func TestARNParsing(t *testing.T) {
    // "arn:aws:sts::123456789012:assumed-role/RoleName/user@example.com" → "user@example.com"
    // "arn:aws:iam::123456789012:user/admin" → "admin"
    // "arn:aws:sts::123456789012:assumed-role/RoleName" → "" (only 2 parts)
}
```

### 7b. Mocked AWS Tests

**File:** `cli/internal/aws/ssm_test.go`

Test `RunCommand` with a mock `SSMClient`:
- **Happy path:** `SendCommand` succeeds, `GetCommandInvocation` returns `Success` on second poll
- **Command failure:** `GetCommandInvocation` returns `Failed` — verify `RunCommandResult.Status == "Failed"`
- **Timeout:** `GetCommandInvocation` always returns `InProgress` — verify timeout error after deadline
- **Consecutive errors:** `GetCommandInvocation` returns errors 5 times then succeeds — verify recovery
- **Consecutive errors exceeded:** 6+ consecutive errors — verify error returned

**File:** `cli/internal/aws/secrets_test.go`

Test `SetSecret`:
- **Update existing:** `PutSecretValue` succeeds — verify no `CreateSecret` call
- **Create new:** `PutSecretValue` returns `ResourceNotFoundException` — verify `CreateSecret` called
- **Create fails:** Both calls fail — verify error returned

Test `ListSecrets`:
- **Single page:** One call returns results with `NextToken == nil`
- **Multi-page:** Two calls, first returns `NextToken`, second returns `nil`
- **Empty:** Returns empty list

**File:** `cli/internal/discovery/agent_test.go`

Test `ListAgents`:
- **Parses valid JSON configs correctly**
- **Skips malformed entries** (with log warning if verbose)
- **Skips `/by-iam/` sub-paths**

### 7c. UI Tests

**File:** `cli/internal/ui/prompt_test.go`

```go
func TestConfirm(t *testing.T) {
    tests := []struct {
        input  string
        expect bool
    }{
        {"y\n", true},
        {"yes\n", true},
        {"Y\n", true},
        {"YES\n", true},
        {"n\n", false},
        {"no\n", false},
        {"\n", false},     // empty = default no
    }
    for _, tt := range tests {
        result := ConfirmWith(strings.NewReader(tt.input), io.Discard, "test?")
        assert(result == tt.expect)
    }
}

func TestTextPromptWithDefault(t *testing.T) {
    // empty input returns default
    // non-empty input overrides default
    // whitespace-only input returns default
}
```

---

## 8. Code Organization

### 8a. `admin.go` Split

| New File | Contents | Approx Lines |
|----------|----------|------------|
| `admin.go` | `adminCmd` definition, flag vars, `init()` with `AddCommand` calls | ~80 |
| `admin_setup.go` | `adminSetupRun` | ~105 |
| `admin_provision.go` | `adminAddUserRun`, `adminAddTeamRun`, `resolveGatewayPort`, `validateAgentName` | ~200 |
| `admin_remove.go` | `adminRemoveAgentRun` | ~80 |
| `admin_cycle.go` | `adminCycleHostRun` | ~50 |

All files remain in `package cmd`. No functional changes — purely organizational.

---

## 9. CI Integration

Add to `.github/workflows/` (new `ci.yml` or extend existing):

```yaml
name: CI
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: cli/go.mod
      - run: cd cli && go test ./... -v -race
      - run: cd cli && go test ./... -coverprofile=coverage.out
      - run: cd cli && go tool cover -func=coverage.out
```

---

## No Changes Needed

| File/Area | Reason |
|-----------|--------|
| `terraform/` | No infrastructure changes |
| `router/` | Not in scope |
| `scripts/*.sh.tmpl` | Embedded templates unchanged |
| `.goreleaser.yaml` | Build config unchanged |
| `cli/internal/tunnel/` | Tunnel management unchanged (only `connect.go` caller changes) |
| `cli/cmd/auth.go` | Auth commands unchanged |
| `cli/cmd/logs.go` | Logs command unchanged (benefits from timeout via context) |
| `cli/cmd/refresh.go` | Refresh command unchanged (benefits from timeout via context) |
| `cli/cmd/version.go` | Version command unchanged |

---

## Edge Cases

| Scenario | Handling |
|----------|----------|
| Existing agents with old-format Slack IDs (shorter than 11 chars) | Validation only applies to new `add-user`/`add-team` commands. Existing agents in SSM are not re-validated. |
| `json.Marshal` of `map[string]interface{}` | Cannot fail for the types used (string, int) — but checking the error is correct practice and prevents future regressions if the map contents change. |
| `parseKeyValues` with no `=` in line | Already handled — `IndexByte` returns -1, line is skipped. Test confirms. |
| `formatUptime` with unparseable timestamp | Returns raw timestamp as fallback. |
| `--timeout 0` | Context with zero timeout fires immediately. Validate minimum (e.g., 10s). |

---

## Implementation Sequence

```
Phase 1 (bug fixes)     → Phase 2 (validation/UX) → Phase 3 (refactoring)
                                                        ↓
Phase 5 (admin.go split) ← Phase 4 (tests)          ← Phase 3
                                                        ↓
                           Phase 6 (status uptime)    CI integration
```

All phases are independently mergeable. Phase 3 (refactoring) should be a single commit to avoid half-migrated state.
