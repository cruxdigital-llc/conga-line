# Requirements: EC2 + Docker Bootstrap

## Goal
Deploy a t4g.medium EC2 instance running Aaron's Conga Line container in Docker rootless mode, connected to Slack via Socket Mode.

## Success Criteria
1. EC2 instance running in private subnet with encrypted EBS, IMDSv2 enforced
2. Docker running in rootless mode
3. Aaron's Conga Line container running with:
   - Correct openclaw.json (channel CEXAMPLE01, Trello skill, model config)
   - Secrets injected as env vars (not on disk)
   - Read-only config mount, isolated Docker network
   - Resource limits, cap-drop=ALL, no-new-privileges, seccomp
4. Persistent workspace/memory via EBS-backed Docker volume
5. Conga Line connects to Slack and responds in Aaron's channel
6. SSM Session Manager access works
7. OS hardened: no SSH, sysctl lockdown, unattended-upgrades
8. AMI: Amazon Linux 2023 arm64

## Key Decisions
- Docker rootless mode from day one
- AL2023 arm64 AMI (good SSM + Docker support)
- EBS-backed persistent volume for Conga Line workspace/memory
