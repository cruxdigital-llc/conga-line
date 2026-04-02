package manifest

import (
	"context"
	"fmt"
	"os"

	"github.com/cruxdigital-llc/conga-line/cli/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/cli/pkg/common"
	"github.com/cruxdigital-llc/conga-line/cli/pkg/policy"
	"github.com/cruxdigital-llc/conga-line/cli/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/cli/pkg/ui"
)

// BootstrapResult holds the outcome of each step for JSON output.
type BootstrapResult struct {
	Steps []StepResult `json:"steps"`
}

// StepResult describes one step's outcome.
type StepResult struct {
	Name    string   `json:"name"`
	Status  string   `json:"status"` // "done", "skipped", "error"
	Details []string `json:"details,omitempty"`
}

type step struct {
	name string
	fn   func() (StepResult, error)
}

// Bootstrap executes the manifest against the given provider.
//
// Execution order is optimized to minimize redundant restarts:
//
//	setup → agents → secrets → channels → policy → bindings → (refresh if needed)
//
// Policy handling: if conga-policy.yaml already exists on disk, the manifest's
// policy section is ignored (existing policy wins). The policy is only seeded
// from the manifest on first bootstrap when no policy file exists.
//
// Policy is written before bindings so that BindChannel's internal refresh
// picks up secrets, policy, and channel config in one shot. RefreshAll only
// runs when there are changes but no bindings triggered a refresh.
func Bootstrap(ctx context.Context, prov provider.Provider, m *Manifest, policyPath string) (*BootstrapResult, error) {
	result := &BootstrapResult{}

	var steps []step
	hasBindings := channelBindingCount(m) > 0

	// Resolve policy: existing file wins over manifest section.
	writePolicy := false
	if m.Policy != nil {
		_, err := os.Stat(policyPath)
		if err != nil {
			if os.IsNotExist(err) {
				writePolicy = true
			} else {
				return nil, fmt.Errorf("checking policy file %s: %w", policyPath, err)
			}
		} else {
			ui.Infoln("Using existing conga-policy.yaml (bootstrap policy section ignored)")
		}
	}

	if m.Setup != nil {
		steps = append(steps, step{"Setting up environment", func() (StepResult, error) {
			return bootstrapSetup(ctx, prov, m.Setup)
		}})
	}
	if len(m.Agents) > 0 {
		steps = append(steps, step{"Provisioning agents", func() (StepResult, error) {
			return bootstrapAgents(ctx, prov, m.Agents)
		}})
		steps = append(steps, step{"Setting agent secrets", func() (StepResult, error) {
			return bootstrapSecrets(ctx, prov, m.Agents)
		}})
	}
	if len(m.Channels) > 0 {
		steps = append(steps, step{"Adding channels", func() (StepResult, error) {
			return bootstrapChannels(ctx, prov, m.Channels)
		}})
	}
	// Policy before bindings — BindChannel's refresh picks up the policy.
	if writePolicy {
		steps = append(steps, step{"Seeding policy", func() (StepResult, error) {
			return bootstrapPolicy(m.Policy, policyPath)
		}})
	}
	if hasBindings {
		steps = append(steps, step{"Binding channels", func() (StepResult, error) {
			return bootstrapBindings(ctx, prov, m.Channels)
		}})
	}
	// Only RefreshAll when no bindings will trigger per-agent refreshes.
	// BindChannel internally refreshes each agent, so a separate RefreshAll
	// would just restart everything a second time.
	if len(steps) > 0 && !hasBindings {
		steps = append(steps, step{"Refreshing agents", func() (StepResult, error) {
			return bootstrapRefresh(ctx, prov)
		}})
	}

	if len(steps) == 0 {
		return result, fmt.Errorf("manifest has no actionable sections (no setup, agents, channels, or policy)")
	}

	total := len(steps)
	for i, s := range steps {
		ui.Info("[%d/%d] %s... ", i+1, total, s.name)
		sr, err := s.fn()
		sr.Name = s.name
		result.Steps = append(result.Steps, sr)
		if err != nil {
			ui.Infoln("error")
			return result, err
		}
		ui.Infoln(sr.Status)
		for _, d := range sr.Details {
			ui.Info("       %s\n", d)
		}
	}

	return result, nil
}

func channelBindingCount(m *Manifest) int {
	n := 0
	for _, ch := range m.Channels {
		n += len(ch.Bindings)
	}
	return n
}

// agentExists checks whether an agent with the given name already exists.
// Uses ListAgents to avoid conflating "not found" with transient errors.
func agentExists(ctx context.Context, prov provider.Provider, name string) (bool, error) {
	agents, err := prov.ListAgents(ctx)
	if err != nil {
		return false, err
	}
	for _, a := range agents {
		if a.Name == name {
			return true, nil
		}
	}
	return false, nil
}

func bootstrapSetup(ctx context.Context, prov provider.Provider, setup *ManifestSetup) (StepResult, error) {
	cfg := &provider.SetupConfig{
		Image:         setup.Image,
		RepoPath:      setup.RepoPath,
		SSHHost:       setup.SSHHost,
		SSHPort:       setup.SSHPort,
		SSHUser:       setup.SSHUser,
		SSHKeyPath:    setup.SSHKeyPath,
		InstallDocker: setup.InstallDocker,
	}
	if err := prov.Setup(ctx, cfg); err != nil {
		return StepResult{Status: "error"}, fmt.Errorf("setup: %w", err)
	}
	return StepResult{Status: "done"}, nil
}

func bootstrapAgents(ctx context.Context, prov provider.Provider, agents []ManifestAgent) (StepResult, error) {
	sr := StepResult{Status: "done"}
	for _, a := range agents {
		exists, err := agentExists(ctx, prov, a.Name)
		if err != nil {
			return sr, fmt.Errorf("checking agent %q: %w", a.Name, err)
		}
		if exists {
			sr.Details = append(sr.Details, a.Name+": already exists")
			continue
		}

		existing, err := prov.ListAgents(ctx)
		if err != nil {
			return sr, fmt.Errorf("listing agents for port assignment: %w", err)
		}
		port := common.NextAvailablePort(existing)

		agentType := provider.AgentTypeUser
		if a.Type == "team" {
			agentType = provider.AgentTypeTeam
		}

		cfg := provider.AgentConfig{
			Name:        a.Name,
			Type:        agentType,
			GatewayPort: port,
			IAMIdentity: a.IAMIdentity,
		}
		if err := prov.ProvisionAgent(ctx, cfg); err != nil {
			return sr, fmt.Errorf("provisioning agent %q: %w", a.Name, err)
		}
		sr.Details = append(sr.Details, fmt.Sprintf("%s: provisioned (port %d)", a.Name, port))
	}
	return sr, nil
}

func bootstrapSecrets(ctx context.Context, prov provider.Provider, agents []ManifestAgent) (StepResult, error) {
	sr := StepResult{Status: "done"}
	count := 0
	for _, a := range agents {
		for name, value := range a.Secrets {
			if err := prov.SetSecret(ctx, a.Name, name, value); err != nil {
				return sr, fmt.Errorf("setting secret %q for agent %q: %w", name, a.Name, err)
			}
			count++
		}
		if len(a.Secrets) > 0 {
			sr.Details = append(sr.Details, fmt.Sprintf("%s: %d secret(s)", a.Name, len(a.Secrets)))
		}
	}
	if count == 0 {
		sr.Status = "skipped"
		sr.Details = []string{"no secrets defined"}
	}
	return sr, nil
}

func bootstrapChannels(ctx context.Context, prov provider.Provider, chans []ManifestChannel) (StepResult, error) {
	sr := StepResult{Status: "done"}

	existing, err := prov.ListChannels(ctx)
	if err != nil {
		return sr, fmt.Errorf("listing channels: %w", err)
	}
	configured := make(map[string]bool)
	for _, ch := range existing {
		if ch.Configured {
			configured[ch.Platform] = true
		}
	}

	for _, ch := range chans {
		if configured[ch.Platform] {
			sr.Details = append(sr.Details, ch.Platform+": already configured")
			continue
		}
		if err := prov.AddChannel(ctx, ch.Platform, ch.Secrets); err != nil {
			return sr, fmt.Errorf("adding channel %q: %w", ch.Platform, err)
		}
		sr.Details = append(sr.Details, ch.Platform+": added")
	}
	return sr, nil
}

func bootstrapBindings(ctx context.Context, prov provider.Provider, chans []ManifestChannel) (StepResult, error) {
	sr := StepResult{Status: "done"}

	for _, ch := range chans {
		for _, b := range ch.Bindings {
			agent, err := prov.GetAgent(ctx, b.Agent)
			if err != nil {
				return sr, fmt.Errorf("getting agent %q for binding: %w", b.Agent, err)
			}

			alreadyBound := false
			for _, existing := range agent.Channels {
				if existing.Platform == ch.Platform && existing.ID == b.ID {
					alreadyBound = true
					break
				}
			}
			if alreadyBound {
				sr.Details = append(sr.Details, fmt.Sprintf("%s -> %s:%s: already bound", b.Agent, ch.Platform, b.ID))
				continue
			}

			binding := channels.ChannelBinding{
				Platform: ch.Platform,
				ID:       b.ID,
				Label:    b.Label,
			}
			if err := prov.BindChannel(ctx, b.Agent, binding); err != nil {
				return sr, fmt.Errorf("binding agent %q to %s:%s: %w", b.Agent, ch.Platform, b.ID, err)
			}
			sr.Details = append(sr.Details, fmt.Sprintf("%s -> %s:%s: bound", b.Agent, ch.Platform, b.ID))
		}
	}
	return sr, nil
}

func bootstrapPolicy(mp *ManifestPolicy, policyPath string) (StepResult, error) {
	pf := &policy.PolicyFile{
		APIVersion: policy.CurrentAPIVersion,
		Egress:     mp.Egress,
		Routing:    mp.Routing,
		Posture:    mp.Posture,
		Agents:     mp.Agents,
	}
	if err := policy.Save(pf, policyPath); err != nil {
		return StepResult{Status: "error"}, fmt.Errorf("saving policy: %w", err)
	}
	return StepResult{Status: "done", Details: []string{"seeded to " + policyPath}}, nil
}

func bootstrapRefresh(ctx context.Context, prov provider.Provider) (StepResult, error) {
	if err := prov.RefreshAll(ctx); err != nil {
		return StepResult{Status: "error"}, fmt.Errorf("refreshing agents: %w", err)
	}
	return StepResult{Status: "done"}, nil
}
