# Plan: Egress Proxy Image Cleanup

## Approach

Single-phase cleanup: update the image constant, remove all build logic, replace `conga-egress-proxy` references with the pinned upstream image, delete the Dockerfile.

## Phases

### Phase 1: Update Image Constant & Remove Build Logic

1. **`cli/pkg/policy/egress.go`** — Change `EgressProxyImage` from `"conga-egress-proxy"` to `"envoyproxy/envoy:v1.32.3"`. Remove `EgressProxyBaseImage` constant and `EgressProxyDockerfile()` function.

2. **`cli/pkg/provider/localprovider/provider.go`** — Remove `buildEgressProxyImage()` function (lines 1284-1295). Remove the build-if-missing check in `startAgentEgressProxy()` (lines 1215-1221). Remove `egressProxyImage` local constant and `egressProxyDir()` helper if only used for builds. Add `docker pull` before `docker run`.

3. **`cli/pkg/provider/remoteprovider/provider.go`** — Remove build-if-missing block in `startAgentEgressProxy()` (lines 964-974). Remove `egressProxyImage` local constant and `remoteEgressProxyDir()` helper if only used for builds. Add `docker pull` via SSH before `docker run`.

4. **AWS scripts** — Update `cli/scripts/deploy-egress.sh.tmpl`, `cli/scripts/add-user.sh.tmpl`, `cli/scripts/add-team.sh.tmpl`: remove build-if-missing blocks, replace `conga-egress-proxy` with the pinned image name. Update `terraform/modules/infrastructure/user-data.sh.tftpl` similarly.

5. **Delete** `deploy/egress-proxy/Dockerfile`.

6. **Tests** — Update `cli/scripts/scripts_test.go` assertions that reference `conga-egress-proxy`.

## Risk Assessment

- **Low risk**: This is a simplification — no behavioral change, same Envoy binary, same configs, same security controls.
- **Image availability**: `docker pull` requires network access on first deploy. This is already true (the build step pulls the base image anyway).
- **Pinned version**: Must verify `v1.32.3` exists. If not, use the latest stable `v1.32.x`.
