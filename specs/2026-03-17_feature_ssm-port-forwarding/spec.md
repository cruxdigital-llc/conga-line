# Spec: SSM Port Forwarding for Web UI

## Overview
Add per-user `gateway_port` to the Terraform `users` variable, publish each container's gateway port to localhost on the host, and output ready-to-run SSM port forwarding commands.

## Deliverables

### 1. `terraform/variables.tf` — Add `gateway_port` to user config

Extend the `users` variable type to include `gateway_port`:

```hcl
variable "users" {
  description = "Map of user IDs to their config. Admin adds entries, users self-serve secrets."
  type = map(object({
    slack_channel = string
    gateway_port  = number
  }))
  default = {
    UEXAMPLE01 = {
      slack_channel = "CEXAMPLE01"
      gateway_port  = 18789
    }
    UEXAMPLE02 = {
      slack_channel = "CEXAMPLE02"
      gateway_port  = 18790
    }
  }

  validation {
    condition = alltrue([
      for uid, cfg in var.users : cfg.gateway_port >= 18789 && cfg.gateway_port <= 18889
    ])
    error_message = "All gateway_port values must be in range 18789-18889."
  }

  validation {
    condition = length(values(var.users)[*].gateway_port) == length(distinct(values(var.users)[*].gateway_port))
    error_message = "All gateway_port values must be unique."
  }
}
```

### 2. `terraform/user-data.sh.tftpl` — Publish container port to localhost

**Line 273** — Add `-p` flag to the `docker run` command in the systemd unit's `ExecStart`:

Before:
```bash
ExecStart=/usr/bin/docker run --name conga-$USER_ID --network conga-$USER_ID --cap-drop ALL --security-opt no-new-privileges --memory 2g --cpus 1.5 -e NODE_OPTIONS="--max-old-space-size=1536" --pids-limit 256 -v /opt/conga/data/$USER_ID:/home/node/.openclaw:rw $ENV_FLAGS $CONGA_IMAGE
```

After:
```bash
ExecStart=/usr/bin/docker run --name conga-$USER_ID --network conga-$USER_ID -p 127.0.0.1:${user_config.gateway_port}:18789 --cap-drop ALL --security-opt no-new-privileges --memory 2g --cpus 1.5 -e NODE_OPTIONS="--max-old-space-size=1536" --pids-limit 256 -v /opt/conga/data/$USER_ID:/home/node/.openclaw:rw $ENV_FLAGS $CONGA_IMAGE
```

The `-p 127.0.0.1:${user_config.gateway_port}:18789` flag:
- Binds to `127.0.0.1` only (not `0.0.0.0`) — no external exposure
- Maps the user's unique host port to the container's fixed gateway port 18789
- Works with the existing `"gateway": { "port": 18789, "bind": "lan" }` in `openclaw.json`

**Line 458** — Update the status echo to include port:

Before:
```bash
echo "Service: conga-${user_id} (channel: ${user_config.slack_channel}, HTTP webhook mode)"
```

After:
```bash
echo "Service: conga-${user_id} (channel: ${user_config.slack_channel}, port: ${user_config.gateway_port}, HTTP webhook mode)"
```

### 3. `terraform/outputs.tf` — Add SSM port forward commands

Append a new output that generates a ready-to-run SSM port forwarding command per user:

```hcl
output "ssm_port_forward_commands" {
  description = "SSM port forwarding commands per user"
  value = {
    for uid, cfg in var.users : uid => join(" ", [
      "aws ssm start-session",
      "--target ${aws_instance.conga.id}",
      "--region ${var.aws_region}",
      "--profile ${var.aws_profile}",
      "--document-name AWS-StartPortForwardingSession",
      "--parameters '{\"portNumber\":[\"${cfg.gateway_port}\"],\"localPortNumber\":[\"${cfg.gateway_port}\"]}'"
    ])
  }
}
```

This uses the built-in `AWS-StartPortForwardingSession` SSM document — no custom documents needed.

## No Changes Needed

| File | Reason |
|------|--------|
| `terraform/security.tf` | SSM uses existing outbound HTTPS; no ingress rules needed |
| `terraform/router.tf` | Passes `users` variable through; no structural changes |
| `terraform/iam.tf` | Instance role already has SSM permissions |
| `openclaw.json` config | `"gateway": { "bind": "lan" }` already works with Docker's `-p` |

## Edge Cases

| Scenario | Handling |
|---|---|
| Port collision between users | Terraform validation rejects duplicate ports |
| Port out of range | Terraform validation rejects ports outside 18789-18889 |
| Container not running | SSM tunnel connects but browser gets "connection refused" — user checks `systemctl status` |
| Multiple SSM tunnels to same port | SSM allows multiple sessions; only one local port bind succeeds |
| Gateway auth token not set | Phase 2 concern — SSM tunnel provides IAM-based authentication for now |
| Instance replacement | New instance ID in output; user re-runs `terraform output` for updated command |

## Validation Steps

1. `terraform validate` / `terraform plan` — confirm clean, no unexpected changes beyond launch template
2. `terraform apply` — replaces instance (user-data changed)
3. SSM into instance, verify:
   - `docker port conga-UEXAMPLE01` shows `127.0.0.1:18789->18789/tcp`
   - `docker port conga-UEXAMPLE02` shows `127.0.0.1:18790->18790/tcp`
4. From local machine, run the SSM port forward command from `terraform output ssm_port_forward_commands`
5. Open `http://localhost:18789` in browser — should see Conga Line web UI
