package hermes

import (
	"fmt"
	"strings"

	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
	"gopkg.in/yaml.v3"
)

func (r *Runtime) GenerateConfig(params runtime.ConfigParams) ([]byte, error) {
	apiServerExtra := map[string]any{
		"host": "0.0.0.0",
		"port": ContainerPort,
	}

	// Gateway auth token
	if params.GatewayToken != "" {
		apiServerExtra["key"] = params.GatewayToken
		origins := []string{
			fmt.Sprintf("http://localhost:%d", ContainerPort),
			fmt.Sprintf("http://localhost:%d", params.Agent.GatewayPort),
		}
		apiServerExtra["cors_origins"] = strings.Join(origins, ",")
	}

	platforms := map[string]any{
		"api_server": map[string]any{
			"enabled": true,
			"extra":   apiServerExtra,
		},
	}

	// Enable webhook adapter if any channels are bound.
	// All channel events (Slack, Telegram, etc.) arrive via the webhook adapter.
	if len(params.Agent.Channels) > 0 {
		platforms["webhook"] = map[string]any{
			"enabled": true,
			"extra": map[string]any{
				"host": "0.0.0.0",
				"port": 8644,
			},
		}
	}

	cfg := map[string]any{
		"platforms": platforms,
	}

	// Set model if provided (configured during conga admin setup).
	// If empty, Hermes will prompt the user via `hermes model` on first use.
	if params.Model != "" {
		cfg["model"] = params.Model
	}

	return yaml.Marshal(cfg)
}

func (r *Runtime) ConfigFileName() string { return "config.yaml" }
