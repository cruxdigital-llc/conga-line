# VPS Provider — Requirements

## Goal
Enable users to run always-on Conga Line agent clusters on any VPS (Hetzner, DigitalOcean, Linode, Hostinger, etc.) without AWS expertise, Terraform, or IAM. Validate the provider abstraction beyond the original two implementations.

## Target Audience
Individual developers and small teams who want cloud-hosted agents at $5-10/mo with minimal operational overhead. Only requirement: a VPS with SSH access.

## User Stories

### Setup & Provisioning
1. As a developer, I want to run `conga admin setup --provider vps` and have it connect to my VPS via SSH, install Docker if needed, and prepare the host for agents.
2. As a developer, I want setup to prompt me for SSH connection details (host, port, user, key path) and persist them so I don't re-enter them.
3. As a developer, I want setup to handle Docker installation automatically on common Linux distros (Ubuntu/Debian, CentOS/RHEL, Fedora).

### Agent Management
4. As a developer, I want `conga admin add-user` and `conga admin add-team` to create isolated Docker containers on my VPS, identical to local provider behavior.
5. As a developer, I want `conga admin pause` and `conga admin unpause` to stop/restart agents while preserving all data.
6. As a developer, I want `conga admin remove-user/remove-team` to clean up containers, networks, and optionally secrets on the VPS.

### Operations
7. As a developer, I want `conga status` to show container state, memory, CPU, and readiness from the remote host.
8. As a developer, I want `conga logs` to retrieve container logs from the remote host.
9. As a developer, I want `conga refresh` to restart an agent with fresh config/secrets on the VPS.

### Secrets
10. As a developer, I want `conga secrets set/list/delete` to manage per-agent secrets stored as files on the VPS (mode 0400).

### Connectivity
11. As a developer, I want `conga connect` to open an SSH tunnel to the agent's gateway, so I can access the web UI in my browser without exposing any ports.

### Slack Integration
12. As a developer, I want Slack to work the same way as local — router container holds the Socket Mode connection and fans out to per-agent containers via HTTP webhook.
13. As a developer, I want to run agents in gateway-only mode (no Slack) by skipping Slack credentials during setup.

### Teardown
14. As a developer, I want `conga admin teardown` to remove all containers, networks, and data from the VPS and clear local VPS config.

## Success Criteria
1. **Full lifecycle verified**: setup → add-user → add-team → status → logs → secrets → connect → pause/unpause → teardown all work on a real VPS
2. **Feature parity with local provider**: every command that works with `--provider local` also works with `--provider vps`
3. **Gateway + Slack both functional**: web UI accessible via SSH tunnel; Slack router fans out events correctly
4. **Gateway-only mode works**: agents run without Slack when no Slack credentials provided
5. **User-facing documentation**: setup guide covering VPS requirements and step-by-step walkthrough

## Constraints
- SSH key auth only (no password authentication)
- No VPS vendor API integrations — user provisions the VM themselves
- Single new dependency: `golang.org/x/crypto` (SSH, SFTP, known_hosts)
- Remote directory at `/opt/conga/` (consistent with AWS provider)
- No inbound ports beyond SSH (22) — gateway via SSH tunnel only
- Same container hardening as local: cap-drop ALL, no-new-privileges, memory/CPU/PID limits
