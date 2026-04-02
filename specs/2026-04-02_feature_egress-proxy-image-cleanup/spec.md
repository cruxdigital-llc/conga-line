# Spec: Egress Proxy Image Cleanup

## Summary

Eliminate the trivial `docker build` step that re-tags `envoyproxy/envoy:v1.32-latest` as `conga-egress-proxy`. Use the upstream Envoy image directly with a pinned version tag. Remove all build machinery across the three providers.

## Pinned Image Version

**Image**: `envoyproxy/envoy:v1.32.6` (latest stable v1.32.x as of 2026-04-02)

Rationale: `v1.32-latest` is a floating tag that can change without notice. Pinning to a specific patch version ensures reproducible deployments and avoids surprise breakage from upstream changes.

## Changes by File

### 1. `cli/pkg/policy/egress.go`

**Before:**
```go
const EgressProxyImage = "conga-egress-proxy"
const EgressProxyBaseImage = "envoyproxy/envoy:v1.32-latest"
func EgressProxyDockerfile() string { ... }
```

**After:**
```go
const EgressProxyImage = "envoyproxy/envoy:v1.32.6"
```

Remove:
- `EgressProxyBaseImage` constant
- `EgressProxyDockerfile()` function

Update comment on `EgressProxyImage` to reflect it's now the upstream image directly.

### 2. `cli/pkg/policy/egress_test.go`

Remove `TestEgressProxyDockerfile` test (line 263+). The function no longer exists.

### 3. `cli/pkg/provider/localprovider/provider.go`

**Remove:**
- `egressProxyImage` constant (line 25) — unused after change; `policy.EgressProxyImage` is used everywhere
- `egressProxyDir()` helper (line 57) — only used for Dockerfile copy/build
- `buildEgressProxyImage()` function (lines 1284-1295)
- Build-if-missing check in `startAgentEgressProxy()` (lines 1215-1221)
- Dockerfile copy block in `Setup()` (lines 949-955) — `deploy/egress-proxy/` directory is being deleted
- Egress proxy build block in `Setup()` (lines 977-988)
- `egressProxyDir()` from directory creation list in `Setup()` (line 805)

**Add to `Setup()`** (near the existing image pull section, ~line 958-968):
```go
// Pull egress proxy image
fmt.Println("Pulling egress proxy image...")
spin = ui.NewSpinner("Pulling egress proxy image...")
err = pullImage(ctx, policy.EgressProxyImage)
spin.Stop()
if err != nil {
    fmt.Fprintf(os.Stderr, "Warning: failed to pull egress proxy image: %v\nYou can pull it manually: docker pull %s\n", err, policy.EgressProxyImage)
} else {
    fmt.Println("  Egress proxy image pulled.")
}
```

**In `startAgentEgressProxy()`** — replace the build-if-missing block (lines 1215-1221) with a pull-if-missing:
```go
// Pull proxy image if not present locally.
if !imageExists(ctx, policy.EgressProxyImage) {
    fmt.Printf("  Pulling egress proxy image...\n")
    if err := pullImage(ctx, policy.EgressProxyImage); err != nil {
        return fmt.Errorf("pulling egress proxy image: %w", err)
    }
}
```

### 4. `cli/pkg/provider/remoteprovider/provider.go`

**Remove:**
- `egressProxyImage` constant (line 22)
- Build-if-missing block in `startAgentEgressProxy()` (lines 964-974)

**Add to `startAgentEgressProxy()`** — pull-if-missing via SSH:
```go
// Pull proxy image if not present on remote.
exists, _ := p.ssh.Run(ctx, fmt.Sprintf("docker image inspect %s >/dev/null 2>&1 && echo yes || echo no", policy.EgressProxyImage))
if strings.TrimSpace(exists) != "yes" {
    fmt.Printf("  Pulling egress proxy image on remote...\n")
    if _, err := p.ssh.Run(ctx, fmt.Sprintf("docker pull %s", policy.EgressProxyImage)); err != nil {
        return fmt.Errorf("pulling egress proxy image: %w", err)
    }
}
```

### 5. `cli/pkg/provider/remoteprovider/setup.go`

**Remove:**
- `remoteEgressProxyDir()` helper (in `provider.go` line 85)
- Egress proxy upload block in `Setup()` (lines 284-292) — `deploy/egress-proxy/` directory is being deleted
- Egress proxy build block in `Setup()` (lines 313-326)

**Add to `Setup()`** (near the existing image pull section):
```go
// Pull egress proxy image on remote
fmt.Println("Pulling egress proxy image on remote host...")
spin = ui.NewSpinner("Pulling egress proxy image...")
err = p.pullImage(ctx, policy.EgressProxyImage)
spin.Stop()
if err != nil {
    fmt.Fprintf(os.Stderr, "Warning: failed to pull egress proxy image: %v\n", err)
} else {
    fmt.Println("  Egress proxy image pulled.")
}
```

### 6. `cli/scripts/deploy-egress.sh.tmpl`

**Remove** build-if-missing block (lines 42-52):
```bash
# REMOVE:
if ! docker image inspect conga-egress-proxy >/dev/null 2>&1; then
  ...build...
fi
```

**Replace** `conga-egress-proxy` in `docker run` (line 87) with the image name injected via template variable. Add a template variable `{{.EgressProxyImage}}` and use it in place of the hardcoded image name.

**Add** a pull-if-missing before `docker run`:
```bash
docker pull "{{.EgressProxyImage}}" 2>/dev/null || true
```

### 7. `cli/scripts/add-user.sh.tmpl` and `cli/scripts/add-team.sh.tmpl`

Same changes as `deploy-egress.sh.tmpl`:
- Remove build-if-missing blocks
- Replace `conga-egress-proxy` with `{{.EgressProxyImage}}` template variable
- Add `docker pull` before `docker run`

### 8. `terraform/modules/infrastructure/user-data.sh.tftpl`

**Remove** build-if-missing block (lines 773-789):
```bash
# REMOVE:
if ! docker image inspect conga-egress-proxy >/dev/null 2>&1; then
  ...build...
fi
```

**Replace** `conga-egress-proxy` in `docker run` (line 803) with a Terraform variable or the pinned image string directly.

**Add** `docker pull` in the setup section (before first agent loop):
```bash
# Pull egress proxy image
log "Pulling egress proxy image..."
docker pull envoyproxy/envoy:v1.32.6
```

### 9. `cli/scripts/scripts_test.go`

Update assertions on lines 164 and 206 that match `conga-egress-proxy` to match the new upstream image name.

### 10. `deploy/egress-proxy/Dockerfile`

**Delete this file.** Also delete the `deploy/egress-proxy/` directory if it contains no other files.

## Edge Cases

| Scenario | Handling |
|---|---|
| First deploy, no internet | `docker pull` fails — same as today (build also pulls the base image). Error message guides user to pull manually. |
| Image already cached locally | `imageExists` / `docker image inspect` check skips pull. No behavioral change. |
| Upgrading from existing deployment | Old `conga-egress-proxy` image remains cached but unused. Next `startAgentEgressProxy` pulls the new pinned image. No migration needed. |
| AWS EC2 first boot | `docker pull` in user-data.sh replaces `docker build`. Same network requirement. Failure handling preserved (enforce mode = fatal, validate mode = warn). |

## Security Parity Checklist

All existing container security controls are **preserved unchanged**:

- [x] `--cap-drop ALL`
- [x] `--security-opt no-new-privileges`
- [x] `--memory 128m`
- [x] `--read-only`
- [x] `--user 101:101`
- [x] `--tmpfs /tmp:rw,noexec,nosuid`
- [x] `--entrypoint ''` (override default)
- [x] Volume-mounted Envoy config (`:ro`)
- [x] Per-agent Docker network isolation
- [x] iptables DROP rules unchanged

## Verification

1. **Unit tests**: `go test ./cli/...` — all packages pass
2. **Local provider**: `conga admin setup --provider local` → `conga admin add-user` → verify `docker ps` shows container running `envoyproxy/envoy:v1.32.6` (not `conga-egress-proxy`) → `conga get-proxy-logs` works → egress filtering works
3. **Remote provider**: Same lifecycle on remote host
4. **AWS provider**: `conga policy deploy` → verify SSM script uses correct image → proxy container running
5. **Image not cached**: Remove all envoy images (`docker rmi envoyproxy/envoy:v1.32.6`), run setup, verify auto-pull works
6. **Security**: Verify `docker inspect` on proxy container shows same security settings (cap-drop, user, memory, read-only)
