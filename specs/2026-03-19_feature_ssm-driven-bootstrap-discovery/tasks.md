# Implementation Tasks

## Task 1: Terraform — variables.tf
- [x] Rename `member_id` → `slack_member_id` in variable definition and all validations
- [x] Remove `agent_container_id` local
- [x] Remove `team_agents` local
- [x] Keep `user_agents` local (needed by secrets.tf)
- [x] Update `slack_member_id` uniqueness validation
- [x] `terraform validate`

## Task 2: Terraform — ssm-parameters.tf
- [x] Replace `user_config`, `team_config`, `user_iam_mapping` with single `agent_config` resource under `/conga/agents/<name>`
- [x] Add `conga_image` SSM parameter under `/conga/config/conga-image`
- [x] Add doc comment
- [x] `terraform validate`

## Task 3: Terraform — iam.tf
- [x] Replace per-user secret ARN enumeration with `conga/shared/*` + `conga/agents/*`
- [x] `terraform validate`

## Task 4: Terraform — secrets.tf
- [x] Update `for_each` and secret path to use `conga/agents/<name>/`
- [x] `terraform validate`

## Task 5: Terraform — monitoring.tf + outputs.tf
- [x] Replace `local.agent_container_id[name]` with just `name`
- [x] `terraform validate`

## Task 6: Terraform — router.tf + user-data.sh.tftpl (atomic)
- [x] router.tf: Remove `agents`, `agent_container_id`, `conga_image`, `routing_json` from `templatefile()`
- [x] user-data.sh.tftpl: Add `jq` to dnf install
- [x] user-data.sh.tftpl: Read `conga_image` from SSM
- [x] user-data.sh.tftpl: Remove static `routing.json` heredoc
- [x] user-data.sh.tftpl: Define `setup_user_agent`, `setup_team_agent`, `setup_agent_common` bash functions
- [x] user-data.sh.tftpl: Replace section 6 template loop with SSM discovery + function calls
- [x] user-data.sh.tftpl: Build `routing.json` dynamically after discovery
- [x] user-data.sh.tftpl: Handle empty agents case (warning + empty routing)
- [x] user-data.sh.tftpl: Handle unknown agent type (warning + skip)
- [x] user-data.sh.tftpl: Replace section 10 template loops with bash loops
- [x] user-data.sh.tftpl: Replace final status output template loop
- [x] Remove all `%{ for }` / `%{ if }` / `%{ endfor }` / `%{ endif }` template directives
- [x] `terraform validate`

## Task 7: Terraform — terraform.tfvars + terraform.tfvars.example
- [x] Update `terraform.tfvars` with `slack_member_id` field name
- [x] Update `terraform.tfvars.example` with new schema and placeholder IDs

## Task 8: CLI — discovery refactor (`cli/pkg/discovery/`)
- [x] Create `agent.go` with unified `AgentConfig` struct
- [x] `ResolveAgent(ctx, ssmClient, name)` — direct lookup at `/conga/agents/<name>`
- [x] `ResolveAgentByIAM(ctx, ssmClient, iamIdentity)` — scan + match `iam_identity`
- [x] `ListAgents(ctx, ssmClient)` — list all agents
- [x] Delete `user.go` (removed `ResolveUser`, `ResolveTeam`, `UserConfig`, `TeamConfig`)
- [x] Update `identity.go`: replace `by-iam` lookup with `ResolveAgentByIAM`, `MemberID` → `AgentName`
- [x] `go build ./...`

## Task 9: CLI — admin commands (`cli/cmd/admin.go`)
- [x] `add-user`: 2 args `<name> <slack_member_id>`, validate name with `validateAgentName`, write to `/conga/agents/<name>`
- [x] `add-team`: update SSM path to `/conga/agents/<name>`, include `type` in JSON
- [x] Merge `remove-user` + `remove-team` → `remove-agent <name>`
- [x] `list-agents`: single call via `discovery.ListAgents`
- [x] `resolveGatewayPort`: single call via `discovery.ListAgents`
- [x] Add `validateAgentName` (lowercase alphanumeric + hyphens)
- [x] Remove `map-user` command (no longer needed — iam_identity is in agent config)
- [x] Update command registration in `init()`
- [x] `go build ./...`

## Task 10: CLI — setup scripts + other commands
- [x] `add-user.sh.tmpl`: rename `MemberID` → `AgentName` + `SlackMemberID`, all paths use `AgentName`, secrets under `conga/agents/$AGENT_NAME/`
- [x] `add-team.sh.tmpl`: rename `TeamName` → `AgentName`, secrets under `conga/agents/$AGENT_NAME/`
- [x] `refresh-user.sh.tmpl`: rename `MemberID` → `AgentName`, secrets under `conga/agents/$AGENT_NAME/`
- [x] Update `connect.go`, `status.go`, `logs.go`, `secrets.go`, `auth.go`, `refresh.go` to use `resolveAgentName` and agent-name-based paths
- [x] `go build ./...`

## Task 11: Verify
- [x] `terraform validate` passes
- [x] `terraform fmt -check -recursive` passes
- [x] `go build ./...` passes
- [x] `terraform.tfvars` has correct structure
