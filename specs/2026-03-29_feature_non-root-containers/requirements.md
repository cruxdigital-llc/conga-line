# Requirements — Non-Root Container Enforcement

## Goal
Enforce non-root execution for all Docker containers (agent and router) across all three providers by adding explicit `--user 1000:1000` to `docker run` commands. Eliminates reliance on upstream image `USER` directives.

## Success Criteria
1. `docker inspect --format '{{.Config.User}}' conga-<agent>` returns `1000:1000` on all providers
2. `docker inspect --format '{{.Config.User}}' conga-router` returns `1000:1000` on all providers
3. Egress proxy unchanged (`--user 101:101` already set)
4. All existing functionality preserved (gateway, Slack routing, egress proxy, config integrity)
5. Security documentation reflects explicit enforcement
