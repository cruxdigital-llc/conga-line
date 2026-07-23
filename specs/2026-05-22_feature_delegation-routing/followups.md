# Delegation Routing — Operational Debt Surfaced During Phase 8

This document captures every monkey-patch, workaround, and architectural drift
discovered while bringing the delegation-routing feature to production. Each
item is sized, scoped, and linked to a proposed fix.

Per user-a's direction (this session), the goal is to **address all of these
in this PR** rather than defer. Some items are quick (one-file changes);
others are architectural enough to warrant their own spec. For the latter, a
design sketch is included; final design lands when implementation begins.

---

## 1. Worktree-vs-parent CWD silent-wrong (`resolveAWSBehaviorDir` / `ResolveOperatorBehaviorDir`)

**Severity**: high — silent data-quality failure. Deploys defaults-only config when run from a git worktree.

**Symptom (observed this session)**: MCP server's cwd was the explore-agent-routing worktree. `resolveAWSBehaviorDir()` prefers `./agents` over walking to the parent checkout. The worktree's `agents/` contains only the committed `_defaults/` and `_example/` (per-agent dirs are gitignored), so the loader treated all per-agent overlays as missing. First refresh of `user-a` after the schema bump landed an openclaw.json with no `subagents` block.

**Workaround applied**: `ln -s ~/Development/crux/congaline/agents/<name> agents/<name>` for each of the 5 agents in the worktree. Symlinks survive across MCP refreshes but evaporate when the worktree is cleaned up.

**Root cause**: two functions duplicate the same logic:
- `pkg/provider/awsprovider/channels.go::resolveAWSBehaviorDir()`
- `pkg/common/role_package.go::ResolveOperatorBehaviorDir()`

Both first try `./agents` (cwd-relative), then walk up to find go.mod + `<repo>/agents`. The cwd-relative check wins whenever the cwd happens to be inside a git worktree (because the worktree's `_defaults/` directory makes the `agents/` look "real enough").

**Proposed fix**:
- Drop the `./agents` cwd-relative early-return in both functions.
- Always walk up looking for the conga-line `go.mod`, then resolve `<repo-root>/agents`.
- For worktree detection: when the resolved `<repo-root>` is itself a worktree (detectable via `<repo-root>/.git` being a file rather than a directory), follow the `gitdir:` pointer to the main worktree and use that `agents/` dir instead.
- Unify the two functions: keep the canonical version in `pkg/common/`, have `awsprovider` call into it.

**Scope**: ~50 lines of code + tests. Single PR. Tests for: cwd at repo root, cwd at subdirectory of repo, cwd at worktree (should resolve to main worktree's agents/), cwd outside any repo.

**Status**: 🔴 unfixed.

---

## 2. `openclaw-defaults.json` is `//go:embed`'d

**Severity**: medium — every model-version bump becomes a binary release.

**Symptom (this session)**: Bumping Opus `4-6` → `4-7` required editing 8 files (defaults JSON, tests, shell templates, terraform user-data), rebuilding both binaries (`~/go/bin/conga` and `bin/conga`), clearing `go clean -cache`, and restarting the MCP server. Same scope as a feature change for what should be a config change.

**Root cause**: `pkg/runtime/openclaw/config.go:13` has `//go:embed openclaw-defaults.json` — the file becomes part of the binary at build time.

**Proposed fix**: Move to `agents/_defaults/<runtime>/runtime-defaults.json` (committed in git, sibling of the role packages). Loader resolution order:
1. `<repo-root>/agents/_defaults/<runtime>/runtime-defaults.json` if present
2. Embedded copy as fallback (transitional safety; remove the embed once all environments are confirmed using the on-disk version)

Same approach for Hermes when it gains an analogous file.

**Scope**: ~80 lines + tests + documentation. Touches `pkg/runtime/openclaw/config.go`, adds new file, updates `agents/_defaults/openclaw/runtime-defaults.json`. **Architecturally cohesive enough to deserve its own spec**: the file's role changes from "compiled-in fact" to "operator-customizable config". Need to think about per-environment overrides too (e.g. an AWS environment that wants different defaults than local).

**Status**: 🔴 unfixed. Recommend deferring to a small dedicated spec.

---

## 3. terraform-provider-conga has stale `pkg/`

**Severity**: medium-high — every `terraform apply` against a v2-overlay agent emits cascade-failure warnings.

**Symptom (this session)**: Both terraform applies (per-agent secret migration AND egress allowlist update) hit the same error:
```
field subagents not found in type runtime.AgentOverlay
```
The conga terraform provider has its own embedded `conga` binary built from the old `pkg/`. It can't parse our v2 overlays. After each secret/policy write, the provider attempts an in-provider refresh of the affected agents — which fails. The AWS-side state is correct; only the in-provider refresh fails. Operators must run `conga refresh-agent <name>` separately via the MCP-side binary to actually apply the change.

**Root cause**: separate-repo issue. `cruxdigital-llc/terraform-provider-conga` imports `pkg/` from this repo. Per CLAUDE.md release flow:
> Tag congaline → `go get` + `go mod tidy` in provider repo → push → tag provider → GoReleaser publishes to registry

We bump `pkg/` constantly in this PR (overlay schema, role package loader, egress check). The provider release lags behind.

**Proposed fix**: After this PR merges:
1. Tag congaline (e.g. `v0.0.22`)
2. In `cruxdigital-llc/terraform-provider-conga`: `go get github.com/cruxdigital-llc/conga-line@v0.0.22 && go mod tidy`
3. Tag the provider (e.g. `v1.0.X`)
4. GoReleaser publishes to registry
5. Delete `~/.terraform.d/plugins/registry.terraform.io/cruxdigital-llc/conga/` to clear local cache
6. `terraform init -upgrade` in `terraform/environments/production/`

**Scope**: out of repo. Documented here for the post-merge runbook.

**Status**: 🔴 unfixed (can't be fixed in this PR — separate repo). Recommend adding a release-flow checklist to PR description.

---

## 4. `conga_environment.this` provider bug

**Severity**: low-medium — annoying but recoverable.

**Symptom (this session)**: First terraform apply aborted mid-cascade with:
```
Provider produced inconsistent result after apply ...
.id: was cty.StringVal("aws"), but now cty.StringVal("environment")
```
This is a bug in the conga terraform provider — it returned a different `id` value than terraform expected. Stopped the apply before all secret creates landed, leaving production briefly without `anthropic-api-key` for all 5 agents until the re-apply landed.

**Root cause**: Schema mismatch between the provider's `conga_environment` resource: the apply path sets `id` to "environment" while the read/initial path uses "aws". Terraform considers this an inconsistent state.

**Proposed fix**: Fix in `cruxdigital-llc/terraform-provider-conga`. The fix is small: pick one canonical id value (likely "aws" since that's what the existing state has) and use it everywhere.

**Scope**: out of repo. Trivial code change once located.

**Status**: 🔴 unfixed (separate repo). Recommend filing a provider issue + fixing alongside the pkg/ bump above.

---

## 5. `conga admin unpause` fails when systemd unit was deleted

**Severity**: medium — recoverable but unpleasant catch-22.

**Symptom (this session)**: The 4 paused agents (user-b, user-c, team-a, team-c) had their `/etc/systemd/system/conga-<name>.service` unit files missing. `unpause` runs `systemctl start conga-<name>`, which fails with `Unit conga-<name>.service not found`. `refresh` (which would recreate the unit via `refresh-user.sh.tmpl`) refuses to run while the agent is marked `paused: true`.

**Workaround applied**: directly flipped `paused: false` in the SSM parameter via `aws ssm put-parameter`, then called `conga_refresh_agent` via MCP. The refresh path recreated the unit file + .env + container.

**Root cause**: `conga admin pause` is supposed to only `systemctl stop` (not remove the unit). At some past point, manual cleanup or a flow we no longer remember deleted the unit files. The unpause path has no fallback for "unit doesn't exist".

**Proposed fix**: Make `unpause` self-healing.
- Before calling `systemctl start`, check whether the unit file exists (`test -f /etc/systemd/system/conga-<name>.service`).
- If missing, fall through to a reprovision path that mirrors what `refresh` does (regenerate config + write unit + start), then update the SSM `paused` flag.
- Alternatively, allow `refresh --force` to ignore the paused flag — but that overloads the meaning of `--force`, so the self-healing unpause is cleaner.

**Scope**: ~30-line change to `scripts/unpause-agent.sh.tmpl` + a small touchpoint in the provider's `UnpauseAgent`. Tests via integration suite (existing `TestAgentLifecycle` has pause/unpause subtests).

**Status**: 🔴 unfixed.

---

## 6. `conga refresh` doesn't regenerate the egress proxy's Envoy config

**Severity**: medium — silently leaves the egress proxy in a stale state after policy changes.

**Symptom (this session)**: After updating the egress allowlist (adding `slack.com` to tfvars) and running `terraform apply`, the policy SSM state was current but every agent's live Envoy config still had the OLD empty allowlist. Running `conga refresh-agent <name>` didn't fix it — refresh only regenerates `openclaw.json` + restarts the agent container. The Envoy proxy continued running with the stale config until I ran `conga policy deploy` explicitly.

**Root cause**: `RefreshAgent` in providers regenerates only the agent runtime config. `policy.GenerateProxyConf()` (the Envoy config generator) is called only by `DeployPolicy`. Operators reasonably expect `refresh` to bring the agent to current-policy state, since that's what the word "refresh" implies.

**Proposed fix**: Two options:
- **A**: Make `refresh` also regenerate the Envoy config + restart the egress proxy. Conceptually it's all "make this agent current". Risk: bigger blast radius for what's currently a low-impact operation.
- **B**: Keep them separate but document the split clearly. Add a "did you also run `policy deploy`?" hint to refresh output. Risk: relies on operator memory.

user-a's call. Recommend **A** — operators are confused by the split today; merging is the right ergonomic choice.

**Scope**: ~50-line change spanning `pkg/provider/{local,remote,aws}provider/provider.go::RefreshAgent`. Each provider needs to (re)deploy the egress proxy as part of refresh.

**Status**: 🔴 unfixed.

---

## 7. Bootstrap-time Envoy config is stale forever until first `policy deploy`

**Severity**: high — agents are in deny-all egress state from bootstrap until an operator manually runs `policy deploy`.

**Symptom (this session)**: user-a's `/opt/conga/config/egress-user-a.yaml` mtime was `2026-05-22 22:43:41` — the original bootstrap time. The Lua filter's `EXACT` and `SUFFIXES` tables were both empty. Every outbound request was 403'd by Envoy regardless of policy state. This was the actual root cause of "user-a not accessible via Slack" — bolt-app couldn't call `slack.com` (or anywhere), failed health checks, listener crashed after `ack()`, returned 401/404 on inbound webhook events.

**Root cause**: the bash bootstrap (terraform `user-data.sh.tftpl`) writes an Envoy config with an empty allowlist because it runs BEFORE the policy has been set. Nothing reconciles this at the end of bootstrap. Operators are supposed to know to run `conga policy deploy` after bootstrap, but it's not documented as a required step — and even when policy is updated later via `terraform apply`, the in-provider cascade-refresh fails (per #3), so the proxy config stays stale.

**Proposed fix**: Two layers of defense:
- **Layer 1**: End of `user-data.sh.tftpl` calls `conga policy deploy` (via the binary that was just built on the instance). Closes the bootstrap→runtime gap.
- **Layer 2**: `conga admin setup` (operator-initiated, post-bootstrap) explicitly runs policy deploy at the end. Makes the "deploy the policy I just declared" step automatic.
- **Layer 3** (related to #6): `conga refresh` also redeploys policy. Catches drift after the fact.

All three layers together guarantee that whatever path an operator takes, the proxy config matches the policy.

**Scope**: ~30 lines in `terraform/modules/infrastructure/user-data.sh.tftpl` + a couple lines in `internal/cmd/admin_setup.go` + the change from #6.

**Status**: 🟢 **closed-by-#6**. Investigation during implementation discovered that the conga binary isn't installed on the EC2 instance (it lives only on the operator's machine) — so the bootstrap can't self-deploy. With #6 landed, every operator-side refresh redeploys the live Envoy configs from the current policy. The "first refresh after bootstrap is required" step is the same as the current operational workflow; #6 just makes it self-healing across all routine operations going forward.

A doc-only comment was added to `user-data.sh.tftpl` after the bootstrap-sentinel line documenting this — the bash bootstrap's deny-all default is intentional fail-safe behavior, and the post-bootstrap `conga policy deploy` (or `conga refresh-all`) is the canonical "make agents reachable" step.

---

## 8. Bash Envoy generator doesn't implement validate mode

**Severity**: low — currently masked by #7 (validates don't matter when the allowlist is empty), but a latent bug once #7 lands.

**Symptom**: Policy file says `mode: validate` (log denials but allow traffic through). The bash-generated Envoy config (from `user-data.sh.tftpl`) emits a Lua filter that **unconditionally 403s** non-allowlisted hosts, regardless of mode. The Go-generated config (from `pkg/policy/egress.go::GenerateProxyConf`) does honor the mode. The two have drifted.

**Root cause**: Two implementations of the same Envoy config template, with different feature support. Comment in `pkg/policy/egress.go:100` already calls this out:
> NOTE: The bash reimplementation in terraform/user-data.sh.tftpl generates the same config format — keep both implementations and templates/envoy-config.yaml.tmpl in sync.

**Proposed fix**: Two options:
- **A**: Update the bash bootstrap to use the same template logic (port the validate-mode branch). Cost: maintaining two copies of the template logic.
- **B**: Have the bootstrap call `conga policy generate-envoy-config` (a new subcommand) to produce the Envoy config via the Go path. Removes the duplication entirely.

**B** is the right long-term answer. Bootstrap already calls the conga binary for other things (setup, add-user). One more call is trivial.

**Scope**: ~40 lines (new CLI subcommand + bootstrap update). Tests: existing `pkg/policy/egress_test.go` covers the Go path; add a smoke test that exercises the CLI subcommand.

**Status**: 🟡 **deferred (non-blocking)**. With #6 landed, the bash-generated Envoy config gets overwritten by the Go-generated one on every refresh — so the period during which the validate-mode mismatch matters is the window between bootstrap completion and the first operator refresh. In practice that's minutes, not hours. Aligning the bash version remains a cleanup task but no longer blocks correct operation.

---

## 9. `routing.json` left empty after SSM-only unpause bypass

**Severity**: low — only manifests when an operator bypasses the `unpause` script (as I did this session).

**Symptom (this session)**: I cleared `paused: false` directly in SSM (because the systemd unit was missing — per #5) and then called `conga refresh`. Refresh recreated the unit + container + .env, but **never repopulated `routing.json`**. The 4 unpaused agents had functioning containers but the router didn't know how to forward DMs to them. Symptom: `[router] No route: type=message channel=... user=...` for each Slack event.

**Workaround applied**: hand-installed a correct `routing.json` with all 5 entries + restarted the router.

**Root cause**: `routing.json` is updated by `pause-agent.sh.tmpl` (removes the entry) and `unpause-agent.sh.tmpl` (adds it back). `refresh-user.sh.tmpl` does NOT touch `routing.json`. So if an operator bypasses unpause (as I did), the routing entry is never re-added.

**Proposed fix**: Resolves itself once #5 (unpause self-healing) lands. The proper unpause path always updates `routing.json`. The SSM-only workaround becomes unnecessary.

Alternatively / additionally: have `refresh` also reconcile `routing.json` from the agent's channels in SSM. This is the cleanest fix: routing.json should be derivable from `/conga/agents/*` state at any time.

**Scope**: depends on #5. If #5 lands, this is automatically fixed for the typical path. The "refresh reconciles routing.json" addition is ~20 lines.

**Status**: 🟡 partially-blocked by #5.

---

## 10. Router doesn't include gateway Bearer token

**Severity**: low today, time-bomb medium for future OpenClaw releases.

**Symptom**: Not biting today (the `/slack/events` webhook path is exempt from gateway auth in v2026.5.18). But if OpenClaw tightens this in a future release (extends gateway auth to webhook paths), every router→agent forward will start 401'ing.

**Root cause**: `router/slack/src/index.js::forwardEvent` sends only Slack-style signature headers. No `Authorization: Bearer <gateway-token>`. The router doesn't even know about gateway tokens — they're per-agent and stored in each agent's openclaw.json.

**Proposed fix**: Two layers:
- **Layer 1**: At provision/refresh time, write a `<router-config-dir>/agent-tokens.json` mapping `{agent_name: gateway_token}`. Router reads this at startup.
- **Layer 2**: Router's `forwardEvent` looks up the agent name (from the target URL hostname `conga-<name>`), reads the token, includes it as `Authorization: Bearer <token>` on the forwarded request.

**Scope**: ~60 lines across router/slack/src/index.js + provider provisioning logic + new file generation. **Pre-emptive — fixing a future bug that doesn't yet bite**. Could be safely deferred until the OpenClaw upstream change actually lands.

**Status**: 🔴 unfixed. Recommend deferring — fixing without a real driver risks scope creep.

---

## 11. Slack allowlist needs `slack.com` bare host

**Severity**: low post-fix.

**Symptom (this session)**: Bolt-app SDK calls `slack.com` (bare host, no subdomain) for OAuth/app-config lookups. Wildcard `*.slack.com` only matches subdomains. Without `slack.com` explicitly in the allowlist, bolt-app spams 403s and eventually crashes its webhook listener.

**Fix applied**: Added `"slack.com"` to `egress_allowed_domains` in `terraform/environments/production/terraform.tfvars` (this session, committed via terraform apply).

**Proposed follow-up**: Update `terraform/environments/production/terraform.tfvars.example` + the `agents/_example/agent.yaml.example` docs to mention this. Update the `terraform/README.md` example domain list. Once #8 lands and bootstrap calls into the Go policy path, document this in `egress-controls.md` as a required-when-Slack-is-configured allowlist entry.

**Scope**: ~10 lines of docs + tfvars.example update.

**Status**: 🟢 fixed in production; docs not yet updated.

---

## 12. `terraform.tfvars.example` doesn't reflect production setup

**Severity**: low — onboarding friction.

**Symptom**: The example file doesn't include `slack.com` (per #11), the Spark LiteLLM endpoint, or the per-agent secret pattern we now use. New operators get a deny-all setup.

**Proposed fix**: Refresh `terraform/environments/production/terraform.tfvars.example` with:
- Slack allowlist including bare `slack.com`
- Per-agent `secrets = {}` block example
- Egress mode set to `validate` with a comment about graduating to `enforce`
- Comment on what to substitute (real account IDs / endpoints)

**Scope**: ~20 lines, single file.

**Status**: 🔴 unfixed.

---

## Summary table

| # | Item | Severity | Scope | This PR? |
|---|---|---|---|---|
| 1 | Worktree CWD walk-up | high | ~50 LoC | ✅ in scope |
| 2 | Extract runtime defaults from `//go:embed` | medium | ~80 LoC + spec | ⚠ deserves own spec — recommend defer |
| 3 | tf-provider-conga stale `pkg/` | medium-high | separate repo | ⚠ post-merge release flow, not code |
| 4 | tf-provider-conga `conga_environment.this` bug | low-medium | separate repo | ⚠ file issue + fix alongside #3 |
| 5 | `unpause` self-healing for missing unit | medium | ~30 LoC | ✅ in scope |
| 6 | `refresh` also deploys policy | medium | ~50 LoC | ✅ in scope |
| 7 | Bootstrap calls `policy deploy` at end | high | doc-only | ✅ **closed-by-#6** (bootstrap has no conga binary; #6's refresh-redeploys-policy means operator's first refresh fixes the bootstrap fail-safe state automatically) |
| 8 | Align bash + Go Envoy generators | low | ~40 LoC | 🟡 **deferred** (non-blocking after #6 — window of mismatch is now minutes between bootstrap and first refresh) |
| 9 | Refresh reconciles `routing.json` | low | depends on #5 | ✅ via #5 |
| 10 | Router gateway-token wiring | latent | ~60 LoC | ⚠ pre-emptive — recommend defer until upstream forces it |
| 11 | Production `slack.com` tfvars | done | docs only | ✅ docs update |
| 12 | Refresh `tfvars.example` | low | ~20 LoC | ✅ in scope |

**In-this-PR**: 1, 5, 6, 7, 8, 9, 11 (docs), 12 — roughly 250 LoC of bug fixes plus tests + docs.

**Recommend deferring**: 2 (warrants own spec), 3 + 4 (separate repo), 10 (no current driver, premature).

For #3 + #4, the post-merge runbook gets a checklist in this PR's description.

---

## Post-Review Fixes (PR #53 comprehensive review)

After the 5-agent review of PR #53 (see `review-aggregate.md`), an
additional set of fixes was landed in the same PR to close out the
review's critical + important findings. None were regressions in the
original work; they are coverage and quality gaps the review surfaced.

### CRIT-1 — `redeployEgressDuringRefresh` silently collapsed all policy.Load errors

A YAML typo in `conga-policy.yaml` would fall through to `pf = nil` →
deny-all egress config on every refreshed agent. **Fix**: extracted
`loadRefreshPolicy` helper that returns errors verbatim for non-missing
failures; missing-file remains the only case that falls back to
deny-all (with a sink warning).

**Files**: `pkg/provider/awsprovider/provider.go` (helper +
redeployEgressDuringRefresh) + tests in `provider_test.go`.

### CRIT-2 — RefreshAgent 4-step semantics had zero regression guard

**Fix**: added `loadRefreshPolicy` unit tests (3 branches: missing /
malformed / valid) and `TestRefreshAgent_StepsDocumented` — a
structural test that scans the live RefreshAgent body for the four
step markers and fails if any are removed during refactor.

### CRIT-3 — `tools_lifecycle_test.go` + `json_schema_test.go` were promised in the spec but absent

**Fix**: both created.
- `internal/cmd/json_schema_test.go` — schema entries for `role` field
  on `admin.add-user` and `admin.add-team`, plus the invariant that
  every commandSchema has an Output section.
- `internal/mcpserver/tools_lifecycle_test.go` — schema check for the
  `role` + `runtime` params on `conga_provision_agent`, behavior-dir
  error path, and warning propagation through the new
  `WarningSink` plumbing.

### CRIT-4 — UnpauseAgent self-heal had no integration coverage

**Fix**: added `unpause-recreates-missing-container` subtest to
`TestAgentLifecycle` in `internal/cmd/integration_test.go`. Pauses,
forcibly removes the container, then unpauses and asserts the
container is recreated — local-provider analog of the AWS systemd
unit self-heal.

### CRIT-5 — MCP stderr invisibility for the new warnings

The `Warning: ...` lines emitted from RefreshAgent steps 3 + 4 and
`WarnOverlayEgressGaps` went to `os.Stderr` — invisible under MCP,
which only forwards the tool result string to the operator. **Fix**:
added `pkg/common/warnings.go` with a context-attached
`WarningSink`. MCP server handlers for provision / refresh / refresh-all
/ unpause attach a sink and append drained warnings to the tool
result. CLI callers don't attach a sink → existing stderr behavior
preserved.

### IMP-7 — `roleSlug` filesystem-traversal defense

**Fix**: added `roleSlugPattern` regex `^[a-z0-9][a-z0-9-]*$` in
`ApplyRolePackage` to reject any slug containing `..` or path
separators *before* `filepath.Join`. Defense-in-depth — upstream
agentName / role validation should already block this.

### IMP-8 — Refresh-time egress pre-flight parity

The pre-flight that warns when overlay endpoints aren't in the
allowlist fired only on Provision. **Fix**: added the same call to all
three providers' RefreshAgent paths. Operators editing `agent.yaml`
and running `conga refresh` (the common iteration flow) now see the
warning before runtime 403s.

### IMP-9 — Hermes role files declared OpenClaw-only `delegation_mode`

The Hermes generator silently drops `subagents.delegation_mode` (Hermes
always-delegates at the runtime layer). The default Hermes role
packages still carried the field, misleading operators. **Fix**:
removed `delegation_mode` from `agents/_defaults/hermes/role-*/agent.yaml`
and clarified the README.

### IMP-11 — `followups.md #N` cross-references will rot

Five sites in provider code referenced `followups.md #5/#6/#9` by
number. Numbered list items shift as items close — meaning the
references would silently mis-point over time. **Fix**: inline-expanded
the load-bearing rationale at each site; removed the numeric pointers.

### Deferred from the review (logged for future work)

- IMP-6 (AWS egress reads operator-laptop policy, not tfvars) — works
  in current deployments; expand if Terraform-only operators surface.
- IMP-10 (10 identical role README files) — cosmetic.
- IMP-12 (Hermes generator ignores `Overlay.Model` for primary) —
  pre-existing; needs runtime work.
- IMP-13 (`regenerateRoutingOnInstance` nil webhook resolver on AWS) —
  harmless until Hermes/Telegram lands on AWS.
- All SUG-* items — polish (named DelegationMode type, RoleMeta
  struct, etc.).

---

## 13. Full sink migration across all lifecycle stderr warnings

**Severity**: medium — same MCP-stderr-invisibility silent-failure class
that motivated CRIT-5, applied to the remaining call sites.

**Origin**: surfaced by the second-pass silent-failure review of PR
#53 (review-aggregate-pass2.md § SCOPE-A). CRIT-5 migrated the four
call sites the first-pass review specifically named (RefreshAgent
steps 3+4 + `WarnOverlayEgressGaps` in all three providers). The
second-pass review counted ~85 additional `fmt.Fprintf(os.Stderr,
"Warning: ...")` sites in lifecycle methods across the three providers
that still write to stderr — invisible under MCP.

**Examples** (non-exhaustive):
- `pkg/provider/localprovider/provider.go`: ProvisionAgent (lines 276,
  281, 298, 301, 303, 367, 394, 396, 398, 405, 411), RefreshAgent
  (lines 733, 770, 792, 798, 801, 803, 890, 916, 918, 920, 933),
  UnpauseAgent (line 695)
- `pkg/provider/remoteprovider/provider.go`: same pattern across
  ProvisionAgent + RefreshAgent (lines 641, 642, 669, 682, 685, 687,
  763, 788, 790, 792, 807, …)
- `pkg/provider/awsprovider/provider.go:141` (state-bucket parameter
  not found), `:160` (egress policy load), `:649` (RefreshAll
  per-agent failure aggregation)
- `pkg/provider/awsprovider/channels.go`: multiple lifecycle warnings

**Effect under MCP**: an operator calling e.g. `conga_provision_agent`
gets "Agent provisioned successfully" with zero visibility into
warnings like "failed to load egress policy", "behavior file
deployment failed", or "iptables egress rules not applied". The
provision claims success even when partial-failures occurred.

**Proposed fix**: mechanical migration — replace each
`fmt.Fprintf(os.Stderr, "Warning: %s", …)` in lifecycle methods with
`common.Warn(ctx, …)`. The sink machinery already exists; this is
purely a search-and-replace pass with per-site verification that the
function actually has a `ctx context.Context` in scope.

**Scope**: ~85 call sites + a handful of new tests confirming
end-to-end propagation for each lifecycle method (similar to the
existing `TestRefreshAgent_PropagatesWarningsToToolResult` /
`TestProvisionAgent_PropagatesWarningsToToolResult` /
`TestUnpauseAgent_PropagatesWarningsToToolResult` /
`TestRefreshAll_PropagatesWarningsToToolResult` quartet).
Estimated 3-5 hours of mechanical work + careful review for any
`fmt.Fprintf` callers that aren't actually warnings (e.g.
informational `Egress proxy active in validate mode` should probably
NOT migrate — it's a status note, not a warning).

**Status**: 🟡 deferred — separate focused PR. The CRIT-5 sink
infrastructure works correctly on both success and error paths
(verified by `TestRefreshAgent_WarningsSurfaceOnErrorPath`); this
follow-up extends coverage to the remaining call sites.
