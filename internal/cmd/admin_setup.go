package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/ui"
	"github.com/spf13/cobra"
)

var adminSetupConfig string

func init() {
	// setupCmd is registered in admin.go init()
}

func adminSetupRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	// Mutual exclusion: --json and --config cannot both be provided
	if ui.JSONInputActive && adminSetupConfig != "" {
		return fmt.Errorf("cannot use both --json and --config")
	}

	var cfg *provider.SetupConfig
	if ui.JSONInputActive {
		// Parse JSON input directly into SetupConfig, rejecting unknown fields
		data, err := json.Marshal(ui.JSONData())
		if err != nil {
			return fmt.Errorf("marshaling JSON input: %w", err)
		}
		cfg = &provider.SetupConfig{}
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(cfg); err != nil {
			return fmt.Errorf("parsing JSON input as setup config: %w", err)
		}
	} else if adminSetupConfig != "" {
		var err error
		cfg, err = provider.ParseSetupConfig(adminSetupConfig)
		if err != nil {
			return fmt.Errorf("invalid --config: %w", err)
		}
	}

	// Pre-persist --runtime flag so Setup() sees it via getConfigValue().
	// This mirrors how --provider is resolved before Setup is called.
	if flagRuntime != "" {
		if cfg == nil {
			// Interactive mode: just set the runtime, don't create a full SetupConfig
			// (a non-nil cfg would skip interactive prompts for other fields)
			presetRuntime(flagRuntime)
		} else if cfg.Runtime == "" {
			cfg.Runtime = flagRuntime
		}
	}

	if err := prov.Setup(ctx, cfg); err != nil {
		return err
	}

	if ui.OutputJSON {
		ui.EmitJSON(struct {
			Provider string `json:"provider"`
			Status   string `json:"status"`
		}{
			Provider: prov.Name(),
			Status:   "configured",
		})
	}
	return nil
}

// presetRuntime writes the runtime value to the provider's local config
// before Setup() runs, so Setup() sees it via getConfigValue("runtime")
// and skips the interactive prompt for it.
func presetRuntime(rt string) {
	dataDir := flagDataDir
	if dataDir == "" {
		dataDir = provider.DefaultDataDir()
	}
	configPath := filepath.Join(dataDir, "local-config.json")

	extra := make(map[string]string)
	if data, err := os.ReadFile(configPath); err == nil {
		json.Unmarshal(data, &extra)
	}
	extra["runtime"] = rt

	os.MkdirAll(dataDir, 0700)
	if data, err := json.MarshalIndent(extra, "", "  "); err == nil {
		os.WriteFile(configPath, data, 0600)
	}
}
