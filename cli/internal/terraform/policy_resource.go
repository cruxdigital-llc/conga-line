package terraform

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cruxdigital-llc/conga-line/cli/internal/policy"
	congaprovider "github.com/cruxdigital-llc/conga-line/cli/internal/provider"
)

var (
	_ resource.Resource                = &policyResource{}
	_ resource.ResourceWithImportState = &policyResource{}
)

type policyResource struct {
	prov    congaprovider.Provider
	dataDir string
}

type policyResourceModel struct {
	ID types.String `tfsdk:"id"`

	// Egress
	EgressMode           types.String `tfsdk:"egress_mode"`
	EgressAllowedDomains types.List   `tfsdk:"egress_allowed_domains"`
	EgressBlockedDomains types.List   `tfsdk:"egress_blocked_domains"`

	// Posture
	PostureIsolationLevel       types.String `tfsdk:"posture_isolation_level"`
	PostureSecretsBackend       types.String `tfsdk:"posture_secrets_backend"`
	PostureMonitoring           types.String `tfsdk:"posture_monitoring"`
	PostureComplianceFrameworks types.List   `tfsdk:"posture_compliance_frameworks"`

	// Routing
	RoutingDefaultModel types.String `tfsdk:"routing_default_model"`

	// Per-agent overrides
	AgentOverrides []agentOverrideModel `tfsdk:"agent_override"`
}

type agentOverrideModel struct {
	Name                 types.String `tfsdk:"name"`
	EgressMode           types.String `tfsdk:"egress_mode"`
	EgressAllowedDomains types.List   `tfsdk:"egress_allowed_domains"`
	EgressBlockedDomains types.List   `tfsdk:"egress_blocked_domains"`
	RoutingDefaultModel  types.String `tfsdk:"routing_default_model"`
}

func NewPolicyResource() resource.Resource {
	return &policyResource{}
}

func (r *policyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_policy"
}

func (r *policyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages the CongaLine policy (egress, routing, posture).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Policy identifier (always 'policy').",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},

			// Egress
			"egress_mode": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: `Egress enforcement mode: "enforce" (default) or "validate".`,
				Validators: []validator.String{
					stringvalidator.OneOf("enforce", "validate"),
				},
			},
			"egress_allowed_domains": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "List of allowed external domains (supports wildcards like *.example.com).",
			},
			"egress_blocked_domains": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "List of blocked domains (takes precedence over allowed).",
			},

			// Posture
			"posture_isolation_level": schema.StringAttribute{
				Optional:    true,
				Description: `Isolation level: "standard", "hardened", or "segmented".`,
				Validators: []validator.String{
					stringvalidator.OneOf("standard", "hardened", "segmented"),
				},
			},
			"posture_secrets_backend": schema.StringAttribute{
				Optional:    true,
				Description: `Secrets backend: "file", "managed", or "proxy".`,
				Validators: []validator.String{
					stringvalidator.OneOf("file", "managed", "proxy"),
				},
			},
			"posture_monitoring": schema.StringAttribute{
				Optional:    true,
				Description: `Monitoring level: "basic", "standard", or "full".`,
				Validators: []validator.String{
					stringvalidator.OneOf("basic", "standard", "full"),
				},
			},
			"posture_compliance_frameworks": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: `Compliance frameworks (e.g. "SOC2", "HIPAA").`,
			},

			// Routing
			"routing_default_model": schema.StringAttribute{
				Optional:    true,
				Description: "Default model for agent routing.",
			},
		},
		Blocks: map[string]schema.Block{
			"agent_override": schema.ListNestedBlock{
				Description: "Per-agent policy overrides. Each block replaces (not merges) the corresponding global section for that agent.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Required:    true,
							Description: "Agent name to override policy for.",
						},
						"egress_mode": schema.StringAttribute{
							Optional:    true,
							Description: `Egress mode override: "enforce" or "validate".`,
							Validators: []validator.String{
								stringvalidator.OneOf("enforce", "validate"),
							},
						},
						"egress_allowed_domains": schema.ListAttribute{
							Optional:    true,
							ElementType: types.StringType,
							Description: "Allowed domains override for this agent.",
						},
						"egress_blocked_domains": schema.ListAttribute{
							Optional:    true,
							ElementType: types.StringType,
							Description: "Blocked domains override for this agent.",
						},
						"routing_default_model": schema.StringAttribute{
							Optional:    true,
							Description: "Default model override for this agent.",
						},
					},
				},
			},
		},
	}
}

func (r *policyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	p, ok := req.ProviderData.(*congaProvider)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *congaProvider, got %T", req.ProviderData),
		)
		return
	}
	r.prov = p.prov
	r.dataDir = p.dataDir
}

func (r *policyResource) policyPath() string {
	return filepath.Join(r.dataDir, "conga-policy.yaml")
}

func (r *policyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan policyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	pf := r.buildPolicyFile(ctx, plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := policy.Save(pf, r.policyPath()); err != nil {
		resp.Diagnostics.AddError("Failed to save policy", err.Error())
		return
	}

	if err := r.prov.RefreshAll(ctx); err != nil {
		resp.Diagnostics.AddError("Failed to deploy policy", err.Error())
		return
	}

	plan.ID = types.StringValue("policy")
	if plan.EgressMode.IsNull() || plan.EgressMode.IsUnknown() {
		plan.EgressMode = types.StringValue(string(policy.EgressModeEnforce))
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *policyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state policyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	pf, err := policy.Load(r.policyPath())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read policy", err.Error())
		return
	}
	if pf == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	r.readPolicyToState(ctx, pf, &state, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *policyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan policyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	pf := r.buildPolicyFile(ctx, plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := policy.Save(pf, r.policyPath()); err != nil {
		resp.Diagnostics.AddError("Failed to save policy", err.Error())
		return
	}

	if err := r.prov.RefreshAll(ctx); err != nil {
		resp.Diagnostics.AddError("Failed to deploy policy", err.Error())
		return
	}

	plan.ID = types.StringValue("policy")
	if plan.EgressMode.IsNull() || plan.EgressMode.IsUnknown() {
		plan.EgressMode = types.StringValue(string(policy.EgressModeEnforce))
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *policyResource) Delete(ctx context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	if err := os.Remove(r.policyPath()); err != nil && !os.IsNotExist(err) {
		resp.Diagnostics.AddError("Failed to remove policy file", err.Error())
		return
	}

	// Best-effort refresh to clear policy enforcement.
	// During terraform destroy, agents may already be gone — that's expected.
	if err := r.prov.RefreshAll(ctx); err != nil && !isNotFoundErr(err) {
		resp.Diagnostics.AddWarning(
			"Policy file deleted but agent refresh failed",
			fmt.Sprintf("Agents may still be running with the old policy. Error: %s", err),
		)
	}
}

func (r *policyResource) ImportState(ctx context.Context, _ resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), "policy")...)
}

func (r *policyResource) buildPolicyFile(ctx context.Context, model policyResourceModel, diags *diag.Diagnostics) *policy.PolicyFile {
	pf := &policy.PolicyFile{
		APIVersion: policy.CurrentAPIVersion,
	}

	// Egress
	hasEgress := !model.EgressMode.IsNull() || !model.EgressAllowedDomains.IsNull() || !model.EgressBlockedDomains.IsNull()
	if hasEgress {
		pf.Egress = &policy.EgressPolicy{}
		if !model.EgressMode.IsNull() {
			pf.Egress.Mode = policy.EgressMode(model.EgressMode.ValueString())
		}
		if !model.EgressAllowedDomains.IsNull() {
			var domains []string
			diags.Append(model.EgressAllowedDomains.ElementsAs(ctx, &domains, false)...)
			pf.Egress.AllowedDomains = domains
		}
		if !model.EgressBlockedDomains.IsNull() {
			var domains []string
			diags.Append(model.EgressBlockedDomains.ElementsAs(ctx, &domains, false)...)
			pf.Egress.BlockedDomains = domains
		}
	}

	// Posture
	hasPosture := !model.PostureIsolationLevel.IsNull() || !model.PostureSecretsBackend.IsNull() ||
		!model.PostureMonitoring.IsNull() || !model.PostureComplianceFrameworks.IsNull()
	if hasPosture {
		pf.Posture = &policy.PostureDeclarations{}
		if !model.PostureIsolationLevel.IsNull() {
			pf.Posture.IsolationLevel = model.PostureIsolationLevel.ValueString()
		}
		if !model.PostureSecretsBackend.IsNull() {
			pf.Posture.SecretsBackend = model.PostureSecretsBackend.ValueString()
		}
		if !model.PostureMonitoring.IsNull() {
			pf.Posture.Monitoring = model.PostureMonitoring.ValueString()
		}
		if !model.PostureComplianceFrameworks.IsNull() {
			var frameworks []string
			diags.Append(model.PostureComplianceFrameworks.ElementsAs(ctx, &frameworks, false)...)
			pf.Posture.ComplianceFrameworks = frameworks
		}
	}

	// Routing
	if !model.RoutingDefaultModel.IsNull() {
		pf.Routing = &policy.RoutingPolicy{
			DefaultModel: model.RoutingDefaultModel.ValueString(),
		}
	}

	// Per-agent overrides
	if len(model.AgentOverrides) > 0 {
		pf.Agents = make(map[string]*policy.AgentOverride, len(model.AgentOverrides))
		for _, ao := range model.AgentOverrides {
			name := ao.Name.ValueString()
			override := &policy.AgentOverride{}

			hasEgressOverride := !ao.EgressMode.IsNull() || !ao.EgressAllowedDomains.IsNull() || !ao.EgressBlockedDomains.IsNull()
			if hasEgressOverride {
				override.Egress = &policy.EgressPolicy{}
				if !ao.EgressMode.IsNull() {
					override.Egress.Mode = policy.EgressMode(ao.EgressMode.ValueString())
				}
				if !ao.EgressAllowedDomains.IsNull() {
					var domains []string
					diags.Append(ao.EgressAllowedDomains.ElementsAs(ctx, &domains, false)...)
					override.Egress.AllowedDomains = domains
				}
				if !ao.EgressBlockedDomains.IsNull() {
					var domains []string
					diags.Append(ao.EgressBlockedDomains.ElementsAs(ctx, &domains, false)...)
					override.Egress.BlockedDomains = domains
				}
			}

			if !ao.RoutingDefaultModel.IsNull() {
				override.Routing = &policy.RoutingPolicy{
					DefaultModel: ao.RoutingDefaultModel.ValueString(),
				}
			}

			pf.Agents[name] = override
		}
	}

	return pf
}

func (r *policyResource) readPolicyToState(ctx context.Context, pf *policy.PolicyFile, state *policyResourceModel, diags *diag.Diagnostics) {
	state.ID = types.StringValue("policy")

	if pf.Egress != nil {
		state.EgressMode = types.StringValue(string(pf.Egress.Mode))
		if len(pf.Egress.AllowedDomains) > 0 {
			list, d := types.ListValueFrom(ctx, types.StringType, pf.Egress.AllowedDomains)
			diags.Append(d...)
			state.EgressAllowedDomains = list
		} else {
			state.EgressAllowedDomains = types.ListNull(types.StringType)
		}
		if len(pf.Egress.BlockedDomains) > 0 {
			list, d := types.ListValueFrom(ctx, types.StringType, pf.Egress.BlockedDomains)
			diags.Append(d...)
			state.EgressBlockedDomains = list
		} else {
			state.EgressBlockedDomains = types.ListNull(types.StringType)
		}
	} else {
		state.EgressMode = types.StringNull()
		state.EgressAllowedDomains = types.ListNull(types.StringType)
		state.EgressBlockedDomains = types.ListNull(types.StringType)
	}

	if pf.Posture != nil {
		state.PostureIsolationLevel = stringOrNull(pf.Posture.IsolationLevel)
		state.PostureSecretsBackend = stringOrNull(pf.Posture.SecretsBackend)
		state.PostureMonitoring = stringOrNull(pf.Posture.Monitoring)
		if len(pf.Posture.ComplianceFrameworks) > 0 {
			list, d := types.ListValueFrom(ctx, types.StringType, pf.Posture.ComplianceFrameworks)
			diags.Append(d...)
			state.PostureComplianceFrameworks = list
		} else {
			state.PostureComplianceFrameworks = types.ListNull(types.StringType)
		}
	} else {
		state.PostureIsolationLevel = types.StringNull()
		state.PostureSecretsBackend = types.StringNull()
		state.PostureMonitoring = types.StringNull()
		state.PostureComplianceFrameworks = types.ListNull(types.StringType)
	}

	if pf.Routing != nil {
		state.RoutingDefaultModel = stringOrNull(pf.Routing.DefaultModel)
	} else {
		state.RoutingDefaultModel = types.StringNull()
	}

	// Per-agent overrides (sorted by name for deterministic state)
	if len(pf.Agents) > 0 {
		names := make([]string, 0, len(pf.Agents))
		for name := range pf.Agents {
			names = append(names, name)
		}
		sort.Strings(names)

		state.AgentOverrides = make([]agentOverrideModel, 0, len(pf.Agents))
		for _, name := range names {
			override := pf.Agents[name]
			if override == nil {
				continue
			}
			ao := agentOverrideModel{
				Name: types.StringValue(name),
			}
			if override.Egress != nil {
				ao.EgressMode = stringOrNull(string(override.Egress.Mode))
				if len(override.Egress.AllowedDomains) > 0 {
					list, d := types.ListValueFrom(ctx, types.StringType, override.Egress.AllowedDomains)
					diags.Append(d...)
					ao.EgressAllowedDomains = list
				} else {
					ao.EgressAllowedDomains = types.ListNull(types.StringType)
				}
				if len(override.Egress.BlockedDomains) > 0 {
					list, d := types.ListValueFrom(ctx, types.StringType, override.Egress.BlockedDomains)
					diags.Append(d...)
					ao.EgressBlockedDomains = list
				} else {
					ao.EgressBlockedDomains = types.ListNull(types.StringType)
				}
			} else {
				ao.EgressMode = types.StringNull()
				ao.EgressAllowedDomains = types.ListNull(types.StringType)
				ao.EgressBlockedDomains = types.ListNull(types.StringType)
			}
			if override.Routing != nil {
				ao.RoutingDefaultModel = stringOrNull(override.Routing.DefaultModel)
			} else {
				ao.RoutingDefaultModel = types.StringNull()
			}
			state.AgentOverrides = append(state.AgentOverrides, ao)
		}
	} else {
		state.AgentOverrides = nil
	}
}

func stringOrNull(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}
