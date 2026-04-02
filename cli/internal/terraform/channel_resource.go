package terraform

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	congaprovider "github.com/cruxdigital-llc/conga-line/cli/internal/provider"
)

var (
	_ resource.Resource                = &channelResource{}
	_ resource.ResourceWithImportState = &channelResource{}
)

type channelResource struct {
	prov congaprovider.Provider
}

type channelResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Platform       types.String `tfsdk:"platform"`
	BotToken       types.String `tfsdk:"bot_token"`
	SigningSecret   types.String `tfsdk:"signing_secret"`
	AppToken       types.String `tfsdk:"app_token"`
	Configured     types.Bool   `tfsdk:"configured"`
	RouterRunning  types.Bool   `tfsdk:"router_running"`
}

func NewChannelResource() resource.Resource {
	return &channelResource{}
}

func (r *channelResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_channel"
}

func (r *channelResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a messaging channel platform (e.g. Slack) for CongaLine agents.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Channel identifier (platform name).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"platform": schema.StringAttribute{
				Required:    true,
				Description: `Channel platform (e.g. "slack").`,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"bot_token": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Bot token for the platform.",
			},
			"signing_secret": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Signing secret for webhook verification.",
			},
			"app_token": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "App-level token (e.g. Slack Socket Mode).",
			},
			"configured": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the channel credentials are present.",
			},
			"router_running": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the router container is running.",
			},
		},
	}
}

func (r *channelResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.prov = extractProvider(req, resp)
}

func (r *channelResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan channelResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	secrets := r.buildSecrets(plan)
	platform := plan.Platform.ValueString()

	if err := r.prov.AddChannel(ctx, platform, secrets); err != nil {
		resp.Diagnostics.AddError("Failed to add channel", err.Error())
		return
	}

	r.readComputedState(ctx, plan.Platform.ValueString(), &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *channelResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state channelResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	channels, err := r.prov.ListChannels(ctx)
	if err != nil {
		resp.State.RemoveResource(ctx)
		return
	}

	found := false
	for _, ch := range channels {
		if ch.Platform == state.Platform.ValueString() {
			found = true
			state.Configured = types.BoolValue(ch.Configured)
			state.RouterRunning = types.BoolValue(ch.RouterRunning)
			break
		}
	}

	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *channelResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan channelResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// AddChannel is idempotent — re-adding overwrites secrets and restarts router.
	secrets := r.buildSecrets(plan)
	if err := r.prov.AddChannel(ctx, plan.Platform.ValueString(), secrets); err != nil {
		resp.Diagnostics.AddError("Failed to update channel", err.Error())
		return
	}

	r.readComputedState(ctx, plan.Platform.ValueString(), &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *channelResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state channelResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.prov.RemoveChannel(ctx, state.Platform.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to remove channel", err.Error())
	}
}

func (r *channelResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("platform"), req.ID)...)
}

func (r *channelResource) buildSecrets(model channelResourceModel) map[string]string {
	secrets := make(map[string]string)
	if !model.BotToken.IsNull() && !model.BotToken.IsUnknown() {
		secrets["slack-bot-token"] = model.BotToken.ValueString()
	}
	if !model.SigningSecret.IsNull() && !model.SigningSecret.IsUnknown() {
		secrets["slack-signing-secret"] = model.SigningSecret.ValueString()
	}
	if !model.AppToken.IsNull() && !model.AppToken.IsUnknown() {
		secrets["slack-app-token"] = model.AppToken.ValueString()
	}
	return secrets
}

func (r *channelResource) readComputedState(ctx context.Context, platform string, model *channelResourceModel) {
	model.ID = types.StringValue(platform)
	channels, err := r.prov.ListChannels(ctx)
	if err != nil {
		model.Configured = types.BoolValue(false)
		model.RouterRunning = types.BoolValue(false)
		return
	}
	for _, ch := range channels {
		if ch.Platform == platform {
			model.Configured = types.BoolValue(ch.Configured)
			model.RouterRunning = types.BoolValue(ch.RouterRunning)
			return
		}
	}
	model.Configured = types.BoolValue(false)
	model.RouterRunning = types.BoolValue(false)
}
