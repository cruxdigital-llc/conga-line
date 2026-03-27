# Conga Line Demo Script

## Prerequisites (complete before presentation)

- [ ] VPS provisioned (4GB+ RAM, Ubuntu/Debian, SSH accessible)
- [ ] Slack workspace and app created (see below)
- [ ] SSH connectivity verified from presentation machine
- [ ] `demo.env` ready in project root (SSH, Slack, and API key values)
- [ ] Egress allowlist domains decided (see Step 5 — policy is set via MCP tools, no file needed)

## Creating the Slack App

### 1. Create a new Slack workspace

Go to https://slack.com/create and create a workspace for the demo.

### 2. Create the app from manifest

1. Go to https://api.slack.com/apps and click **Create New App** → **From an app manifest**
2. Select your demo workspace
3. Choose **JSON** and paste the manifest below
4. Review and click **Create**

```json
{
    "display_information": {
        "name": "CongaLine",
        "description": "CongaLine connector for OpenClaw",
        "background_color": "#000000"
    },
    "features": {
        "app_home": {
            "home_tab_enabled": false,
            "messages_tab_enabled": true,
            "messages_tab_read_only_enabled": false
        },
        "bot_user": {
            "display_name": "CongaLine",
            "always_online": false
        },
        "slash_commands": [
            {
                "command": "/openclaw",
                "description": "Send a message to OpenClaw",
                "should_escape": false
            }
        ],
        "assistant_view": {
            "assistant_description": "OpenClaw AI assistant",
            "suggested_prompts": [
                {
                    "title": "Say hello",
                    "message": "Hey, what can you do?"
                }
            ]
        }
    },
    "oauth_config": {
        "scopes": {
            "bot": [
                "assistant:write",
                "chat:write",
                "channels:history",
                "channels:read",
                "groups:history",
                "groups:read",
                "im:history",
                "im:read",
                "im:write",
                "mpim:history",
                "mpim:read",
                "mpim:write",
                "users:read",
                "app_mentions:read",
                "reactions:read",
                "reactions:write",
                "pins:read",
                "pins:write",
                "emoji:read",
                "commands",
                "files:read",
                "files:write"
            ]
        },
        "pkce_enabled": false
    },
    "settings": {
        "event_subscriptions": {
            "bot_events": [
                "assistant_thread_started",
                "app_mention",
                "message.channels",
                "message.groups",
                "message.im",
                "message.mpim",
                "reaction_added",
                "reaction_removed",
                "member_joined_channel",
                "member_left_channel",
                "channel_rename",
                "pin_added",
                "pin_removed"
            ]
        },
        "interactivity": {
            "is_enabled": true
        },
        "org_deploy_enabled": false,
        "socket_mode_enabled": true,
        "token_rotation_enabled": false
    }
}
```

### 3. Generate the app-level token

1. Go to **Settings → Basic Information**
2. Under **App-Level Tokens**, click **Generate Token and Scopes**
3. Name: `socket-mode`, Scope: `connections:write`
4. Click **Generate** and copy the `xapp-...` token

### 4. Install to workspace

1. Go to **Settings → Install App**
2. Click **Install to Workspace** and authorize

### 5. Collect your tokens

You'll need these three values for `conga admin setup`:

| Secret | Format | Where to find |
|---|---|---|
| `slack-bot-token` | `xoxb-...` | OAuth & Permissions → Bot User OAuth Token |
| `slack-app-token` | `xapp-...` | Basic Information → App-Level Tokens |
| `slack-signing-secret` | hex string | Basic Information → Signing Secret |

## Demo Flow

The demo is driven conversationally through Claude Code, which manages the agents via the Conga MCP server. All commands below are natural-language prompts you give to Claude — it calls the appropriate MCP tools behind the scenes.

### Step 1: Bootstrap the Server

**Talk track**: "We have a bare server already provisioned — this could be a VPS, a bare metal box, or in production, an EC2 instance in a hardened AWS environment. I'm going to ask Claude to bootstrap it as a gateway-only environment — no messaging platforms yet, just the core infrastructure."

> Prompt Claude: "Set up the remote server using the SSH config in demo.env — just the server setup, no Slack yet"

Claude calls `conga_setup` with the SSH credentials, image, and `install_docker: true`. No Slack tokens are passed — setup creates a gateway-only environment.

**Voiceover while running**: "Claude connected to the server, installed the container runtime, and created the secrets store. Notice we didn't configure Slack yet — the agents will start with a web UI only. We'll add Slack as a separate step. This modular approach means messaging platforms are a capability layer, not a requirement."

---

### Step 2: Provision the Agents

**Talk track**: "Now let's create two agents — a personal assistant for me, and a team agent."

> Prompt Claude: "Provision a user agent called 'aaron' and a team agent called 'team'"

Claude calls `conga_provision_agent` twice in parallel — no channel bindings.

**Voiceover**: "Each agent gets its own isolated container, its own network, and its own secrets store. Right now they're gateway-only — accessible through a web UI. We'll bind them to Slack in a moment."

---

### Step 3: Set API Keys

**Talk track**: "Each agent needs an API key to talk to the AI model. Users self-serve this — the admin never sees their keys."

> Prompt Claude: "Set the anthropic API key for both agents using the key in demo.env"

Claude reads the key from the file and calls `conga_set_secret` for each agent.

---

### Step 4: Add Slack Integration

**Talk track**: "Now let's add Slack as a messaging channel. This is a separate step — you add channels when you're ready, not during initial setup."

> Prompt Claude: "Add Slack as a channel using the tokens in demo.env"

Claude calls `conga_channels_add` with the Slack bot token, signing secret, and app token. This stores the credentials and starts the Slack event router.

**Voiceover**: "One tool call configured Slack and started the router. The router holds a single connection to Slack and fans out events to the right agent. Now let's bind our agents to specific Slack channels and DMs."

> Prompt Claude: "Bind agent aaron to slack:U0ANSPZPG9X and agent team to slack:C0ANFAD41GB"

Claude calls `conga_channels_bind` twice — binding the user agent to a member ID (DMs) and the team agent to a channel ID.

**Voiceover**: "Each agent is now connected to Slack — the personal agent responds to DMs, and the team agent responds to @mentions in a channel. The binding is platform-agnostic — the same interface supports any messaging platform."

---

### Step 5: Apply Egress Policy

**Talk track**: "Before we start the agents, let's talk about governance. These agents will be able to reach the internet — they need to hit the Anthropic API and Slack. But should they be able to reach *anything*? In an enterprise, the answer is no. Let's check what policy is in place right now."

> Prompt Claude: "Show me the current egress policy"

Claude calls `conga_policy_get` and shows that no policy exists — the agents would be unrestricted.

**Voiceover**: "Right now there's no policy — if we started the agents, they could reach any domain on the internet. Let's lock that down before we bring them up."

> Prompt Claude: "Set an egress policy that only allows api.anthropic.com, *.slack.com, and *.slack-edge.com, in enforce mode"

Claude calls `conga_policy_set_egress` with the allowed domains and mode. The response shows the updated policy with the three allowed domains and `enforce` mode — confirming the change in one step.

**Voiceover**: "One prompt took us from no policy to a locked-down allowlist. No config files, no SSH — Claude set the policy through the management API. Now let's deploy it and bring the agents up."

---

### Step 6: Deploy and Verify

**Talk track**: "Now we deploy the policy and start the agents. One step picks up the secrets we set earlier and the egress policy we just defined."

> Prompt Claude: "Deploy the policy and check the status of both agents"

Claude calls `conga_policy_deploy` to validate the policy and refresh all agents, then `conga_get_status` for each to confirm they're running.

**Voiceover**: "Both containers are up with the egress proxy active from the start. Each agent has its own proxy that only allows traffic to the three domains we whitelisted. The agent can reach the AI model and Slack, but nothing else. This is per-agent isolation — one agent's allowlist doesn't affect another's."

---

### Step 7: Connect and Seed Memories

**Talk track**: "Each agent has a web gateway. Let's connect through a secure tunnel — no ports are exposed to the internet."

> Prompt Claude: "Connect me to the aaron agent"

Claude calls `conga_connect_help` which provides the tunnel command. Run it to open the gateway in a browser.

**In the aaron agent's web UI**, tell it: "My favorite animal is the capybara." Wait for it to confirm it saved the memory.

> Prompt Claude: "Now connect me to the team agent"

**In the team agent's web UI**, tell it: "Our team mascot is the pangolin." Wait for it to confirm.

Then tell it: "We're planning a team building trip to Disneyland next month." Wait for it to confirm.

**Voiceover**: "Each agent has its own persistent memory. What I just told each one is stored independently — the personal agent knows my favorite animal, and the team agent knows the team mascot and about the Disneyland trip."

---

### Step 8: Egress Policy in Action

**Talk track**: "Now let's see what happens when an agent tries to reach a domain that isn't in the allowlist."

> In the team agent's web UI, ask: "Can you look up the current ticket prices for Disneyland on disney.com and help plan our day?"

The agent will try to fetch disney.com and fail — the egress proxy blocks the request. The agent should report that it couldn't access the site.

**Voiceover**: "The agent knows about the trip — that's in its memory. But when it tries to reach disney.com to get live information, the egress proxy blocks it. It can only reach the three domains we explicitly allowed: the AI model, and Slack. This isn't a firewall rule on the VPS — it's a per-agent proxy with its own allowlist. If we wanted the team agent to reach disney.com, we'd just ask Claude to add it to the policy and deploy — without affecting the other agent at all."

---

### Step 9: Prove Isolation via Slack

**Talk track**: "Now let's see this from the end user's perspective — and prove that these agents are truly isolated from each other."

> Switch to Slack. DM the bot: "What's my favorite animal?"

The personal agent should answer "capybara." It has this in its memory.

> Then ask it: "What's our team mascot?"

It shouldn't know — that memory lives in the team agent, not here.

> Switch to the Slack channel. @mention the bot: "What's our team mascot?"

The team agent should answer "pangolin."

> Then ask it: "What's Aaron's favorite animal?"

It shouldn't know — that's the personal agent's memory.

**Voiceover**: "Same Slack app, same server, but completely isolated memory and context. Each agent only knows what it's been told directly. This is the same isolation model you get on AWS — separate containers, separate networks, separate secrets stores, separate egress proxies."

---

### Step 10: Wrap Up

**Talk track — bridge to enterprise**:
- "Everything I just showed you runs identically on hardened AWS — same CLI, same commands"
- "On AWS, the server sits in a zero-ingress VPC with no SSH access — you connect through AWS Session Manager"
- "Secrets move from file-based to AWS Secrets Manager"
- "The egress policy is the same YAML file — on AWS it layers on top of VPC security groups for defense in depth"
- "Infrastructure is defined in Terraform — auditable, reproducible, version-controlled"
- "The model API lives in the cloud, but everything that makes your agent yours — its memory, its personality, its credentials, its conversations — lives on infrastructure you control"

---

## Recovery

If something goes wrong during the demo, prompt Claude naturally:

- "List all agents" → `conga_list_agents`
- "Refresh the aaron agent" → `conga_refresh_agent`
- "Show me the logs for aaron" → `conga_get_logs`
- "Remove the aaron agent" → `conga_remove_agent`
- "Tear it all down and start fresh" → `conga_teardown`

If the egress demo doesn't work as expected:
- "Show me the current policy" → `conga_policy_get`
- "Validate the policy" → `conga_policy_validate`
- Check proxy is running: "Run `docker ps` on the remote host" → `conga_container_exec`
- "Set egress mode to validate and deploy" → disables enforcement without removing the policy
