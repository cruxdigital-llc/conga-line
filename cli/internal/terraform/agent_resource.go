package terraform

import (
	"context"

	"github.com/cruxdigital-llc/conga-line/cli/internal/common"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	congaprovider "github.com/cruxdigital-llc/conga-line/cli/internal/provider"
)

var (
	_ resource.Resource                = &agentResource{}
	_ resource.ResourceWithImportState = &agentResource{}
)

type agentResource struct {
	prov congaprovider.Provider
}

type agentResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Type        types.String `tfsdk:"type"`
	GatewayPort types.Int64  `tfsdk:"gateway_port"`
	Paused      types.Bool   `tfsdk:"paused"`
}

func NewAgentResource() resource.Resource {
	return &agentResource{}
}

func (r *agentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_agent"
}

func (r *agentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a CongaLine agent (AI assistant container).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Agent identifier (same as name).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Unique agent name.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"type": schema.StringAttribute{
				Required:    true,
				Description: `Agent type: "user" (DM-only) or "team" (channel-based).`,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"gateway_port": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Description: "Gateway port on the host. Auto-assigned if omitted.",
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"paused": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the agent is paused.",
			},
		},
	}
}

func (r *agentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.prov = extractProvider(req, resp)
}

func (r *agentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan agentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Use explicit port if provided, otherwise auto-assign.
	var port int
	if !plan.GatewayPort.IsNull() && !plan.GatewayPort.IsUnknown() {
		port = int(plan.GatewayPort.ValueInt64())
	} else {
		existing, err := r.prov.ListAgents(ctx)
		if err != nil {
			resp.Diagnostics.AddError("Failed to list agents for port assignment", err.Error())
			return
		}
		port = common.NextAvailablePort(existing)
	}

	cfg := congaprovider.AgentConfig{
		Name:        plan.Name.ValueString(),
		Type:        congaprovider.AgentType(plan.Type.ValueString()),
		GatewayPort: port,
	}

	if err := r.prov.ProvisionAgent(ctx, cfg); err != nil {
		resp.Diagnostics.AddError("Failed to provision agent", err.Error())
		return
	}

	// Read back to get computed fields (gateway_port).
	agent, err := r.prov.GetAgent(ctx, cfg.Name)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read agent after provisioning", err.Error())
		return
	}

	plan.ID = types.StringValue(agent.Name)
	plan.GatewayPort = types.Int64Value(int64(agent.GatewayPort))
	plan.Paused = types.BoolValue(agent.Paused)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *agentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state agentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	agent, err := r.prov.GetAgent(ctx, state.Name.ValueString())
	if err != nil {
		resp.State.RemoveResource(ctx)
		return
	}

	state.ID = types.StringValue(agent.Name)
	state.Name = types.StringValue(agent.Name)
	state.Type = types.StringValue(string(agent.Type))
	state.GatewayPort = types.Int64Value(int64(agent.GatewayPort))
	state.Paused = types.BoolValue(agent.Paused)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *agentResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Agent is immutable — name and type both RequiresReplace.
	resp.Diagnostics.AddError("Agent update not supported", "Agents are immutable. Change name or type to force recreation.")
}

func (r *agentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state agentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.prov.RemoveAgent(ctx, state.Name.ValueString(), true); err != nil {
		resp.Diagnostics.AddError("Failed to remove agent", err.Error())
	}
}

func (r *agentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
