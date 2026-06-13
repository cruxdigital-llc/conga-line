package openclaw

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
)

// openclawDefaults is the embedded runtime baseline. As of feature #31 it is a
// FALLBACK: operators may ship an editable copy at
// agents/_defaults/openclaw/openclaw-defaults.json (resolved by
// common.ResolveRuntimeDefaults into ConfigParams.RuntimeDefaults). The embed is
// retained for first-boot / air-gap / tamper-safe operation — a missing or
// malformed on-disk file fails safe to this known-good baseline.
//
//go:embed openclaw-defaults.json
var openclawDefaults []byte

func (r *Runtime) GenerateConfig(params runtime.ConfigParams) ([]byte, error) {
	// Prefer the operator-editable on-disk defaults (feature #31 de-embed);
	// fall back to the embedded baseline if absent or not valid JSON.
	base := openclawDefaults
	if len(params.RuntimeDefaults) > 0 {
		if json.Valid(params.RuntimeDefaults) {
			base = params.RuntimeDefaults
		} else {
			fmt.Fprintln(os.Stderr, "warning: on-disk openclaw-defaults.json is not valid JSON; using embedded runtime defaults")
		}
	}

	var config map[string]any
	if err := json.Unmarshal(base, &config); err != nil {
		return nil, fmt.Errorf("failed to parse openclaw-defaults.json: %w", err)
	}

	config["gateway"] = buildGatewayConfig(ContainerPort, params.Agent.GatewayPort, params.GatewayToken)

	channelsCfg := map[string]any{}
	pluginsCfg := map[string]any{}

	for _, binding := range params.Agent.Channels {
		ch, ok := channels.Get(binding.Platform)
		if !ok {
			continue
		}
		hasCreds := ch.HasCredentials(params.Secrets.Values)
		pluginsCfg[binding.Platform] = ch.OpenClawPluginConfig(hasCreds)
		if hasCreds {
			section, err := ch.OpenClawChannelConfig(string(params.Agent.Type), binding, params.Secrets.Values)
			if err != nil {
				return nil, fmt.Errorf("channel %s config: %w", binding.Platform, err)
			}
			channelsCfg[binding.Platform] = section
		}
	}

	if len(channelsCfg) > 0 {
		config["channels"] = channelsCfg
	}
	if len(pluginsCfg) > 0 {
		config["plugins"] = map[string]any{"entries": pluginsCfg}
	}

	if params.Overlay != nil && params.Overlay.Model != nil {
		if err := applyModelOverlay(config, params.Overlay.Model); err != nil {
			return nil, fmt.Errorf("apply model overlay: %w", err)
		}
	}

	if params.Overlay != nil && params.Overlay.Subagents != nil {
		if err := applySubagentsOverlay(config, params.Overlay.Subagents); err != nil {
			return nil, fmt.Errorf("apply subagents overlay: %w", err)
		}
	}

	if params.Agent.Type == provider.AgentTypeTeam {
		applyTeamChannelDiscipline(config)
	}

	// Reference the layered custom-config files. OpenClaw deep-merges them under
	// this managed root in array order (later wins); the root wins over every
	// include (verified). Effective precedence: root > admin-drift > per-agent > fleet.
	// Providers guarantee all three exist (a missing $include target makes the
	// whole config invalid). See FleetCustomConfigFile / AgentManagedCustomConfigFile
	// / AgentCustomConfigFile.
	config["$include"] = []string{
		FleetCustomConfigFile,
		AgentManagedCustomConfigFile,
		AgentCustomConfigFile,
	}

	return json.MarshalIndent(config, "", "  ")
}

// applyTeamChannelDiscipline tightens group-chat delivery for team agents:
// only an explicit `message` tool call is forwarded to the channel; bare text
// content blocks (preamble narration, decision-not-to-reply prose, inter-tool
// commentary) stay internal.
//
// Workaround for openclaw/openclaw#25592 ("Text between tool calls leaks to
// messaging channels"), still open at v2026.5.26. The fix has two parts:
//   - messages.groupChat.visibleReplies = "message_tool" — gates delivery on
//     an explicit message() tool call.
//   - tools.alsoAllow = ["message"] — restores the `message` tool that the
//     "coding" profile (set in openclaw-defaults.json) strips out. Without
//     this the agent would have no way to deliver replies and every turn
//     would silently drop (the failure mode behind openclaw/openclaw#77320).
//
// Scope: team agents only. User agents operate in DMs where a touch of
// preamble is acceptable and the silent-drop risk is higher (a missed reply
// in a 1:1 DM is noticed immediately).
func applyTeamChannelDiscipline(config map[string]any) {
	messages, ok := config["messages"].(map[string]any)
	if !ok {
		messages = map[string]any{}
		config["messages"] = messages
	}
	groupChat, ok := messages["groupChat"].(map[string]any)
	if !ok {
		groupChat = map[string]any{}
		messages["groupChat"] = groupChat
	}
	groupChat["visibleReplies"] = "message_tool"

	tools, ok := config["tools"].(map[string]any)
	if !ok {
		tools = map[string]any{}
		config["tools"] = tools
	}
	existing, _ := tools["alsoAllow"].([]any)
	for _, e := range existing {
		if s, ok := e.(string); ok && s == "message" {
			return
		}
	}
	tools["alsoAllow"] = append(existing, "message")
}

// applyModelOverlay mutates config in place to reflect the operator's model
// choice. See spec § "Runtime config generator" for the JSON shape contract.
func applyModelOverlay(config map[string]any, m *runtime.ModelOverlay) error {
	modelRef := m.Provider + "/" + m.Name

	// agents.defaults.model.primary + fallbacks + models allowlist
	agents, ok := config["agents"].(map[string]any)
	if !ok {
		return fmt.Errorf("openclaw-defaults.json missing agents section")
	}
	defaults, ok := agents["defaults"].(map[string]any)
	if !ok {
		return fmt.Errorf("openclaw-defaults.json missing agents.defaults section")
	}
	// agents.defaults.model: set primary to the overlay's choice. Fallbacks stay
	// empty so OpenClaw won't auto-switch on errors — operators control model
	// selection explicitly via /model in chat.
	defaults["model"] = map[string]any{
		"primary":   modelRef,
		"fallbacks": []any{},
	}
	// agents.defaults.models: MERGE the overlay's model into the existing
	// allowlist rather than replacing it. This preserves the runtime defaults
	// (e.g. anthropic/claude-opus-4-8 from openclaw-defaults.json) so operators
	// can /model into them mid-conversation. Lockdown — if you want it — should
	// be enforced at the egress policy layer, not by trimming the allowlist.
	allowlist, ok := defaults["models"].(map[string]any)
	if !ok {
		allowlist = map[string]any{}
		defaults["models"] = allowlist
	}
	allowlist[modelRef] = map[string]any{}

	// models.providers.<id> — endpoint, auth marker, and model entries.
	// OpenClaw's schema validator requires `models` to be a non-empty array
	// when models.providers.<id> is set explicitly; auto-discovery is bypassed
	// (see docs/providers/ollama.md "explicit config" section).
	modelEntry := map[string]any{
		"id":   m.Name,
		"name": m.Name,
	}
	// Capability hints — only emitted when the operator sets them in
	// agent.yaml. Without these, OpenClaw's default for max_completion_tokens
	// can exceed what a self-hosted endpoint enforces (e.g. LiteLLM/vLLM
	// where max_model_len < the advertised contextWindow), producing 400s.
	if m.ContextWindow > 0 {
		modelEntry["contextWindow"] = m.ContextWindow
	}
	if m.MaxTokens > 0 {
		modelEntry["maxTokens"] = m.MaxTokens
	}
	providerModels := []any{modelEntry}
	providerCfg := map[string]any{"models": providerModels}
	switch m.Provider {
	case runtime.ProviderOllama:
		providerCfg["baseUrl"] = m.BaseURL
		providerCfg["apiKey"] = runtime.OllamaLocalAPIKey
		providerCfg["api"] = runtime.ProviderOllama
	case runtime.ProviderOpenAI:
		if m.BaseURL != "" {
			providerCfg["baseUrl"] = m.BaseURL
		}
		// apiKey for openai flows via the OPENAI_API_KEY env var (set from the
		// openai-api-key secret); never write the literal value into config —
		// see CLAUDE.md note on OpenClaw issue #9627.
	default:
		return fmt.Errorf("unsupported overlay provider %q (validation should have caught this)", m.Provider)
	}

	models, ok := config["models"].(map[string]any)
	if !ok {
		models = map[string]any{}
		config["models"] = models
	}
	providers, ok := models["providers"].(map[string]any)
	if !ok {
		providers = map[string]any{}
		models["providers"] = providers
	}
	providers[m.Provider] = providerCfg

	return nil
}

// applySubagentsOverlay mutates config in place to emit OpenClaw's native
// subagent configuration under agents.defaults.subagents, extending the
// models allowlist + models.providers map to include the subagent model.
//
// Upstream mechanism: OpenClaw v2026.5.18+ recognizes
// agents.defaults.subagents.{model, delegationMode, maxConcurrent} and pairs
// it with the sessions_spawn tool — see docs/tools/subagents.md and
// docs/concepts/parallel-specialist-lanes.md in github.com/openclaw/openclaw.
//
// Per-block precondition: AgentOverlay.Validate has confirmed that the
// subagent does NOT conflict with the primary (same provider id with
// different base_urls is rejected upstream of this call). The defensive
// re-check below is for programmatic AgentOverlay use that skips Validate.
func applySubagentsOverlay(config map[string]any, s *runtime.SubagentsOverlay) error {
	m := s.Model
	modelRef := m.Provider + "/" + m.Name

	agents, ok := config["agents"].(map[string]any)
	if !ok {
		return fmt.Errorf("openclaw-defaults.json missing agents section")
	}
	defaults, ok := agents["defaults"].(map[string]any)
	if !ok {
		return fmt.Errorf("openclaw-defaults.json missing agents.defaults section")
	}

	// agents.defaults.subagents — the upstream config block.
	// max_spawn_depth is intentionally NOT emitted here (Hermes-only knob).
	subBlock := map[string]any{
		"model": modelRef,
	}
	if s.DelegationMode != "" {
		subBlock["delegationMode"] = s.DelegationMode
	}
	if s.MaxConcurrent > 0 {
		subBlock["maxConcurrent"] = s.MaxConcurrent
	}
	defaults["subagents"] = subBlock

	// Merge the subagent model into the existing allowlist so the orchestrator
	// can also /model into it manually. Preserves the additive-allowlist
	// principle established by Feature #27.
	allowlist, ok := defaults["models"].(map[string]any)
	if !ok {
		allowlist = map[string]any{}
		defaults["models"] = allowlist
	}
	if _, present := allowlist[modelRef]; !present {
		allowlist[modelRef] = map[string]any{}
	}

	// Extend models.providers.<id>: either append to an existing provider
	// entry (when primary uses the same provider) or create one from scratch.
	models, ok := config["models"].(map[string]any)
	if !ok {
		models = map[string]any{}
		config["models"] = models
	}
	providers, ok := models["providers"].(map[string]any)
	if !ok {
		providers = map[string]any{}
		models["providers"] = providers
	}

	modelEntry := map[string]any{
		"id":   m.Name,
		"name": m.Name,
	}
	if m.ContextWindow > 0 {
		modelEntry["contextWindow"] = m.ContextWindow
	}
	if m.MaxTokens > 0 {
		modelEntry["maxTokens"] = m.MaxTokens
	}

	if existing, providerExists := providers[m.Provider].(map[string]any); providerExists {
		// Defensive: same provider key but different base_url is a conflict
		// the validation layer should already have rejected. This guards
		// against programmatic AgentOverlay construction that skips Validate.
		existingBaseURL, _ := existing["baseUrl"].(string)
		if trimSubagentBaseURL(existingBaseURL) != trimSubagentBaseURL(m.BaseURL) {
			return fmt.Errorf("subagent base_url %q conflicts with primary's %q for the same provider key %q; each provider id must map to one endpoint",
				m.BaseURL, existingBaseURL, m.Provider)
		}
		existingModels, _ := existing["models"].([]any)
		for _, em := range existingModels {
			emMap, ok := em.(map[string]any)
			if !ok {
				continue
			}
			if emMap["id"] == m.Name {
				// Already present (e.g. operator set primary and subagent to
				// the same model name — odd but not invalid).
				return nil
			}
		}
		existing["models"] = append(existingModels, modelEntry)
		return nil
	}

	// Provider not yet configured — create the entry from scratch.
	providerCfg := map[string]any{"models": []any{modelEntry}}
	switch m.Provider {
	case runtime.ProviderOllama:
		providerCfg["baseUrl"] = m.BaseURL
		providerCfg["apiKey"] = runtime.OllamaLocalAPIKey
		providerCfg["api"] = runtime.ProviderOllama
	case runtime.ProviderOpenAI:
		if m.BaseURL != "" {
			providerCfg["baseUrl"] = m.BaseURL
		}
		// apiKey flows via OPENAI_API_KEY env var — see CLAUDE.md note on
		// OpenClaw issue #9627.
	default:
		return fmt.Errorf("unsupported overlay provider %q (validation should have caught this)", m.Provider)
	}
	providers[m.Provider] = providerCfg

	return nil
}

func trimSubagentBaseURL(s string) string {
	return strings.TrimRight(s, "/")
}

func (r *Runtime) ConfigFileName() string { return "openclaw.json" }

// CustomConfigFileName returns the admin-owned include file referenced from the
// managed openclaw.json via "$include". Providers ensure it exists ("{}") on
// every config write; a missing $include target invalidates the whole config.
func (r *Runtime) CustomConfigFileName() string { return AgentCustomConfigFile }

// ManagedCustomConfigFiles returns the Conga-deployed declarative layers
// (feature #31): fleet-custom.json (all agents) and agent-managed-custom.json
// (per-agent). Conga re-syncs these from committed sources on every write, so
// they are hash-verified against their deployed baseline in addition to the
// reserved-key guard.
func (r *Runtime) ManagedCustomConfigFiles() []string {
	return []string{FleetCustomConfigFile, AgentManagedCustomConfigFile}
}

// buildGatewayConfig produces the gateway section of openclaw.json.
func buildGatewayConfig(containerPort, hostPort int, token string) map[string]any {
	origins := []string{
		fmt.Sprintf("http://localhost:%d", containerPort),
		fmt.Sprintf("http://127.0.0.1:%d", containerPort),
	}
	if hostPort != containerPort {
		origins = append(origins,
			fmt.Sprintf("http://localhost:%d", hostPort),
			fmt.Sprintf("http://127.0.0.1:%d", hostPort),
		)
	}

	gw := map[string]any{
		"port": containerPort,
		// Gateway and agent runtime live in the same container, so mode is "local".
		// 0.0.0.0 binding (required for Docker -p port forwarding) comes from
		// bind="lan", not from mode. OpenClaw v2026.3.22+ refuses to start with
		// mode="remote" unless --allow-unconfigured is passed.
		"mode": "local",
		"bind": "lan",
		"controlUi": map[string]any{
			"allowedOrigins": origins,
		},
	}

	if token != "" {
		gw["auth"] = map[string]any{
			"mode":  "token",
			"token": token,
		}
	}

	return gw
}
