# Requirements: SSM Port Forwarding for Web UI

## Goal
Expose each user's OpenClaw web UI via AWS SSM port forwarding. Users tunnel from their local machine to the container's gateway port without any VPC ingress changes.

This is **Phase 1: Get the UI visible**. User isolation (per-user SSM documents, IAM restrictions, gateway auth tokens) will be added in a follow-up.

## Success Criteria
1. Each user has a unique `gateway_port` in the `users` Terraform variable (range 18789-18889)
2. Docker containers publish their gateway port to `127.0.0.1` on the host
3. Terraform outputs a ready-to-run `aws ssm start-session` port forwarding command per user
4. User can open `http://localhost:<port>` in their browser after running the SSM command
5. No ingress security group rules added — SSM uses existing outbound HTTPS path

## Design Decisions
- **Localhost-only binding** (`127.0.0.1:<port>:18789`) — port is never exposed on the host's network interface
- **Built-in SSM document** (`AWS-StartPortForwardingSession`) — no custom SSM documents needed for Phase 1
- **Port range 18789-18889** — avoids well-known ports, gives room for 100 users
- **No gateway auth token yet** — Phase 2 concern; the SSM tunnel itself provides authentication via IAM
