package mcpserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cruxdigital-llc/conga-line/cli/internal/policy"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// policyPath resolves ~/.conga/conga-policy.yaml.
func (s *Server) policyPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".conga", "conga-policy.yaml"), nil
}

// loadPolicy loads the policy file, returning a default skeleton if the file
// does not exist. Returns the parsed policy and the file path.
func (s *Server) loadPolicy() (*policy.PolicyFile, string, error) {
	path, err := s.policyPath()
	if err != nil {
		return nil, "", err
	}
	pf, err := policy.Load(path)
	if err != nil {
		return nil, "", err
	}
	if pf == nil {
		pf = &policy.PolicyFile{APIVersion: policy.CurrentAPIVersion}
	}
	return pf, path, nil
}

// getStringSlice extracts a []string from the raw MCP request arguments.
func getStringSlice(req mcp.CallToolRequest, key string) ([]string, error) {
	args := req.GetArguments()
	raw, ok := args[key]
	if !ok {
		return nil, nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an array, got %T", key, raw)
	}
	result := make([]string, 0, len(arr))
	for i, v := range arr {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("%s[%d] must be a string, got %T", key, i, v)
		}
		result = append(result, s)
	}
	return result, nil
}

// getCostLimits extracts a CostLimits from the raw MCP request arguments.
func getCostLimits(req mcp.CallToolRequest) (*policy.CostLimits, error) {
	args := req.GetArguments()
	raw, ok := args["cost_limits"]
	if !ok {
		return nil, nil
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("cost_limits must be an object, got %T", raw)
	}
	cl := &policy.CostLimits{}
	if v, exists := m["daily_per_agent"]; exists {
		f, ok := v.(float64)
		if !ok {
			return nil, fmt.Errorf("cost_limits.daily_per_agent must be a number, got %T", v)
		}
		cl.DailyPerAgent = f
	}
	if v, exists := m["monthly_per_agent"]; exists {
		f, ok := v.(float64)
		if !ok {
			return nil, fmt.Errorf("cost_limits.monthly_per_agent must be a number, got %T", v)
		}
		cl.MonthlyPerAgent = f
	}
	if v, exists := m["monthly_global"]; exists {
		f, ok := v.(float64)
		if !ok {
			return nil, fmt.Errorf("cost_limits.monthly_global must be a number, got %T", v)
		}
		cl.MonthlyGlobal = f
	}
	return cl, nil
}

// --- Read-only tools ---

func (s *Server) toolPolicyGet() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_policy_get",
			Description: "Read the current conga-policy.yaml as JSON. Returns an empty skeleton if no policy file exists. Note: YAML comments are not preserved when the policy is modified via set tools.",
			InputSchema: mcp.ToolInputSchema{
				Type:       "object",
				Properties: map[string]any{},
			},
			Annotations: mcp.ToolAnnotation{
				ReadOnlyHint: boolPtr(true),
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			pf, _, err := s.loadPolicy()
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return jsonResult(pf)
		},
	}
}

type validationResult struct {
	Valid  bool                `json:"valid"`
	Error  string              `json:"error,omitempty"`
	Policy *policy.PolicyFile  `json:"policy"`
	Report []policy.RuleReport `json:"enforcement_report"`
}

func (s *Server) toolPolicyValidate() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_policy_validate",
			Description: "Validate the policy file and return the enforcement report for the current provider. Shows which rules are enforced, validate-only, or not applicable.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"agent": map[string]any{
						"type":        "string",
						"description": "If set, merge per-agent overrides before generating the enforcement report",
					},
				},
			},
			Annotations: mcp.ToolAnnotation{
				ReadOnlyHint: boolPtr(true),
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			pf, _, err := s.loadPolicy()
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			if err := pf.Validate(); err != nil {
				return jsonResult(validationResult{
					Valid:  false,
					Error:  err.Error(),
					Policy: pf,
				})
			}

			reportTarget := pf
			if agent := req.GetString("agent", ""); agent != "" {
				reportTarget = pf.MergeForAgent(agent)
			}

			return jsonResult(validationResult{
				Valid:  true,
				Policy: pf,
				Report: reportTarget.EnforcementReport(s.prov.Name()),
			})
		},
	}
}

func (s *Server) toolPolicyGetAgent() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_policy_get_agent",
			Description: "Get the effective policy for a specific agent with per-agent overrides merged. The returned policy has no 'agents' map — overrides have been flattened into the top-level sections.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"agent": map[string]any{
						"type":        "string",
						"description": "Agent name",
					},
				},
				Required: []string{"agent"},
			},
			Annotations: mcp.ToolAnnotation{
				ReadOnlyHint: boolPtr(true),
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			agent, err := req.RequireString("agent")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			pf, _, err := s.loadPolicy()
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			return jsonResult(pf.MergeForAgent(agent))
		},
	}
}

// --- Mutation tools ---

func (s *Server) toolPolicySetEgress() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_policy_set_egress",
			Description: "Update the egress policy (allowed/blocked domains and enforcement mode). Replaces the entire egress section. Validates before saving. Use 'agent' to create a per-agent override instead of modifying the global policy.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"allowed_domains": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Domains the agent can reach (e.g., api.anthropic.com, *.slack.com)",
					},
					"blocked_domains": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Domains to explicitly block (takes precedence over allowed_domains)",
					},
					"mode": map[string]any{
						"type":        "string",
						"enum":        []string{"validate", "enforce"},
						"description": "Enforcement mode: 'validate' (proxy logs violations but allows traffic) or 'enforce' (proxy blocks non-allowlisted traffic + iptables)",
					},
					"agent": map[string]any{
						"type":        "string",
						"description": "If set, creates a per-agent override instead of modifying the global policy",
					},
				},
			},
			Annotations: mcp.ToolAnnotation{
				DestructiveHint: boolPtr(true),
				IdempotentHint:  boolPtr(true),
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			pf, path, err := s.loadPolicy()
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			allowedDomains, err := getStringSlice(req, "allowed_domains")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			blockedDomains, err := getStringSlice(req, "blocked_domains")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			mode := policy.EgressMode(req.GetString("mode", ""))
			if mode == "" {
				mode = policy.EgressModeEnforce
			}
			patch := &policy.EgressPolicy{
				AllowedDomains: allowedDomains,
				BlockedDomains: blockedDomains,
				Mode:           mode,
			}

			policy.SetEgress(pf, req.GetString("agent", ""), patch)

			if err := pf.Validate(); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("validation failed: %v", err)), nil
			}
			if err := policy.Save(pf, path); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return jsonResult(pf)
		},
	}
}

func (s *Server) toolPolicySetRouting() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_policy_set_routing",
			Description: "Update the routing policy (model selection and cost limits). Replaces the entire routing section. Validates before saving. Note: routing enforcement requires Bifrost integration (future).",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"default_model": map[string]any{
						"type":        "string",
						"description": "Default model name for agent conversations",
					},
					"fallback_chain": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Ordered list of fallback models",
					},
					"cost_limits": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"daily_per_agent":   map[string]any{"type": "number", "description": "Max daily cost per agent (USD)"},
							"monthly_per_agent": map[string]any{"type": "number", "description": "Max monthly cost per agent (USD)"},
							"monthly_global":    map[string]any{"type": "number", "description": "Max monthly cost across all agents (USD)"},
						},
						"description": "Cost budget caps",
					},
					"agent": map[string]any{
						"type":        "string",
						"description": "If set, creates a per-agent override instead of modifying the global policy",
					},
				},
			},
			Annotations: mcp.ToolAnnotation{
				DestructiveHint: boolPtr(true),
				IdempotentHint:  boolPtr(true),
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			pf, path, err := s.loadPolicy()
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			fallbackChain, err := getStringSlice(req, "fallback_chain")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			costLimits, err := getCostLimits(req)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			patch := &policy.RoutingPolicy{
				DefaultModel:  req.GetString("default_model", ""),
				FallbackChain: fallbackChain,
				CostLimits:    costLimits,
			}

			policy.SetRouting(pf, req.GetString("agent", ""), patch)

			if err := pf.Validate(); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("validation failed: %v", err)), nil
			}
			if err := policy.Save(pf, path); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return jsonResult(pf)
		},
	}
}

func (s *Server) toolPolicySetPosture() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_policy_set_posture",
			Description: "Update posture declarations (security properties). Replaces the entire posture section. Validates before saving.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"isolation_level": map[string]any{
						"type":        "string",
						"enum":        []string{"standard", "hardened", "segmented"},
						"description": "Container isolation level",
					},
					"secrets_backend": map[string]any{
						"type":        "string",
						"enum":        []string{"file", "managed", "proxy"},
						"description": "Secrets storage backend",
					},
					"monitoring": map[string]any{
						"type":        "string",
						"enum":        []string{"basic", "standard", "full"},
						"description": "Monitoring level",
					},
					"compliance_frameworks": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Compliance frameworks to declare (e.g., SOC2, HIPAA)",
					},
					"agent": map[string]any{
						"type":        "string",
						"description": "If set, creates a per-agent override instead of modifying the global policy",
					},
				},
			},
			Annotations: mcp.ToolAnnotation{
				DestructiveHint: boolPtr(true),
				IdempotentHint:  boolPtr(true),
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			pf, path, err := s.loadPolicy()
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			complianceFrameworks, err := getStringSlice(req, "compliance_frameworks")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			patch := &policy.PostureDeclarations{
				IsolationLevel:       req.GetString("isolation_level", ""),
				SecretsBackend:       req.GetString("secrets_backend", ""),
				Monitoring:           req.GetString("monitoring", ""),
				ComplianceFrameworks: complianceFrameworks,
			}

			policy.SetPosture(pf, req.GetString("agent", ""), patch)

			if err := pf.Validate(); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("validation failed: %v", err)), nil
			}
			if err := policy.Save(pf, path); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return jsonResult(pf)
		},
	}
}

// --- Deploy tool ---

type deployResult struct {
	Validated      bool     `json:"validated"`
	Deployed       []string `json:"deployed"`
	Errors         []string `json:"errors,omitempty"`
	PartialFailure bool     `json:"partial_failure,omitempty"`
}

func (s *Server) toolPolicyDeploy() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_policy_deploy",
			Description: "Validate the current policy and deploy it to running agents by refreshing their containers. This regenerates egress proxy config and restarts containers to apply the latest policy.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"agent": map[string]any{
						"type":        "string",
						"description": "Deploy to a specific agent. If omitted, deploys to all non-paused agents.",
					},
				},
			},
			Annotations: mcp.ToolAnnotation{
				DestructiveHint: boolPtr(true),
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			path, err := s.policyPath()
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			policyBytes, err := os.ReadFile(path)
			if err != nil {
				if os.IsNotExist(err) {
					return mcp.NewToolResultError("no policy file found — create one with conga_policy_set_egress or conga_policy_set_routing first"), nil
				}
				return mcp.NewToolResultError(fmt.Sprintf("failed to read policy file: %v", err)), nil
			}
			pf, err := policy.LoadFromBytes(policyBytes)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if pf == nil {
				return mcp.NewToolResultError("no policy file found — create one with conga_policy_set_egress or conga_policy_set_routing first"), nil
			}
			if err := pf.Validate(); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("policy validation failed: %v — fix the policy before deploying", err)), nil
			}
			policyContent := string(policyBytes)

			ctx, cancel := toolCtx(ctx)
			defer cancel()

			agent := req.GetString("agent", "")

			// Determine which agents to deploy to
			var targetAgents []string
			if agent != "" {
				if _, err := s.prov.GetAgent(ctx, agent); err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("agent %q not found: %v", agent, err)), nil
				}
				targetAgents = []string{agent}
			} else {
				agents, err := s.prov.ListAgents(ctx)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("listing agents: %v", err)), nil
				}
				for _, a := range agents {
					if !a.Paused {
						targetAgents = append(targetAgents, a.Name)
					}
				}
			}

			if len(targetAgents) == 0 {
				return mcp.NewToolResultError("no active agents to deploy to — all agents are paused"), nil
			}

			// Check if provider supports direct egress deployment (e.g., AWS via SSM)
			type egressDeployer interface {
				DeployEgress(ctx context.Context, agentName, policyContent, envoyConfig string, mode policy.EgressMode) error
			}

			var deployed []string
			var errors []string

			hasEgressDomains := pf.Egress != nil && len(pf.Egress.AllowedDomains) > 0
			if !hasEgressDomains {
				for _, override := range pf.Agents {
					if override != nil && override.Egress != nil && len(override.Egress.AllowedDomains) > 0 {
						hasEgressDomains = true
						break
					}
				}
			}

			if deployer, ok := s.prov.(egressDeployer); ok && hasEgressDomains {
				// Provider supports direct egress deployment — generate configs in Go and push
				for _, name := range targetAgents {
					merged := pf.MergeForAgent(name)
					domains := policy.EffectiveAllowedDomains(merged.Egress)
					if len(domains) == 0 {
						// Agent override cleared egress domains — fall back to RefreshAgent
						// to reconfigure this agent (removes proxy env vars, stops proxy container).
						if err := s.prov.RefreshAgent(ctx, name); err != nil {
							errors = append(errors, fmt.Sprintf("%s (refresh): %v", name, err))
						} else {
							deployed = append(deployed, name)
						}
						continue
					}
					mode := merged.Egress.Mode
					envoyConfig, err := policy.GenerateProxyConf(domains, mode)
					if err != nil {
						errors = append(errors, fmt.Sprintf("%s: %v", name, err))
						continue
					}

					if err := deployer.DeployEgress(ctx, name, policyContent, envoyConfig, mode); err != nil {
						errors = append(errors, fmt.Sprintf("%s: %v", name, err))
					} else {
						deployed = append(deployed, name)
					}
				}
			} else {
				// Fallback: refresh all (local/remote handle egress in their refresh flow)
				if agent != "" {
					if err := s.prov.RefreshAgent(ctx, agent); err != nil {
						return mcp.NewToolResultError(fmt.Sprintf("deploy to %q failed: %v", agent, err)), nil
					}
					deployed = targetAgents
				} else {
					if err := s.prov.RefreshAll(ctx); err != nil {
						return mcp.NewToolResultError(fmt.Sprintf("deploy failed: %v", err)), nil
					}
					deployed = targetAgents
				}
			}

			if len(errors) > 0 && len(deployed) == 0 {
				return mcp.NewToolResultError(fmt.Sprintf("deploy failed for all agents: %s", strings.Join(errors, "; "))), nil
			}
			result := deployResult{
				Validated:      true,
				Deployed:       deployed,
				Errors:         errors,
				PartialFailure: len(errors) > 0,
			}
			return jsonResult(result)
		},
	}
}
