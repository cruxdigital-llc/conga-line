# Requirements: Behavior Management

## Goal

Enable centrally managed, version-controlled behavior definitions (SOUL.md, AGENTS.md, USER.md) that compose differently for individual vs team agents, deploy via S3, and automatically sync on container restart — so we can evolve agent personality and guidelines over time without reprovisioning.

## Background

OpenClaw agents read workspace files at session start to define identity, tone, boundaries, and guidelines:
- **SOUL.md** — core identity and philosophy
- **AGENTS.md** — session startup checklist, red lines, tool guidelines
- **USER.md** — context about who the agent helps

Today, our deployment has no mechanism for managing these files. Agents start with empty workspaces and rely on OpenClaw's built-in defaults. Team agents (serving multiple humans in a shared channel) need different behavioral guidance than individual DM-only agents, but both currently get the same empty workspace.

## Success Criteria

1. Behavior files are version-controlled in the repo under `behavior/`
2. Team agents receive team-specific SOUL.md content; user agents receive user-specific content
3. Editing `behavior/` + `terraform apply` + `cruxclaw admin refresh-all` propagates changes to all running agents
4. MEMORY.md is never overwritten by the deployment system
5. New agents provisioned via CLI (`admin add-user`, `admin add-team`) automatically get correct behavior files

## Constraints

- Pure infrastructure change — no OpenClaw application code modifications
- Must not break existing agents during rollout
- Secrets must never appear in behavior files
- Must work with existing CLI provisioning flow and bootstrap process
- Workspace directory must remain writable (OpenClaw hot-reload writes `.tmp` files)
- Files inside container must be owned by uid 1000 (node user)
