package terraform

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cruxdigital-llc/conga-line/cli/internal/channels"
	congaprovider "github.com/cruxdigital-llc/conga-line/cli/internal/provider"
)

var (
	_ resource.Resource                = &channelBindingResource{}
	_ resource.ResourceWithImportState = &channelBindingResource{}
)

type channelBindingResource struct {
	prov congaprovider.Provider
}

type channelBindingResourceModel struct {
	ID       types.String `tfsdk:"id"`
	Agent    types.String `tfsdk:"agent"`
	Platform types.String `tfsdk:"platform"`
	BindingID types.String `tfsdk:"binding_id"`
	Label    types.String `tfsdk:"label"`
}

func NewChannelBindingResource() resource.Resource {
	return &channelBindingResource{}
}

func (r *channelBindingResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_channel_binding"
}

func (r *channelBindingResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Binds a messaging channel to a CongaLine agent.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Binding identifier (agent/platform).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"agent": schema.StringAttribute{
				Required:    true,
				Description: "Agent name to bind the channel to.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"platform": schema.StringAttribute{
				Required:    true,
				Description: `Channel platform (e.g. "slack").`,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"binding_id": schema.StringAttribute{
				Required:    true,
				Description: "Platform-specific ID (e.g. Slack member ID or channel ID).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"label": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Human-readable label for the binding.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *channelBindingResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.prov = extractProvider(req, resp)
}

func (r *channelBindingResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan channelBindingResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	binding := channels.ChannelBinding{
		Platform: plan.Platform.ValueString(),
		ID:       plan.BindingID.ValueString(),
	}
	if !plan.Label.IsNull() {
		binding.Label = plan.Label.ValueString()
	}

	agentName := plan.Agent.ValueString()
	if err := r.prov.BindChannel(ctx, agentName, binding); err != nil {
		// If binding already exists, treat as success (idempotent create).
		if !strings.Contains(err.Error(), "already has a") {
			resp.Diagnostics.AddError("Failed to bind channel", err.Error())
			return
		}
	}

	plan.ID = types.StringValue(agentName + "/" + binding.Platform)
	if plan.Label.IsNull() || plan.Label.IsUnknown() {
		plan.Label = types.StringValue("")
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *channelBindingResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state channelBindingResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	agent, err := r.prov.GetAgent(ctx, state.Agent.ValueString())
	if err != nil {
		resp.State.RemoveResource(ctx)
		return
	}

	binding := agent.ChannelBinding(state.Platform.ValueString())
	if binding == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	state.BindingID = types.StringValue(binding.ID)
	state.Label = types.StringValue(binding.Label)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *channelBindingResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	// All mutable fields require replace.
	resp.Diagnostics.AddError("Binding update not supported", "Channel bindings are immutable. Change any field to force recreation.")
}

func (r *channelBindingResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state channelBindingResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.prov.UnbindChannel(ctx, state.Agent.ValueString(), state.Platform.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to unbind channel", err.Error())
	}
}

func (r *channelBindingResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := splitImportID(req.ID, 2)
	if parts == nil {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected format: agent/platform, got: %s", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("agent"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("platform"), parts[1])...)
}
