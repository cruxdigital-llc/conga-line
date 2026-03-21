# Requirements: Multi-User Onboarding (Epics 5+6)

## Goal
Refactor Terraform to support multiple users from a simple config variable. Users self-serve their own secrets and skill configuration.

## Roles
- **Admin**: Adds user_id + slack_channel to Terraform, runs apply, creates Slack channel
- **User**: Has own IAM user in the account. Runs onboarding script to add their secrets. Configures skills through Conga Line itself.

## Success Criteria
1. `users` variable drives all per-user resources (no hardcoded user config)
2. Terraform creates: per-user IAM secret path policy, per-user Slack channel config, per-user container + systemd unit
3. Terraform does NOT create per-user secrets — users manage their own
4. Shared secrets (Slack tokens) remain in Terraform
5. User-data dynamically bootstraps containers for all users in the variable
6. Onboarding script lets a user:
   - Add required secret (Anthropic API key) under their path
   - Add optional secrets as needed (any name)
   - List their existing secrets
7. `openclaw.json` is generic — no per-user skill configuration
8. Adding a user = add to tfvars + apply + share onboarding instructions
9. Both users' containers running, isolated, responding in correct Slack channels
