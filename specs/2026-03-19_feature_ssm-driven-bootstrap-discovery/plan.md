# Plan: SSM-Driven Bootstrap Discovery

## Approach

Replace all Terraform template loops in the bootstrap script with bash that queries SSM Parameter Store at boot time. SSM becomes the single source of truth for what agents exist. `var.agents` continues to drive SSM parameter creation (and Terraform-time resources like dashboards), but the bootstrap itself becomes static — no agent-specific content baked in at `terraform apply` time.

## Steps

### Step 1: Enrich SSM parameters + add config params

**Files:** `terraform/ssm-parameters.tf`, `cli/cmd/admin.go`

**What:**
- Add SSM parameter `/openclaw/config/openclaw-image` storing `var.openclaw_image`
- Add `type` field to user SSM param values (`"type": "user"`) and team SSM param values (`"type": "team"`)
- Add `member_id` explicitly to user param values (currently inferred from path — being explicit is safer for the discovery loop)
- Update CLI's `adminAddUserRun` to write `type` and `member_id` into SSM JSON
- Update CLI's `adminAddTeamRun` to write `type` into SSM JSON
- Add doc comment: `var.agents` drives SSM creation + Terraform-time resources; CLI-added agents discovered at boot from SSM

**Why:** The bootstrap needs to determine agent type and all provisioning details from SSM alone, without Terraform template interpolation.

**Testable independently:** Yes — `terraform apply`, verify params with `aws ssm get-parameter`. `cruxclaw admin list-agents` still works (extra JSON fields are ignored by the CLI's unmarshal).

### Step 2: Widen IAM secrets policy

**File:** `terraform/iam.tf`

**What:** Replace the per-user ARN enumeration:
```hcl
[for k, v in local.user_agents : "...openclaw/${v.member_id}/*"]
```
with wildcards:
```hcl
"...openclaw/U*"   # user secrets (member IDs always start with U)
"...openclaw/teams/*"  # team secrets
```

**Why:** CLI-added agents (not in `var.agents`) need their container to access per-agent secrets. The current per-user enumeration only covers Terraform-managed agents.

**Architect note:** This is slightly broader than per-user enumeration but still scoped to `openclaw/` prefix. The instance already has `ListSecrets` on `*`. The incremental risk is minimal — single-tenant infrastructure where any process could already enumerate secrets.

**Testable independently:** Yes — can be applied separately.

### Step 3: Rewrite bootstrap for SSM discovery + update router.tf

**Files:** `terraform/user-data.sh.tftpl`, `terraform/router.tf`

**Must be atomic** — template file and variable map must agree.

**What (user-data.sh.tftpl):**

a. **Add `jq` to package install (section 2):**
   ```bash
   dnf install -y docker nodejs npm jq
   ```

b. **Read `openclaw_image` from SSM (section 4):**
   Replace `OPENCLAW_IMAGE="${openclaw_image}"` with:
   ```bash
   OPENCLAW_IMAGE=$(aws ssm get-parameter --name "/openclaw/config/openclaw-image" \
     --query "Parameter.Value" --output text --region "$AWS_REGION")
   ```

c. **Remove static `routing.json` (section 5):**
   Delete the `cat > routing.json` heredoc that uses `${routing_json}`. Routing will be built after agent discovery.

d. **Replace section 6 entirely:**
   Remove the `%{ for agent_name, agent_config in agents }` ... `%{ endfor }` block. Replace with:

   - Query `/openclaw/users/` and `/openclaw/teams/` via `aws ssm get-parameters-by-path`
   - Filter out `/openclaw/users/by-iam/*` params (these are IAM mappings, not agent configs)
   - Parse each param with `jq` to extract `type`, `member_id`/team name, `slack_channel`, `gateway_port`
   - For each agent: build env file (shared secrets + per-agent secrets), generate `openclaw.json` (user vs team variant), create data dir, config hash, Docker network, systemd unit
   - Collect container IDs and routing entries as we go
   - Write `routing.json` at the end from collected entries

   Extract two bash functions for readability:
   - `setup_user_agent <container_id> <member_id> <gateway_port>` — mirrors `add-user.sh.tmpl` logic
   - `setup_team_agent <container_id> <team_name> <slack_channel> <gateway_port>` — mirrors `add-team.sh.tmpl` logic

   Handle empty case (no agents in SSM): log warning, write empty routing.json, continue.

e. **Sections 7-9 (integrity check, session metrics, CloudWatch):** No template loops — these already use filesystem globs. No changes needed.

f. **Replace section 10 template loops:**
   Replace the `%{ for }` loops for network connect, service enable/start, and status output with bash loops over the collected `ALL_CONTAINER_IDS` variable.

**What (router.tf):**
   Remove `agents`, `agent_container_id`, `openclaw_image`, and `routing_json` from the `templatefile()` call. Remaining vars: `aws_region`, `project_name`, `config_check_interval_minutes`, `state_bucket`.

**QA edge cases:**
- `/openclaw/users/by-iam/*` params must be filtered out — they're IAM mappings, not agent configs
- `jq` base64 iteration pattern (`jq -r '.[] | @base64'`) handles values with spaces/special chars safely
- `GetParametersByPath` returns max 10 results by default — need `--recursive` flag or pagination. For current scale (<10 agents) this is fine, but the script should handle it
- If SSM is unreachable at boot (IAM propagation delay), the script fails on `set -e`. The `get_secret` calls earlier in the script would fail first, so this is consistent behavior
- Empty SSM results (fresh deploy before any agents added): script logs warning, writes empty routing.json, router starts with no routes

**Consequence:** The bootstrap S3 object content hash no longer changes when agents change. Adding/removing agents in `var.agents` only affects SSM parameters — no instance replacement triggered.

### Step 4: Update CLI SSM parameter reads (if needed)

**File:** `cli/cmd/admin.go`, `cli/internal/discovery/user.go`

**What:** Verify the CLI's `list-agents`, `ResolveUser`, and `ResolveTeam` still work with the enriched SSM parameter values from step 1. The extra fields (`type`, `member_id`) should be harmlessly ignored by Go's `json.Unmarshal`, but verify.

**Likely no code changes needed** — just verification.

## What stays unchanged

- **`monitoring.tf`** — CloudWatch dashboards use `var.agents` at Terraform time. CLI-added agents still publish metrics (session-metrics script uses filesystem glob), they just won't appear on the Terraform-managed dashboard. Acceptable.
- **`outputs.tf`** — SSM port-forward commands use `var.agents`. CLI agents use `cruxclaw connect` which reads SSM directly.
- **`secrets.tf`** — cleanup provisioner stays Terraform-driven. CLI agents cleaned up via `cruxclaw admin remove-user --delete-secrets`.
- **`variables.tf`** — `var.agents` definition, validations, and locals all stay. The locals (`user_agents`, `team_agents`, `agent_container_id`) are still used by `ssm-parameters.tf`, `monitoring.tf`, `outputs.tf`, `secrets.tf`.

## Risks

| Risk | Mitigation |
|------|-----------|
| `jq` not available on AL2023 | It's in the default repos — `dnf install -y jq` works. Verified in AL2023 docs. |
| SSM API throttling at boot | With <10 agents and 2-3 API calls per agent, well under rate limits. Not a concern at current scale. |
| Bootstrap fails if SSM param for openclaw-image doesn't exist (fresh deploy before step 1 applied) | Steps 1 and 3 must be applied in order. Step 1 creates the param, step 3 reads it. |
| `GetParametersByPath` pagination | Default page size is 10. Current deployment has 2 agents. Add a note for future if >10 agents are needed. |
| Terraform plan loses visibility into "what agents will be provisioned" | `terraform plan` shows SSM param changes. `cruxclaw admin list-agents` shows the full picture. Acceptable tradeoff. |
| Instance replacement during rollout | Apply step 1 first. After SSM params are enriched, steps 2-3 can be applied together. If instance cycles between step 1 and step 3, the old bootstrap still works (it ignores extra SSM fields). |

## Rollout Order

1. Apply **step 1** (enrich SSM params + add config param) — safe, additive only
2. Apply **step 2** (widen IAM) — safe, broadens permissions
3. Apply **step 3** (rewrite bootstrap + router.tf) — atomic, changes bootstrap behavior
4. Verify **step 4** (CLI compatibility) — no deploy needed
5. Cycle host to pick up new bootstrap

## Follow-up (not in this change)

- CLI setup scripts read `openclaw_image` from SSM instead of template var (eliminates last config divergence)
- Consolidate CLI setup scripts and bootstrap agent functions into a shared script stored in S3 (eliminates template duplication)
- `cruxclaw admin sync` command that reads SSM and reconciles with running containers (diagnose drift without rebooting)
