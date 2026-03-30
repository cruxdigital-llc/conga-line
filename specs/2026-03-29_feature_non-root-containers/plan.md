# Plan — Non-Root Container Enforcement

## Problem
Security baseline claims "Runs as uid 1000 (node)" but:
1. Agent containers have no `--user` flag (relies on image USER directive — fragile)
2. Router container uses `node:22-alpine` which defaults to root (running as root today)
3. AWS router systemd unit missing `--tmpfs` that local/remote already have

## Approach
Add `--user 1000:1000` to all agent and router `docker run` commands across 7 files, 9 invocation sites. Fix AWS router `--tmpfs` inconsistency. Update security docs.

## Out of Scope
- AWS systemd `User=` directive (ExecStartPost needs iptables/root)
- Dead `conga` system user cleanup (separate task)
- `docker exec --user` (inherits container user automatically)
- Ephemeral npm install containers (no security boundary)
