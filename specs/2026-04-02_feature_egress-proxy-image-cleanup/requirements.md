# Requirements: Egress Proxy Image Cleanup

## Problem Statement

The egress proxy Dockerfile (`deploy/egress-proxy/Dockerfile`) is a single line: `FROM envoyproxy/envoy:v1.32-latest`. The `docker build` step across all three providers (local, remote, AWS) is just re-tagging the upstream image as `conga-egress-proxy` — no custom code is baked in. All real configuration (Envoy YAML, Lua filters, entrypoint script, proxy-bootstrap.js) is generated dynamically by the CLI and volume-mounted at runtime.

This adds unnecessary build machinery, uses a floating tag (`v1.32-latest`), and complicates the codebase without adding value.

## Requirements

1. **Pin to a specific Envoy version** — Replace `envoyproxy/envoy:v1.32-latest` (floating tag) with a fixed version tag (e.g., `envoyproxy/envoy:v1.32.3`).
2. **Use upstream image directly** — All `docker run` commands should reference the upstream Envoy image instead of the locally-built `conga-egress-proxy` tag.
3. **Remove build machinery** — Delete `buildEgressProxyImage()` from local and remote providers, the Dockerfile at `deploy/egress-proxy/Dockerfile`, and all build-if-missing logic.
4. **Update AWS bootstrap** — Remove the build step from `user-data.sh.tftpl` and AWS SSM scripts (`deploy-egress.sh.tmpl`, `add-user.sh.tmpl`, `add-team.sh.tmpl`).
5. **Ensure image pull** — Use `docker pull` explicitly before `docker run` (or rely on Docker's auto-pull) to ensure the image is available on first use.
6. **Security parity** — All existing container security controls (cap-drop ALL, read-only, user 101:101, memory limit, etc.) must be preserved unchanged.

## Non-Goals

- No changes to Envoy configuration generation, Lua filters, or proxy-bootstrap.js.
- No changes to iptables enforcement or network isolation.
- No separate repo or registry for a custom image.
- No changes to the proxy's runtime behavior.
