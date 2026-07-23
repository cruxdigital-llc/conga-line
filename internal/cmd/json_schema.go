package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/spf13/cobra"
)

// FieldSchema describes a single JSON field.
type FieldSchema struct {
	Type        string   `json:"type"`
	Required    bool     `json:"required,omitempty"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

// SchemaSection describes the input or output fields for a command.
type SchemaSection struct {
	IsArray bool                   `json:"is_array,omitempty"` // true when output is an array of objects
	Fields  map[string]FieldSchema `json:"fields,omitempty"`
}

// CommandSchema describes the JSON input/output contract for a command.
type CommandSchema struct {
	Command     string         `json:"command"`
	Description string         `json:"description"`
	Input       *SchemaSection `json:"input,omitempty"`
	Output      *SchemaSection `json:"output"`
}

var schemaAll bool

func init() {
	schemaCmd := &cobra.Command{
		Use:   "json-schema [command]",
		Short: "Print JSON input/output schema for a command",
		Long: `Print the JSON schema for a command's --json input and --output json response.

The --json flag accepts inline JSON or a file reference (@file.json):
  conga admin setup --json '{"runtime":"openclaw","image":"ghcr.io/openclaw/openclaw:2026.5.26"}'
  conga admin setup --json @setup.json

Examples:
  conga json-schema status
  conga json-schema admin.setup
  conga json-schema secrets.set
  conga json-schema --all`,
		Args: cobra.MaximumNArgs(1),
		RunE: jsonSchemaRun,
	}
	schemaCmd.Flags().BoolVar(&schemaAll, "all", false, "Print schemas for all commands")
	rootCmd.AddCommand(schemaCmd)
}

func jsonSchemaRun(cmd *cobra.Command, args []string) error {
	if schemaAll {
		keys := make([]string, 0, len(commandSchemas))
		for k := range commandSchemas {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		schemas := make([]CommandSchema, 0, len(keys))
		for _, k := range keys {
			schemas = append(schemas, commandSchemas[k])
		}
		data, err := json.MarshalIndent(schemas, "", "  ")
		if err != nil {
			return err
		}
		os.Stdout.Write(data)
		fmt.Println()
		return nil
	}

	if len(args) == 0 {
		fmt.Println("Available commands:")
		keys := make([]string, 0, len(commandSchemas))
		for k := range commandSchemas {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("  %-24s %s\n", k, commandSchemas[k].Description)
		}
		fmt.Println("\nUse: conga json-schema <command>")
		fmt.Println("  or: conga json-schema --all")
		return nil
	}

	schema, ok := commandSchemas[args[0]]
	if !ok {
		return fmt.Errorf("unknown command %q; run 'conga json-schema' to list available commands", args[0])
	}

	data, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return err
	}
	os.Stdout.Write(data)
	fmt.Println()
	return nil
}

var commandSchemas = map[string]CommandSchema{
	"version": {
		Command:     "version",
		Description: "Print version information",
		Output: &SchemaSection{Fields: map[string]FieldSchema{
			"version": {Type: "string", Description: "Version string"},
			"commit":  {Type: "string", Description: "Git commit hash"},
			"date":    {Type: "string", Description: "Build date"},
		}},
	},
	"auth.login": {
		Command:     "auth login",
		Description: "Show authentication instructions",
		Output: &SchemaSection{Fields: map[string]FieldSchema{
			"provider": {Type: "string", Description: "Active provider name"},
			"command":  {Type: "string", Description: "SSO login command to run"},
			"message":  {Type: "string", Description: "Human-readable message"},
		}},
	},
	"auth.status": {
		Command:     "auth status",
		Description: "Show current identity and agent mapping",
		Output: &SchemaSection{Fields: map[string]FieldSchema{
			"identity":   {Type: "string", Description: "Current identity (ARN or username)"},
			"account_id": {Type: "string", Description: "AWS account ID (if applicable)"},
			"provider":   {Type: "string", Description: "Active provider name"},
			"agent":      {Type: "string", Description: "Mapped agent name"},
		}},
	},
	"status": {
		Command:     "status",
		Description: "Show container status for an agent",
		Output: &SchemaSection{Fields: map[string]FieldSchema{
			"agent":         {Type: "string", Description: "Agent name"},
			"container":     {Type: "string", Description: "Container state: running, stopped, not found"},
			"service":       {Type: "string", Description: "Service state"},
			"readiness":     {Type: "string", Description: "Ready phase: starting, gateway up, slack loading, ready"},
			"paused":        {Type: "boolean", Description: "Whether the agent is paused"},
			"started_at":    {Type: "string", Description: "Container start time (ISO 8601)"},
			"uptime":        {Type: "string", Description: "Human-readable uptime"},
			"restart_count": {Type: "integer", Description: "Number of container restarts"},
			"cpu":           {Type: "string", Description: "CPU usage percentage"},
			"memory":        {Type: "string", Description: "Memory usage"},
			"pids":          {Type: "integer", Description: "Number of processes"},
		}},
	},
	"logs": {
		Command:     "logs",
		Description: "Get container logs",
		Output: &SchemaSection{Fields: map[string]FieldSchema{
			"agent": {Type: "string", Description: "Agent name"},
			"lines": {Type: "array", Description: "Array of log line strings"},
		}},
	},
	"refresh": {
		Command:     "refresh",
		Description: "Restart container with fresh secrets",
		Output: &SchemaSection{Fields: map[string]FieldSchema{
			"agent":  {Type: "string", Description: "Agent name"},
			"status": {Type: "string", Description: "Result status", Enum: []string{"refreshed"}},
		}},
	},
	"mcp.login": {
		Command:     "mcp login",
		Description: "Authenticate a remote-MCP OAuth server for an agent (two-leg flow)",
		Input: &SchemaSection{Fields: map[string]FieldSchema{
			"server": {Type: "string", Description: "MCP server name; optional if the agent has exactly one OAuth server"},
			"code":   {Type: "string", Description: "Authorization code from the browser redirect; omit on the first call, provide to complete"},
		}},
		Output: &SchemaSection{Fields: map[string]FieldSchema{
			"agent":         {Type: "string", Description: "Agent name"},
			"server":        {Type: "string", Description: "MCP server name"},
			"status":        {Type: "string", Description: "Result status", Enum: []string{"authorization_pending", "authenticated"}},
			"authorize_url": {Type: "string", Description: "URL to open in a browser (present when status is authorization_pending)"},
			"next":          {Type: "string", Description: "The follow-up command to complete login (present when status is authorization_pending)"},
		}},
	},
	"doctor": {
		Command:     "doctor",
		Description: "Scan the fleet for remote-MCP servers needing OAuth re-authentication",
		Output: &SchemaSection{Fields: map[string]FieldSchema{
			"healthy":              {Type: "boolean", Description: "True when no agent needs OAuth re-authentication"},
			"agents_needing_oauth": {Type: "integer", Description: "Count of agents with at least one server needing OAuth"},
			"findings":             {Type: "array", Description: "Per-agent findings: {agent, servers_needing_oauth:[{server,last_seen}], fixes:[string], error}"},
		}},
	},
	"connect": {
		Command:     "connect",
		Description: "Get web UI connection info (exits immediately in JSON mode)",
		Output: &SchemaSection{Fields: map[string]FieldSchema{
			"agent": {Type: "string", Description: "Agent name"},
			"url":   {Type: "string", Description: "Web UI URL"},
			"port":  {Type: "integer", Description: "Local port"},
			"token": {Type: "string", Description: "Auth token (if applicable)"},
		}},
	},
	"secrets.set": {
		Command:     "secrets set",
		Description: "Create or update a secret",
		Input: &SchemaSection{Fields: map[string]FieldSchema{
			"name":  {Type: "string", Required: true, Description: "Secret name (e.g. anthropic-api-key)"},
			"value": {Type: "string", Required: true, Description: "Secret value"},
		}},
		Output: &SchemaSection{Fields: map[string]FieldSchema{
			"secret":  {Type: "string", Description: "Secret name"},
			"env_var": {Type: "string", Description: "Environment variable name (SCREAMING_SNAKE_CASE)"},
			"status":  {Type: "string", Description: "Result status", Enum: []string{"saved"}},
		}},
	},
	"secrets.list": {
		Command:     "secrets list",
		Description: "List secrets for an agent",
		Output: &SchemaSection{IsArray: true, Fields: map[string]FieldSchema{
			"name":         {Type: "string", Description: "Secret name"},
			"env_var":      {Type: "string", Description: "Environment variable name"},
			"last_changed": {Type: "string", Description: "Last modified time (ISO 8601)"},
		}},
	},
	"secrets.delete": {
		Command:     "secrets delete",
		Description: "Delete a secret (auto-confirms in JSON mode)",
		Output: &SchemaSection{Fields: map[string]FieldSchema{
			"secret": {Type: "string", Description: "Secret name"},
			"status": {Type: "string", Description: "Result status", Enum: []string{"deleted"}},
		}},
	},
	"admin.setup": {
		Command:     "admin setup",
		Description: "Configure shared secrets and settings",
		Input: &SchemaSection{Fields: map[string]FieldSchema{
			"ssh_host":       {Type: "string", Description: "SSH host — remote provider only"},
			"ssh_port":       {Type: "integer", Description: "SSH port — remote provider only (default: 22)"},
			"ssh_user":       {Type: "string", Description: "SSH user — remote provider only (default: root)"},
			"ssh_key_path":   {Type: "string", Description: "SSH key path — remote provider only (auto-detect if omitted)"},
			"repo_path":      {Type: "string", Description: "Conga Line repo root — local and remote providers"},
			"image":          {Type: "string", Description: "Docker image to deploy — all providers"},
			"secrets":        {Type: "object", Description: "Shared secrets map (keys: slack-bot-token, slack-signing-secret, slack-app-token, google-client-id, google-client-secret)"},
			"install_docker": {Type: "boolean", Description: "Auto-install Docker — remote provider only"},
		}},
		Output: &SchemaSection{Fields: map[string]FieldSchema{
			"provider": {Type: "string", Description: "Active provider name"},
			"status":   {Type: "string", Description: "Result status", Enum: []string{"configured"}},
		}},
	},
	"admin.add-user": {
		Command:     "admin add-user <name>",
		Description: "Provision a user agent (name is a positional arg)",
		Input: &SchemaSection{Fields: map[string]FieldSchema{
			"channel":      {Type: "string", Description: "Channel binding (platform:id, e.g., slack:U0123456789)"},
			"gateway_port": {Type: "integer", Description: "Gateway port (auto-assigned if omitted)"},
			"iam_identity": {Type: "string", Description: "IAM identity / SSO username (AWS provider only)"},
			"role":         {Type: "string", Description: "Role package slug (e.g. role-ops, role-research). Copies overlay defaults from agents/_defaults/<runtime>/role-<slug>/. role.meta must declare type: user."},
		}},
		Output: &SchemaSection{Fields: map[string]FieldSchema{
			"agent":        {Type: "string", Description: "Agent name"},
			"type":         {Type: "string", Description: "Agent type", Enum: []string{"user"}},
			"gateway_port": {Type: "integer", Description: "Assigned gateway port"},
			"status":       {Type: "string", Description: "Result status", Enum: []string{"provisioned"}},
		}},
	},
	"admin.add-team": {
		Command:     "admin add-team <name>",
		Description: "Provision a team agent (name is a positional arg)",
		Input: &SchemaSection{Fields: map[string]FieldSchema{
			"channel":      {Type: "string", Description: "Channel binding (platform:id, e.g., slack:C0123456789)"},
			"gateway_port": {Type: "integer", Description: "Gateway port (auto-assigned if omitted)"},
			"role":         {Type: "string", Description: "Role package slug (e.g. role-code-dev, role-writing). Copies overlay defaults from agents/_defaults/<runtime>/role-<slug>/. role.meta must declare type: team."},
		}},
		Output: &SchemaSection{Fields: map[string]FieldSchema{
			"agent":        {Type: "string", Description: "Agent name"},
			"type":         {Type: "string", Description: "Agent type", Enum: []string{"team"}},
			"gateway_port": {Type: "integer", Description: "Assigned gateway port"},
			"status":       {Type: "string", Description: "Result status", Enum: []string{"provisioned"}},
		}},
	},
	"admin.list-agents": {
		Command:     "admin list-agents",
		Description: "List all provisioned agents",
		Output: &SchemaSection{IsArray: true, Fields: map[string]FieldSchema{
			"name":         {Type: "string", Description: "Agent name"},
			"type":         {Type: "string", Description: "Agent type: user, team"},
			"channels":     {Type: "array", Description: "Channel bindings [{platform, id, label}]"},
			"gateway_port": {Type: "integer", Description: "Gateway port"},
			"paused":       {Type: "boolean", Description: "Whether the agent is paused"},
		}},
	},
	"admin.remove-agent": {
		Command:     "admin remove-agent <name>",
		Description: "Remove an agent (auto-confirms in JSON mode)",
		Output: &SchemaSection{Fields: map[string]FieldSchema{
			"agent":  {Type: "string", Description: "Agent name"},
			"status": {Type: "string", Description: "Result status", Enum: []string{"removed"}},
		}},
	},
	"admin.pause": {
		Command:     "admin pause <name>",
		Description: "Pause an agent",
		Output: &SchemaSection{Fields: map[string]FieldSchema{
			"agent":  {Type: "string", Description: "Agent name"},
			"status": {Type: "string", Description: "Result status", Enum: []string{"paused"}},
		}},
	},
	"admin.unpause": {
		Command:     "admin unpause <name>",
		Description: "Unpause an agent",
		Output: &SchemaSection{Fields: map[string]FieldSchema{
			"agent":  {Type: "string", Description: "Agent name"},
			"status": {Type: "string", Description: "Result status", Enum: []string{"unpaused"}},
		}},
	},
	"agent.rebaseline": {
		Command:     "agent rebaseline <name>",
		Description: "Reset an agent's agent-custom.json to the generated baseline (backs it up, empties it, refreshes). Discards admin customization; preserves agent data.",
		Output: &SchemaSection{Fields: map[string]FieldSchema{
			"agent":  {Type: "string", Description: "Agent name"},
			"status": {Type: "string", Description: "Result status", Enum: []string{"rebaselined"}},
		}},
	},
	"agent.show-config": {
		Command:     "agent show-config <name>",
		Description: "Show the agent's effective OpenClaw config as its precedence-ordered $include layers (root > admin-drift > per-agent > fleet), read live from the container.",
		Output: &SchemaSection{Fields: map[string]FieldSchema{
			"agent":   {Type: "string", Description: "Agent name"},
			"runtime": {Type: "string", Description: "Runtime name (e.g. openclaw)"},
			"layers":  {Type: "array", Description: "Config layers, highest precedence first. Each: file (string), role (string), owner (string), precedence (integer, 1=highest), present (boolean), content (object, omitted if absent or not strict JSON)"},
		}},
	},
	"admin.cycle-host": {
		Command:     "admin cycle-host",
		Description: "Restart the deployment environment (auto-confirms in JSON mode)",
		Output: &SchemaSection{Fields: map[string]FieldSchema{
			"status": {Type: "string", Description: "Result status", Enum: []string{"ok"}},
		}},
	},
	"admin.refresh-all": {
		Command:     "admin refresh-all",
		Description: "Restart all agent containers (auto-confirms in JSON mode)",
		Output: &SchemaSection{Fields: map[string]FieldSchema{
			"agents_refreshed": {Type: "integer", Description: "Number of agents refreshed"},
			"status":           {Type: "string", Description: "Result status", Enum: []string{"ok"}},
		}},
	},
	"admin.teardown": {
		Command:     "admin teardown",
		Description: "Remove entire deployment (auto-confirms in JSON mode)",
		Output: &SchemaSection{Fields: map[string]FieldSchema{
			"status": {Type: "string", Description: "Result status", Enum: []string{"ok"}},
		}},
	},
}
