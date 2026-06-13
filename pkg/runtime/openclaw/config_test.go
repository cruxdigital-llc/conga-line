package openclaw

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"

	// Register the slack channel so OpenClawChannelConfig resolves.
	_ "github.com/cruxdigital-llc/conga-line/pkg/channels/slack"
)

func decodeJSON(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
}

// baseParams returns a minimal ConfigParams that exercises the gateway path
// without any channels or overlay. Used as the "no overlay" regression baseline.
func baseParams() runtime.ConfigParams {
	return runtime.ConfigParams{
		Agent: provider.AgentConfig{
			Name:        "test",
			Type:        provider.AgentTypeUser,
			GatewayPort: 18789,
		},
		Secrets:      provider.SharedSecrets{Values: map[string]string{}},
		GatewayToken: "fixed-token-for-tests",
	}
}

func TestGenerateConfig_NoOverlay_PreservesDefaults(t *testing.T) {
	r := &Runtime{}
	out, err := r.GenerateConfig(baseParams())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	cfg := decodeJSON(t, out)
	agents, ok := cfg["agents"].(map[string]any)
	if !ok {
		t.Fatalf("missing agents section")
	}
	defaults, ok := agents["defaults"].(map[string]any)
	if !ok {
		t.Fatalf("missing agents.defaults section")
	}

	// model.primary unchanged from openclaw-defaults.json
	model, ok := defaults["model"].(map[string]any)
	if !ok {
		t.Fatalf("missing agents.defaults.model")
	}
	if got := model["primary"]; got != "anthropic/claude-opus-4-8" {
		t.Fatalf("want anthropic/claude-opus-4-8, got %v", got)
	}

	// models allowlist unchanged
	models, ok := defaults["models"].(map[string]any)
	if !ok {
		t.Fatalf("missing agents.defaults.models")
	}
	if _, ok := models["anthropic/claude-opus-4-8"]; !ok {
		t.Fatalf("anthropic entry missing from allowlist: %+v", models)
	}

	// No models.providers block should be present without an overlay
	if _, ok := cfg["models"]; ok {
		t.Fatalf("models top-level key should not be set without overlay; got %+v", cfg["models"])
	}

	// update.checkOnStart must be false so the agent doesn't reach out to
	// registry.npmjs.org on every restart. Even in egress validate mode
	// the fetch ties up Envoy workers and pollutes the proxy log; in
	// enforce mode it would 403 the request and time out. We pin a
	// specific image tag so the update hints are noise anyway.
	update, ok := cfg["update"].(map[string]any)
	if !ok {
		t.Fatalf("missing top-level update block — every agent will hit npm on restart")
	}
	if got := update["checkOnStart"]; got != false {
		t.Fatalf("update.checkOnStart: want false, got %v", got)
	}
	auto, ok := update["auto"].(map[string]any)
	if !ok {
		t.Fatalf("missing update.auto block")
	}
	if got := auto["enabled"]; got != false {
		t.Fatalf("update.auto.enabled: want false (no background auto-update from inside agents), got %v", got)
	}
}

func TestGenerateConfig_OllamaOverlay(t *testing.T) {
	params := baseParams()
	params.Overlay = &runtime.AgentOverlay{
		Version: 1,
		Model: &runtime.ModelOverlay{
			Provider: runtime.ProviderOllama,
			Name:     "qwen3:6b",
			BaseURL:  "http://192.168.181.97:11434",
		},
	}

	r := &Runtime{}
	out, err := r.GenerateConfig(params)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	cfg := decodeJSON(t, out)

	defaults := cfg["agents"].(map[string]any)["defaults"].(map[string]any)
	model := defaults["model"].(map[string]any)

	if got := model["primary"]; got != "ollama/qwen3:6b" {
		t.Fatalf("primary: want ollama/qwen3:6b, got %v", got)
	}
	fallbacks, ok := model["fallbacks"].([]any)
	if !ok || len(fallbacks) != 0 {
		t.Fatalf("fallbacks: want [], got %+v", model["fallbacks"])
	}

	allow := defaults["models"].(map[string]any)
	if _, ok := allow["ollama/qwen3:6b"]; !ok {
		t.Fatalf("allowlist missing ollama/qwen3:6b: %+v", allow)
	}
	// The runtime default (anthropic/claude-opus-4-8 from openclaw-defaults.json)
	// must be preserved so operators can /model into it mid-conversation.
	if _, ok := allow["anthropic/claude-opus-4-8"]; !ok {
		t.Fatalf("allowlist should preserve anthropic default for /model switching: %+v", allow)
	}

	providers := cfg["models"].(map[string]any)["providers"].(map[string]any)
	ollama, ok := providers["ollama"].(map[string]any)
	if !ok {
		t.Fatalf("missing models.providers.ollama: %+v", providers)
	}
	want := map[string]any{
		"baseUrl": "http://192.168.181.97:11434",
		"apiKey":  "ollama-local",
		"api":     "ollama",
		"models": []any{
			map[string]any{"id": "qwen3:6b", "name": "qwen3:6b"},
		},
	}
	if !reflect.DeepEqual(ollama, want) {
		t.Fatalf("ollama provider: want %+v, got %+v", want, ollama)
	}

	// Workspace, heartbeat, etc. should be preserved from openclaw-defaults.json.
	if _, ok := defaults["workspace"]; !ok {
		t.Fatalf("defaults.workspace should be preserved from openclaw-defaults.json")
	}
	if _, ok := defaults["heartbeat"]; !ok {
		t.Fatalf("defaults.heartbeat should be preserved")
	}
}

func TestGenerateConfig_OpenAIOverlay(t *testing.T) {
	params := baseParams()
	params.Overlay = &runtime.AgentOverlay{
		Version: 1,
		Model: &runtime.ModelOverlay{
			Provider: runtime.ProviderOpenAI,
			Name:     "qwen-2.5-72b-instruct",
			BaseURL:  "http://10.0.0.5:8000/v1",
		},
	}

	r := &Runtime{}
	out, err := r.GenerateConfig(params)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	cfg := decodeJSON(t, out)

	defaults := cfg["agents"].(map[string]any)["defaults"].(map[string]any)
	if got := defaults["model"].(map[string]any)["primary"]; got != "openai/qwen-2.5-72b-instruct" {
		t.Fatalf("primary: want openai/qwen-2.5-72b-instruct, got %v", got)
	}

	openai := cfg["models"].(map[string]any)["providers"].(map[string]any)["openai"].(map[string]any)
	if got := openai["baseUrl"]; got != "http://10.0.0.5:8000/v1" {
		t.Fatalf("baseUrl: want http://10.0.0.5:8000/v1, got %v", got)
	}
	// apiKey must NOT be in config (flows via OPENAI_API_KEY env var — see CLAUDE.md note on OpenClaw issue #9627).
	if v, ok := openai["apiKey"]; ok {
		t.Fatalf("apiKey should NOT be written to config for openai provider, got %v", v)
	}
	if got, ok := openai["api"]; ok {
		t.Fatalf("api key is ollama-only; openai should omit it, got %v", got)
	}
	// models array required by OpenClaw schema when models.providers.<id> is set explicitly.
	wantModels := []any{
		map[string]any{"id": "qwen-2.5-72b-instruct", "name": "qwen-2.5-72b-instruct"},
	}
	if !reflect.DeepEqual(openai["models"], wantModels) {
		t.Fatalf("openai.models: want %+v, got %+v", wantModels, openai["models"])
	}
}

func TestPluginInstallCommand_RejectsLegacyYesFlag(t *testing.T) {
	// Regression: OpenClaw v2026.5.18+ rejects "--yes" as an unrecognized
	// option and exits non-zero before doing any work, which made the
	// systemd ExecStartPre and the docker-run-based plugin install
	// silently fail across all 3 providers. Keep this guard tight; if a
	// future flag needs to be added, ensure it actually exists in the
	// `openclaw plugins install --help` output for the pinned image.
	r := &Runtime{}
	got := r.PluginInstallCommand("@openclaw/slack")
	for _, arg := range got {
		if arg == "--yes" || arg == "-y" {
			t.Fatalf("install command must not include --yes (or -y); v2026.5.x rejects it. got=%v", got)
		}
	}
	want := []string{"openclaw", "plugins", "install", "@openclaw/slack"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("install command shape changed unexpectedly\nwant: %v\ngot:  %v", want, got)
	}
}

func TestPluginsToInstall_SlackCanonical(t *testing.T) {
	// Slack channel binding should produce exactly one plugin to install,
	// matching the canonical name. Hand-edited JSON with whitespace or
	// case variants should still trigger the install (defensive normalization).
	r := &Runtime{}
	for _, platform := range []string{"slack", "Slack", " slack ", "SLACK"} {
		got := r.PluginsToInstall(provider.AgentConfig{
			Channels: []channels.ChannelBinding{{Platform: platform, ID: "C123"}},
		})
		want := []string{"@openclaw/slack"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("PluginsToInstall(%q): want %v, got %v", platform, want, got)
		}
	}
}

func TestGenerateConfig_OverlayCapabilityCaps(t *testing.T) {
	// Overlay sets context_window + max_tokens — both must flow into
	// models.providers.<id>.models[0] as the OpenClaw-shaped keys.
	// Without these, OpenClaw's default for max_completion_tokens can
	// exceed what a self-hosted endpoint (LiteLLM/vLLM) enforces.
	params := baseParams()
	params.Overlay = &runtime.AgentOverlay{
		Version: 1,
		Model: &runtime.ModelOverlay{
			Provider:      runtime.ProviderOpenAI,
			Name:          "qwen36",
			BaseURL:       "http://192.168.181.97:4000/v1",
			ContextWindow: 131072,
			MaxTokens:     8192,
		},
	}

	r := &Runtime{}
	out, err := r.GenerateConfig(params)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	cfg := decodeJSON(t, out)

	openai := cfg["models"].(map[string]any)["providers"].(map[string]any)["openai"].(map[string]any)
	// JSON decode promotes numbers to float64 in map[string]any.
	wantModels := []any{
		map[string]any{
			"id":            "qwen36",
			"name":          "qwen36",
			"contextWindow": float64(131072),
			"maxTokens":     float64(8192),
		},
	}
	if !reflect.DeepEqual(openai["models"], wantModels) {
		t.Fatalf("openai.models with caps: want %+v, got %+v", wantModels, openai["models"])
	}
}

func TestGenerateConfig_OverlayCapabilityCaps_Omitted(t *testing.T) {
	// Caps unset — the model entry must NOT contain the cap keys (no zero
	// values, no nulls). Lets OpenClaw fall back to its own discovery.
	params := baseParams()
	params.Overlay = &runtime.AgentOverlay{
		Version: 1,
		Model: &runtime.ModelOverlay{
			Provider: runtime.ProviderOpenAI,
			Name:     "qwen36",
			BaseURL:  "http://10.0.0.5:8000/v1",
		},
	}

	r := &Runtime{}
	out, err := r.GenerateConfig(params)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	cfg := decodeJSON(t, out)

	openai := cfg["models"].(map[string]any)["providers"].(map[string]any)["openai"].(map[string]any)
	models, ok := openai["models"].([]any)
	if !ok || len(models) != 1 {
		t.Fatalf("openai.models: want one entry, got %+v", openai["models"])
	}
	entry := models[0].(map[string]any)
	if _, ok := entry["contextWindow"]; ok {
		t.Fatalf("contextWindow should be omitted when overlay doesn't set it, got %+v", entry)
	}
	if _, ok := entry["maxTokens"]; ok {
		t.Fatalf("maxTokens should be omitted when overlay doesn't set it, got %+v", entry)
	}
}

func TestGenerateConfig_OpenAIOverlay_HostedDefault(t *testing.T) {
	// openai provider with no base_url = hosted OpenAI; no baseUrl emitted.
	params := baseParams()
	params.Overlay = &runtime.AgentOverlay{
		Version: 1,
		Model: &runtime.ModelOverlay{
			Provider: runtime.ProviderOpenAI,
			Name:     "gpt-5.5",
		},
	}

	r := &Runtime{}
	out, err := r.GenerateConfig(params)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	cfg := decodeJSON(t, out)

	openai := cfg["models"].(map[string]any)["providers"].(map[string]any)["openai"].(map[string]any)
	if _, ok := openai["baseUrl"]; ok {
		t.Fatalf("baseUrl should be omitted for hosted OpenAI (no base_url in overlay), got %+v", openai)
	}
	// Even with hosted OpenAI, the models array is required when the provider
	// block is set explicitly.
	wantModels := []any{
		map[string]any{"id": "gpt-5.5", "name": "gpt-5.5"},
	}
	if !reflect.DeepEqual(openai["models"], wantModels) {
		t.Fatalf("openai.models: want %+v, got %+v", wantModels, openai["models"])
	}
}

func TestGenerateConfig_OverlayAndChannelsCoexist(t *testing.T) {
	// Overlay should not interfere with channel/plugin generation.
	params := baseParams()
	params.Agent.Channels = []channels.ChannelBinding{
		{Platform: "slack", ID: "UA13HEGTS"},
	}
	params.Secrets.Values = map[string]string{
		"slack-bot-token":      "xoxb-fake",
		"slack-signing-secret": "fake-secret",
	}
	params.Overlay = &runtime.AgentOverlay{
		Version: 1,
		Model: &runtime.ModelOverlay{
			Provider: runtime.ProviderOllama,
			Name:     "qwen3:6b",
			BaseURL:  "http://192.168.181.97:11434",
		},
	}

	r := &Runtime{}
	out, err := r.GenerateConfig(params)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	cfg := decodeJSON(t, out)

	// Channels block present
	if _, ok := cfg["channels"]; !ok {
		t.Fatalf("channels section missing despite slack binding: %+v", cfg)
	}
	// Plugins block present
	if _, ok := cfg["plugins"]; !ok {
		t.Fatalf("plugins section missing despite slack binding: %+v", cfg)
	}
	// Overlay still applied
	defaults := cfg["agents"].(map[string]any)["defaults"].(map[string]any)
	if got := defaults["model"].(map[string]any)["primary"]; got != "ollama/qwen3:6b" {
		t.Fatalf("overlay primary not applied: %v", got)
	}
	if _, ok := cfg["models"].(map[string]any)["providers"].(map[string]any)["ollama"]; !ok {
		t.Fatalf("ollama provider block missing")
	}
}

// --- Phase 2 (delegation-routing): subagents overlay generator tests ---

func TestGenerateConfig_SubagentsOverlay_Basic(t *testing.T) {
	// Subagent-only overlay (no primary model block) — primary stays at the
	// runtime default (anthropic/claude-opus-4-8) and the subagent is Qwen
	// via LiteLLM. Mirrors the role-code-dev shape.
	params := baseParams()
	params.Overlay = &runtime.AgentOverlay{
		Version: 2,
		Subagents: &runtime.SubagentsOverlay{
			Model: &runtime.ModelOverlay{
				Provider: runtime.ProviderOpenAI,
				Name:     "qwen-2.5-72b-instruct",
				BaseURL:  "https://litellm.lan/v1",
			},
		},
	}

	r := &Runtime{}
	out, err := r.GenerateConfig(params)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	cfg := decodeJSON(t, out)

	defaults := cfg["agents"].(map[string]any)["defaults"].(map[string]any)

	// 1. agents.defaults.subagents.model emitted as provider/name pair.
	sub, ok := defaults["subagents"].(map[string]any)
	if !ok {
		t.Fatalf("missing agents.defaults.subagents block: %+v", defaults)
	}
	if got := sub["model"]; got != "openai/qwen-2.5-72b-instruct" {
		t.Fatalf("subagents.model: want openai/qwen-2.5-72b-instruct, got %v", got)
	}
	// delegationMode / maxConcurrent must NOT appear when unset (no zero/null values).
	if _, ok := sub["delegationMode"]; ok {
		t.Fatalf("delegationMode should not be set when omitted: %+v", sub)
	}
	if _, ok := sub["maxConcurrent"]; ok {
		t.Fatalf("maxConcurrent should not be set when omitted: %+v", sub)
	}

	// 2. Subagent merged into the models allowlist.
	allow := defaults["models"].(map[string]any)
	if _, ok := allow["openai/qwen-2.5-72b-instruct"]; !ok {
		t.Fatalf("allowlist missing subagent model: %+v", allow)
	}
	// Runtime default preserved (no overlay primary → defaults stay).
	if _, ok := allow["anthropic/claude-opus-4-8"]; !ok {
		t.Fatalf("allowlist should preserve runtime default: %+v", allow)
	}

	// 3. models.providers.openai created from scratch with the subagent endpoint.
	openai := cfg["models"].(map[string]any)["providers"].(map[string]any)["openai"].(map[string]any)
	if got := openai["baseUrl"]; got != "https://litellm.lan/v1" {
		t.Fatalf("openai.baseUrl: want https://litellm.lan/v1, got %v", got)
	}
	wantModels := []any{
		map[string]any{"id": "qwen-2.5-72b-instruct", "name": "qwen-2.5-72b-instruct"},
	}
	if !reflect.DeepEqual(openai["models"], wantModels) {
		t.Fatalf("openai.models: want %+v, got %+v", wantModels, openai["models"])
	}
	// apiKey must NOT be in config for openai (env-var injection).
	if v, ok := openai["apiKey"]; ok {
		t.Fatalf("apiKey should not be written for openai, got %v", v)
	}
}

func TestGenerateConfig_SubagentsOverlay_DelegationModeAndConcurrent(t *testing.T) {
	params := baseParams()
	params.Overlay = &runtime.AgentOverlay{
		Version: 2,
		Subagents: &runtime.SubagentsOverlay{
			Model: &runtime.ModelOverlay{
				Provider: runtime.ProviderOpenAI,
				Name:     "qwen-2.5-72b-instruct",
				BaseURL:  "https://litellm.lan/v1",
			},
			DelegationMode: runtime.DelegationModePrefer,
			MaxConcurrent:  4,
		},
	}

	r := &Runtime{}
	out, err := r.GenerateConfig(params)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	cfg := decodeJSON(t, out)

	sub := cfg["agents"].(map[string]any)["defaults"].(map[string]any)["subagents"].(map[string]any)
	if got := sub["delegationMode"]; got != "prefer" {
		t.Fatalf("delegationMode: want prefer, got %v", got)
	}
	if got := sub["maxConcurrent"]; got != float64(4) {
		t.Fatalf("maxConcurrent: want 4, got %v", got)
	}
}

func TestGenerateConfig_SubagentsOverlay_MaxSpawnDepthNotEmitted(t *testing.T) {
	// max_spawn_depth is a Hermes-only knob — it must NOT appear in
	// OpenClaw config even when the operator set it in the overlay.
	params := baseParams()
	params.Overlay = &runtime.AgentOverlay{
		Version: 2,
		Subagents: &runtime.SubagentsOverlay{
			Model: &runtime.ModelOverlay{
				Provider: runtime.ProviderOpenAI,
				Name:     "qwen",
				BaseURL:  "https://litellm.lan/v1",
			},
			MaxSpawnDepth: 2, // set but should be ignored by the OpenClaw generator
		},
	}

	r := &Runtime{}
	out, err := r.GenerateConfig(params)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	cfg := decodeJSON(t, out)

	sub := cfg["agents"].(map[string]any)["defaults"].(map[string]any)["subagents"].(map[string]any)
	if _, ok := sub["maxSpawnDepth"]; ok {
		t.Fatalf("maxSpawnDepth must not be emitted by OpenClaw generator (Hermes-only); got %+v", sub)
	}
	if _, ok := sub["max_spawn_depth"]; ok {
		t.Fatalf("max_spawn_depth must not be emitted by OpenClaw generator (Hermes-only); got %+v", sub)
	}
}

func TestGenerateConfig_V2NoSubagentsBlock_IdenticalToV1(t *testing.T) {
	// A v2 overlay without a subagents block must produce byte-identical
	// output to the same overlay declared as v1. This is the no-regression
	// guarantee for Feature #27 documents.
	v1Params := baseParams()
	v1Params.Overlay = &runtime.AgentOverlay{
		Version: 1,
		Model: &runtime.ModelOverlay{
			Provider: runtime.ProviderOllama,
			Name:     "qwen3:6b",
			BaseURL:  "http://192.168.181.97:11434",
		},
	}

	v2Params := baseParams()
	v2Params.Overlay = &runtime.AgentOverlay{
		Version: 2,
		Model:   v1Params.Overlay.Model, // same primary, no subagents block
	}

	r := &Runtime{}
	v1Out, err := r.GenerateConfig(v1Params)
	if err != nil {
		t.Fatalf("v1 generate: %v", err)
	}
	v2Out, err := r.GenerateConfig(v2Params)
	if err != nil {
		t.Fatalf("v2 generate: %v", err)
	}
	if !reflect.DeepEqual(v1Out, v2Out) {
		t.Fatalf("v2-without-subagents must equal v1\nv1: %s\nv2: %s", v1Out, v2Out)
	}

	// And neither output should have a subagents block.
	cfg := decodeJSON(t, v2Out)
	defaults := cfg["agents"].(map[string]any)["defaults"].(map[string]any)
	if _, ok := defaults["subagents"]; ok {
		t.Fatalf("v2 without subagents block should not emit agents.defaults.subagents: %+v", defaults)
	}
}

func TestGenerateConfig_SubagentsOverlay_AllowlistMergePreservesPrimary(t *testing.T) {
	// Both primary (Opus) and subagent (Qwen) must appear in the allowlist,
	// alongside the runtime default. Verifies the additive-allowlist invariant
	// from Feature #27 holds when subagents extends it.
	params := baseParams()
	params.Overlay = &runtime.AgentOverlay{
		Version: 2,
		Model: &runtime.ModelOverlay{
			Provider: runtime.ProviderOllama,
			Name:     "qwen3:6b",
			BaseURL:  "http://h:11434",
		},
		Subagents: &runtime.SubagentsOverlay{
			Model: &runtime.ModelOverlay{
				Provider: runtime.ProviderOpenAI,
				Name:     "qwen-2.5-72b-instruct",
				BaseURL:  "https://litellm.lan/v1",
			},
		},
	}

	r := &Runtime{}
	out, err := r.GenerateConfig(params)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	cfg := decodeJSON(t, out)

	allow := cfg["agents"].(map[string]any)["defaults"].(map[string]any)["models"].(map[string]any)
	for _, modelRef := range []string{"ollama/qwen3:6b", "openai/qwen-2.5-72b-instruct", "anthropic/claude-opus-4-8"} {
		if _, ok := allow[modelRef]; !ok {
			t.Fatalf("allowlist missing %q: %+v", modelRef, allow)
		}
	}

	// Primary remains the overlay's choice.
	if got := cfg["agents"].(map[string]any)["defaults"].(map[string]any)["model"].(map[string]any)["primary"]; got != "ollama/qwen3:6b" {
		t.Fatalf("primary: want ollama/qwen3:6b, got %v", got)
	}

	// Both providers configured.
	providers := cfg["models"].(map[string]any)["providers"].(map[string]any)
	if _, ok := providers["ollama"]; !ok {
		t.Fatalf("missing ollama provider: %+v", providers)
	}
	if _, ok := providers["openai"]; !ok {
		t.Fatalf("missing openai provider: %+v", providers)
	}
}

func TestGenerateConfig_SubagentsOverlay_SameProviderAppendsToModelsArray(t *testing.T) {
	// Primary openai + subagent openai with the SAME base_url is the
	// validation-passing same-provider case. The generator must append the
	// subagent model to the existing provider's models[] array rather than
	// clobbering or creating two entries.
	params := baseParams()
	params.Overlay = &runtime.AgentOverlay{
		Version: 2,
		Model: &runtime.ModelOverlay{
			Provider: runtime.ProviderOpenAI,
			Name:     "primary-model",
			BaseURL:  "https://litellm.lan/v1",
		},
		Subagents: &runtime.SubagentsOverlay{
			Model: &runtime.ModelOverlay{
				Provider: runtime.ProviderOpenAI,
				Name:     "subagent-model",
				BaseURL:  "https://litellm.lan/v1",
			},
		},
	}

	r := &Runtime{}
	out, err := r.GenerateConfig(params)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	cfg := decodeJSON(t, out)

	openai := cfg["models"].(map[string]any)["providers"].(map[string]any)["openai"].(map[string]any)
	models, ok := openai["models"].([]any)
	if !ok {
		t.Fatalf("openai.models is not an array: %+v", openai["models"])
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models entries (primary + subagent), got %d: %+v", len(models), models)
	}
	// Both ids should be represented, no duplicates.
	gotIDs := map[string]bool{}
	for _, m := range models {
		gotIDs[m.(map[string]any)["id"].(string)] = true
	}
	if !gotIDs["primary-model"] || !gotIDs["subagent-model"] {
		t.Fatalf("expected both primary-model and subagent-model in openai.models, got %v", gotIDs)
	}
}

func TestGenerateConfig_SubagentsOverlay_SameProviderConflictDefense(t *testing.T) {
	// Defense-in-depth: AgentOverlay.Validate normally catches this conflict
	// at load time, but the generator itself must also reject if a caller
	// constructs an AgentOverlay programmatically and skips Validate.
	params := baseParams()
	params.Overlay = &runtime.AgentOverlay{
		Version: 2,
		Model: &runtime.ModelOverlay{
			Provider: runtime.ProviderOpenAI,
			Name:     "primary",
			BaseURL:  "https://api.openai.com/v1",
		},
		Subagents: &runtime.SubagentsOverlay{
			Model: &runtime.ModelOverlay{
				Provider: runtime.ProviderOpenAI,
				Name:     "subagent",
				BaseURL:  "https://litellm.lan/v1", // DIFFERENT endpoint
			},
		},
	}

	r := &Runtime{}
	_, err := r.GenerateConfig(params)
	if err == nil {
		t.Fatal("expected generator-level same-provider-conflict error, got nil")
	}
	if !strings.Contains(err.Error(), "conflicts with primary") {
		t.Fatalf("expected conflict error, got %v", err)
	}
}

func TestGenerateConfig_TeamAgent_AppliesChannelDiscipline(t *testing.T) {
	params := baseParams()
	params.Agent.Type = provider.AgentTypeTeam

	r := &Runtime{}
	out, err := r.GenerateConfig(params)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	cfg := decodeJSON(t, out)

	messages, ok := cfg["messages"].(map[string]any)
	if !ok {
		t.Fatalf("team agent missing messages section; got %+v", cfg["messages"])
	}
	groupChat, ok := messages["groupChat"].(map[string]any)
	if !ok {
		t.Fatalf("team agent missing messages.groupChat; got %+v", messages)
	}
	if got := groupChat["visibleReplies"]; got != "message_tool" {
		t.Fatalf("messages.groupChat.visibleReplies: want \"message_tool\", got %v", got)
	}

	tools, ok := cfg["tools"].(map[string]any)
	if !ok {
		t.Fatalf("tools section missing; got %+v", cfg["tools"])
	}
	alsoAllow, ok := tools["alsoAllow"].([]any)
	if !ok {
		t.Fatalf("tools.alsoAllow missing; got %+v", tools)
	}
	found := false
	for _, v := range alsoAllow {
		if s, ok := v.(string); ok && s == "message" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("tools.alsoAllow must contain \"message\" to keep replies from silently dropping; got %+v", alsoAllow)
	}
}

func TestGenerateConfig_UserAgent_NoChannelDiscipline(t *testing.T) {
	// User agents operate in DMs where mild preamble is fine and silent-drop
	// risk is higher. The team-only branch must NOT fire here.
	params := baseParams() // baseParams uses AgentTypeUser
	r := &Runtime{}
	out, err := r.GenerateConfig(params)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	cfg := decodeJSON(t, out)

	if msgs, ok := cfg["messages"].(map[string]any); ok {
		if gc, ok := msgs["groupChat"].(map[string]any); ok {
			if _, set := gc["visibleReplies"]; set {
				t.Fatalf("user agent must not set messages.groupChat.visibleReplies; got %+v", gc)
			}
		}
	}
	if tools, ok := cfg["tools"].(map[string]any); ok {
		if _, set := tools["alsoAllow"]; set {
			t.Fatalf("user agent must not set tools.alsoAllow; got %+v", tools)
		}
	}
}

func TestGenerateConfig_IncludesAdminCustomFile(t *testing.T) {
	// Every generated managed root must reference the admin-owned include via a
	// relative "$include" so OpenClaw deep-merges agent-custom.json. Verified
	// across agent types and overlay presence; the directive is purely additive
	// (the rest of the suite guards the unchanged structure).
	r := &Runtime{}

	team := baseParams()
	team.Agent.Type = provider.AgentTypeTeam
	team.Agent.Channels = []channels.ChannelBinding{{Platform: "slack", ID: "C0123456789"}}
	team.Secrets.Values = map[string]string{
		"slack-bot-token":      "xoxb-test",
		"slack-signing-secret": "secret",
	}

	overlay := baseParams()
	overlay.Overlay = &runtime.AgentOverlay{
		Version: 1,
		Model: &runtime.ModelOverlay{
			Provider: runtime.ProviderOllama,
			Name:     "qwen3:6b",
			BaseURL:  "http://192.168.181.97:11434",
		},
	}

	cases := map[string]runtime.ConfigParams{
		"user-no-overlay":   baseParams(),
		"team-with-channel": team,
		"user-with-overlay": overlay,
	}

	for name, params := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := r.GenerateConfig(params)
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			cfg := decodeJSON(t, out)

			inc, ok := cfg["$include"].([]any)
			if !ok {
				t.Fatalf("missing $include array; got %T %+v", cfg["$include"], cfg["$include"])
			}
			// Feature #31: layered includes, order = precedence (later wins),
			// admin-drift (agent-custom.json) last so it wins over per-agent + fleet.
			want := []any{FleetCustomConfigFile, AgentManagedCustomConfigFile, AgentCustomConfigFile}
			if !reflect.DeepEqual(inc, want) {
				t.Fatalf("$include = %+v, want %+v", inc, want)
			}

			// Purely additive: core managed sections still present alongside it.
			if _, ok := cfg["gateway"]; !ok {
				t.Fatalf("gateway section missing after $include injection")
			}
			if _, ok := cfg["agents"]; !ok {
				t.Fatalf("agents section missing after $include injection")
			}
		})
	}
}

// TestManagedCustomConfigFiles asserts the openclaw runtime advertises its two
// Conga-deployed managed include layers in lowest-precedence-first order (the
// order EffectiveConfigSpecs reverses and the integrity/hashing paths rely on).
func TestManagedCustomConfigFiles(t *testing.T) {
	r := &Runtime{}
	got := r.ManagedCustomConfigFiles()
	want := []string{FleetCustomConfigFile, AgentManagedCustomConfigFile}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ManagedCustomConfigFiles = %v, want %v", got, want)
	}
}

// TestGenerateConfig_RuntimeDefaults covers the feature #31 de-embed: when
// ConfigParams.RuntimeDefaults carries an on-disk baseline it replaces the
// embedded copy as the generation base; absent or malformed bytes fall back to
// the embed (first-boot / air-gap / tamper-safe).
func TestGenerateConfig_RuntimeDefaults(t *testing.T) {
	r := &Runtime{}

	const sentinel = "anthropic/claude-sentinel-on-disk"
	onDisk := []byte(`{"agents":{"defaults":{"model":{"primary":"` + sentinel + `","fallbacks":[]}}}}`)

	t.Run("file present and valid is used as base", func(t *testing.T) {
		p := baseParams()
		p.RuntimeDefaults = onDisk
		out, err := r.GenerateConfig(p)
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		if got := modelPrimary(t, out); got != sentinel {
			t.Fatalf("on-disk defaults not used: model.primary = %q, want %q", got, sentinel)
		}
	})

	t.Run("absent falls back to embedded", func(t *testing.T) {
		p := baseParams() // RuntimeDefaults nil
		out, err := r.GenerateConfig(p)
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		// Embedded baseline's model.primary (see openclaw-defaults.json).
		if got := modelPrimary(t, out); got != "anthropic/claude-opus-4-8" {
			t.Fatalf("embedded fallback not used: model.primary = %q", got)
		}
	})

	t.Run("malformed falls back to embedded", func(t *testing.T) {
		p := baseParams()
		p.RuntimeDefaults = []byte(`{"agents": this is not json`)
		out, err := r.GenerateConfig(p)
		if err != nil {
			t.Fatalf("generate should fall back, not error: %v", err)
		}
		if got := modelPrimary(t, out); got != "anthropic/claude-opus-4-8" {
			t.Fatalf("malformed input should fall back to embedded: model.primary = %q", got)
		}
	})
}

// modelPrimary extracts agents.defaults.model.primary from generated config.
func modelPrimary(t *testing.T, b []byte) string {
	t.Helper()
	cfg := decodeJSON(t, b)
	agents, ok := cfg["agents"].(map[string]any)
	if !ok {
		t.Fatalf("missing agents section")
	}
	defaults, ok := agents["defaults"].(map[string]any)
	if !ok {
		t.Fatalf("missing agents.defaults")
	}
	model, ok := defaults["model"].(map[string]any)
	if !ok {
		t.Fatalf("missing agents.defaults.model")
	}
	s, _ := model["primary"].(string)
	return s
}
