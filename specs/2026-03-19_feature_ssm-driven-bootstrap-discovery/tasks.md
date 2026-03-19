# Implementation Tasks

## Task 1: Terraform — variables.tf
- [ ] Rename `member_id` → `slack_member_id` in variable definition and all validations
- [ ] Remove `agent_container_id` local (no longer needed — agent name = container ID)
- [ ] Remove `team_agents` local if unused after other changes
- [ ] Keep `user_agents` local (needed by secrets.tf)
- [ ] Update `slack_member_id` uniqueness validation
- [ ] `terraform validate`

## Task 2: Terraform — ssm-parameters.tf
- [ ] Replace `user_config`, `team_config`, `user_iam_mapping` with single `agent_config` resource under `/openclaw/agents/<name>`
- [ ] Add `openclaw_image` SSM parameter under `/openclaw/config/openclaw-image`
- [ ] Add doc comment: `var.agents` drives SSM creation + Terraform-time resources; CLI-added agents discovered at boot
- [ ] `terraform validate`

## Task 3: Terraform — iam.tf
- [ ] Replace per-user secret ARN enumeration with `openclaw/shared/*` + `openclaw/agents/*`
- [ ] `terraform validate`

## Task 4: Terraform — secrets.tf
- [ ] Update `for_each` and secret path to use `openclaw/agents/<name>/`
- [ ] `terraform validate`

## Task 5: Terraform — monitoring.tf + outputs.tf
- [ ] Replace `local.agent_container_id[name]` with just `name`
- [ ] `terraform validate`

## Task 6: Terraform — router.tf + user-data.sh.tftpl (atomic)
- [ ] router.tf: Remove `agents`, `agent_container_id`, `openclaw_image`, `routing_json` from `templatefile()`
- [ ] user-data.sh.tftpl: Add `jq` to dnf install
- [ ] user-data.sh.tftpl: Read `openclaw_image` from SSM
- [ ] user-data.sh.tftpl: Remove static `routing.json` heredoc
- [ ] user-data.sh.tftpl: Define `setup_user_agent` and `setup_team_agent` bash functions (agent name as container ID, secrets under `openclaw/agents/<name>/`)
- [ ] user-data.sh.tftpl: Replace section 6 template loop with SSM discovery + function calls
- [ ] user-data.sh.tftpl: Build `routing.json` dynamically after discovery
- [ ] user-data.sh.tftpl: Handle empty agents case (warning + empty routing)
- [ ] user-data.sh.tftpl: Handle unknown agent type (warning + skip)
- [ ] user-data.sh.tftpl: Replace section 10 template loops with bash loops
- [ ] user-data.sh.tftpl: Replace final status output template loop
- [ ] Remove all `%{ for }` / `%{ if }` / `%{ endfor }` / `%{ endif }` template directives
- [ ] `terraform validate`

## Task 7: Terraform — terraform.tfvars + terraform.tfvars.example
- [ ] Update `terraform.tfvars` with `slack_member_id` field name
- [ ] Update `terraform.tfvars.example` with new schema and placeholder IDs

## Task 8: CLI — discovery refactor (`cli/internal/discovery/`)
- [ ] Rename `user.go` → `agent.go`
- [ ] Replace `UserConfig` + `TeamConfig` with unified `AgentConfig` struct
- [ ] `ResolveAgent(ctx, ssmClient, name)` — direct lookup at `/openclaw/agents/<name>`
- [ ] `ResolveAgentByIAM(ctx, ssmClient, iamIdentity)` — scan + match `iam_identity`
- [ ] Remove `ResolveUser`, `ResolveTeam`
- [ ] Update `identity.go`: replace `by-iam` lookup with `ResolveAgentByIAM`
- [ ] `go build ./...`

## Task 9: CLI — admin commands (`cli/cmd/admin.go`)
- [ ] `add-user`: 2 args `<name> <slack_member_id>`, validate name with `validateAgentName`, write to `/openclaw/agents/<name>`, remove `by-iam` write
- [ ] `add-team`: update SSM path to `/openclaw/agents/<name>`, include `type` in JSON
- [ ] Merge `remove-user` + `remove-team` → `remove-agent <name>`, read agent to determine type for secrets cleanup
- [ ] `list-agents`: single `GetParametersByPath` on `/openclaw/agents/`, parse `type` from value
- [ ] `resolveGatewayPort`: single `GetParametersByPath` on `/openclaw/agents/`
- [ ] Add `validateAgentName` (lowercase alphanumeric + hyphens) for all agent names
- [ ] Update command registration in `init()`
- [ ] Update all callers of `ResolveUser`/`ResolveTeam` → `ResolveAgent`
- [ ] `go build ./...`

## Task 10: CLI — setup scripts (`cli/scripts/`)
- [ ] `add-user.sh.tmpl`: rename `MemberID` → `AgentName` + `SlackMemberID`, all paths use `AgentName`, secrets under `openclaw/agents/$AGENT_NAME/`
- [ ] `add-team.sh.tmpl`: rename `TeamName` → `AgentName`, secrets under `openclaw/agents/$AGENT_NAME/`
- [ ] `embed.go`: update if any embedded file names changed
- [ ] `go build ./...`

## Task 11: Verify
- [ ] `terraform validate` passes
- [ ] `go build ./...` passes
- [ ] `terraform.tfvars` has correct structure
