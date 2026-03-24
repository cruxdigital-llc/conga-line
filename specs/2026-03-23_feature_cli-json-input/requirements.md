# Requirements — CLI JSON Input

## Goal

Make every `conga` CLI command fully scriptable by LLMs and autonomous agents. Commands accept structured JSON input (replacing interactive prompts) and emit structured JSON output (replacing human-formatted text), so that an orchestrating agent can drive the CLI programmatically without screen-scraping or expect-style automation.

## Who

- **Primary**: LLMs and AI agents that invoke `conga` as a tool/subprocess
- **Secondary**: CI/CD pipelines, shell scripts, and power users composing conga with `jq`/pipes

## Success Criteria

1. Every command that currently prompts interactively can be driven entirely via `--json '{...}'` with zero TTY interaction.
2. Every command that produces output supports `--output json` (or a global `--json` that implies both input + output mode) and writes a well-formed JSON object to stdout.
3. In JSON output mode, human-oriented messages (spinners, "Next steps:", prompts) are suppressed or routed to stderr.
4. Errors in JSON mode are emitted as `{"error": "..."}` on stdout (exit code still non-zero).
5. Existing interactive behavior is 100% preserved when `--json` is not used — zero breaking changes.
6. The JSON schemas for input and output are documented (or self-describing) so an LLM can discover them.

## User Stories

### US-1: Agent runs full setup non-interactively
```
conga admin setup --json '{"image":"ghcr.io/openclaw/openclaw:2026.3.11","slack_bot_token":"xoxb-...","slack_signing_secret":"abc"}'
```
All prompts are skipped. Output is a JSON result object.

### US-2: Agent provisions a user and reads the result
```
conga admin add-user myagent U12345 --gateway-port 18790 --output json
```
Returns: `{"agent":"myagent","gateway_port":18790,"status":"provisioned"}`

### US-3: Agent checks status and parses it
```
conga status --agent myagent --output json
```
Returns: `{"agent":"myagent","container":"running","readiness":"ready","memory":"512MiB",...}`

### US-4: Agent sets a secret without TTY
```
conga secrets set anthropic-api-key --value sk-ant-... --agent myagent --output json
```
Returns: `{"secret":"anthropic-api-key","env_var":"ANTHROPIC_API_KEY","status":"saved"}`

### US-5: Agent lists agents and iterates
```
conga admin list-agents --output json
```
Returns: `[{"name":"myagent","type":"user","paused":false,"gateway_port":18790},...]`

### US-6: Agent handles errors programmatically
```
conga secrets delete nonexistent --agent myagent --force --output json
```
Exit code 1. Stdout: `{"error":"secret 'nonexistent' not found"}`

## Non-Goals

- Changing the Provider interface or any provider implementation logic
- Adding new commands
- Websocket/gRPC API — this is purely CLI flag-driven
- Interactive JSON editor or REPL mode

## Constraints

- Must work with existing `--config` flag on `admin setup` (extend, don't replace)
- JSON output must go to stdout; all human text in JSON mode goes to stderr (so `2>/dev/null` cleans it up)
- No new dependencies — use `encoding/json` from stdlib
- Confirmation prompts (`ui.Confirm`) are auto-accepted in JSON input mode (equivalent to `--force`)
- JSON input for a command only needs to cover the fields that would otherwise be prompted — flags and positional args still work alongside `--json`

## Architect Review Questions

- **Pattern consistency**: The `--json` flag as a persistent root flag mirrors the pattern used by `gh` (GitHub CLI), `aws` CLI, and `kubectl -o json`. Consistent with industry standard.
- **No new dependency**: Pure `encoding/json`. No schema generator libraries.
- **Existing `--config` on `admin setup`**: `--config` already accepts JSON for setup. The new `--json` flag will be a superset — it works on all commands, and for `admin setup` it subsumes `--config`. We preserve `--config` for backward compatibility.

## QA Edge Cases to Cover

- `--json` with malformed JSON → clear error, exit 1
- `--json` with unknown fields → ignore (forward compatibility) or warn on stderr
- `--json` with missing required fields → clear error listing what's missing
- `--json '{}'` (empty object) on a command that needs input → error, not silent no-op
- Mixing `--json` input with positional args (e.g. `conga admin add-user myagent --json '{"slack_member_id":"U123"}'`) → positional args take precedence
- `--output json` on commands with no meaningful output (e.g. `refresh`) → `{"status":"ok"}`
- Piping: `echo '{"name":"x","value":"y"}' | conga secrets set --json -` (stdin) → stretch goal, not MVP
