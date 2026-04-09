# Per-Agent Behavior: SOUL.md

<!--
  OVERRIDE NOTICE
  Placing this file in behavior/agents/<your-agent>/SOUL.md completely
  REPLACES behavior/default/SOUL.md for that agent. The default will
  not be used.

  Getting started:
    1. mkdir behavior/agents/<your-agent>
    2. cp behavior/default/<runtime>/<type>/SOUL.md behavior/agents/<your-agent>/SOUL.md
       — e.g. behavior/default/openclaw/user/SOUL.md, or write your own from scratch
    3. Edit to define the agent's personality, boundaries, and context
    4. conga refresh --agent <your-agent>

  Per-agent overrides apply regardless of runtime — the agents/<name>/
  directory is not scoped by runtime or type.

  This example file is a reference, not a starting point. Clone the
  default and extend it, or write something purpose-built for your agent.
-->

## What goes here

SOUL.md defines who the agent is — personality, tone, boundaries, and
what it knows. The default version is intentionally generic. When you
override it for a specific agent, you can tailor the voice and rules
to a particular product, project, client, or team dynamic.

## Tips

- Start by copying `behavior/default/<runtime>/<type>/SOUL.md` so you
  inherit the baseline personality, then customize.
- The default SOUL.md already differs between user and team agents
  (privacy vs multi-user awareness). Your override replaces it entirely.
- Keep it under 150 lines. SOUL.md is the first file loaded into the
  system prompt.
- Use `conga agent show <name> SOUL.md` to inspect the deployed version.
