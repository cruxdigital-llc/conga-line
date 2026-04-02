# Bugfix: SSH Auto-Reconnect for MCP Server

**Status**: CLOSED
**Reported**: 2026-03-24
**Severity**: High (blocks all remote operations until manual restart)

## Symptom

When the MCP server's SSH connection to the remote host goes stale (network blip, NAT timeout, server reboot), every subsequent MCP tool call fails with:

```
SSH connection failed: ssh session failed: read tcp <local>->72.60.70.39:22: read: operation timed out
```

The only recovery is restarting Claude Code, which restarts the MCP server subprocess and forces a fresh SSH connection.

## Reproduction

1. Start Claude Code with the remote provider configured
2. Run any MCP tool (e.g. `conga_get_status`) — works fine
3. Wait for the SSH connection to go stale (or simulate by killing the connection on the server side)
4. Run any MCP tool again — fails with "ssh session failed"
5. All subsequent tool calls continue to fail

## Root Cause Analysis (5 Whys)

1. **Why did the tool call fail?** — `client.NewSession()` returned an error because the underlying TCP connection was dead.
2. **Why was the TCP connection dead?** — A network disruption (NAT timeout, route change, server reboot) killed the connection, but the local side didn't receive a TCP RST/FIN.
3. **Why didn't the keepalive detect it?** — The keepalive sends requests every 30s, but a half-open TCP connection can hang on the read for the full OS TCP timeout (often 15+ minutes on macOS). By the time it fails, the user has already hit the error.
4. **Why didn't it recover after the keepalive detected the failure?** — The keepalive goroutine just `return`s on error. There is no reconnection logic — the dead `*ssh.Client` stays on the `SSHClient` struct forever.
5. **Why is there no reconnection logic?** — `SSHClient` doesn't store the `keyPath` parameter needed to rebuild the connection, and `SSHConnect` is a standalone constructor with no retry-on-use path. The single-connection-at-init design assumed the connection would remain stable for the process lifetime.

## Fix Strategy

See [plan.md](plan.md).

## Implementation

**Status**: Complete — all tests pass.

### Code changes

**`cli/pkg/provider/remoteprovider/ssh.go`**:
- Added `config *ssh.ClientConfig` field to `SSHClient` struct (stored for reconnection)
- `SSHConnect` now saves the `config` on the returned struct
- Added `reconnect()` — closes dead client, re-dials with stored config, starts new keepalive
- Added `session()` — tries `NewSession()`, reconnects once on failure, retries
- Added `sftpClient()` — same pattern for SFTP handshakes
- Replaced 5 direct `c.client.NewSession()` / `sftp.NewClient(c.client)` calls with wrappers
- `ForwardPort` left unchanged (intentional — tunnel lifecycle is different)

**`cli/pkg/provider/remoteprovider/integrity.go`**:
- Replaced `p.ssh.client.NewSession()` (line 75) with `p.ssh.session()`

**`cli/pkg/provider/remoteprovider/ssh_reconnect_test.go`** (new file):
- `testSSHServer` / `testSSHServerOnPort` — in-process SSH server helpers with connection tracking
- `TestSessionReconnectsOnStaleConnection` — kill server, restart, verify transparent recovery
- `TestSessionFailsWhenServerTrulyDown` — verify fail-fast with clear error, no infinite retries
- `TestReconnectPreservesParameters` — host/port/user/config unchanged after reconnect
- `TestRunSucceedsWithoutReconnect` — happy path regression guard, verifies no spurious reconnects

### Design deviation from plan

Plan proposed storing `keyPath` and calling `SSHConnect` in `reconnect()`. Implementation stores `*ssh.ClientConfig` directly and re-dials with it. This is better because:
1. Avoids re-resolving auth methods (key files, ssh-agent) on every reconnect
2. Makes the reconnect path testable without real SSH keys on disk
3. Keeps the reconnect fast — just a TCP dial + SSH handshake

### Test results

```
PASS: TestSessionReconnectsOnStaleConnection (0.03s)
PASS: TestSessionFailsWhenServerTrulyDown (0.00s)
PASS: TestReconnectPreservesParameters (0.00s)
PASS: TestRunSucceedsWithoutReconnect (0.00s)
```

Full suite: all packages pass, zero regressions.

## Verification

- Full test suite: all 8 packages pass, zero failures
- `go vet`: clean
- Code review: no new technical debt, no security surface changes
- Standards audit: no stale references in `product-knowledge/standards/`
- Test sync: no stale imports, fakes aligned with real behavior, all new public methods covered (except `sftpClient()` which is structurally identical to `session()`)
- Plan updated to reflect `*ssh.ClientConfig` deviation from original `keyPath` approach
- PROJECT_STATUS.md updated
