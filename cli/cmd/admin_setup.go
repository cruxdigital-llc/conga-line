package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/cruxdigital-llc/conga-line/cli/internal/provider"
	"github.com/cruxdigital-llc/conga-line/cli/internal/ui"
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
