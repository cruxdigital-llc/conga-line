package terraform

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	congaprovider "github.com/cruxdigital-llc/conga-line/cli/internal/provider"
)

var _ datasource.DataSource = &channelsDataSource{}

type channelsDataSource struct {
	prov congaprovider.Provider
}

type channelsDataSourceModel struct {
	ID       types.String             `tfsdk:"id"`
	Channels []channelDataSourceEntry `tfsdk:"channels"`
}

type channelDataSourceEntry struct {
	Platform      types.String `tfsdk:"platform"`
	Configured    types.Bool   `tfsdk:"configured"`
	RouterRunning types.Bool   `tfsdk:"router_running"`
	BoundAgents   types.List   `tfsdk:"bound_agents"`
}

func NewChannelsDataSource() datasource.DataSource {
	return &channelsDataSource{}
}

func (d *channelsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_channels"
}

func (d *channelsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists all configured messaging channels.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Always 'channels'.",
			},
			"channels": schema.ListNestedAttribute{
				Computed:    true,
				Description: "List of channel statuses.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"platform": schema.StringAttribute{
							Computed:    true,
							Description: "Channel platform name.",
						},
						"configured": schema.BoolAttribute{
							Computed:    true,
							Description: "Whether credentials are present.",
						},
						"router_running": schema.BoolAttribute{
							Computed:    true,
							Description: "Whether the router is running.",
						},
						"bound_agents": schema.ListAttribute{
							Computed:    true,
							ElementType: types.StringType,
							Description: "Agent names bound to this channel.",
						},
					},
				},
			},
		},
	}
}

func (d *channelsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	p, ok := req.ProviderData.(*congaProvider)
	if !ok {
		resp.Diagnostics.AddError("Unexpected DataSource Configure Type", fmt.Sprintf("Expected *congaProvider, got %T", req.ProviderData))
		return
	}
	d.prov = p.prov
}

func (d *channelsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	channelList, err := d.prov.ListChannels(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list channels", err.Error())
		return
	}

	var state channelsDataSourceModel
	state.ID = types.StringValue("channels")

	for _, ch := range channelList {
		agents, diags := types.ListValueFrom(ctx, types.StringType, ch.BoundAgents)
		resp.Diagnostics.Append(diags...)

		state.Channels = append(state.Channels, channelDataSourceEntry{
			Platform:      types.StringValue(ch.Platform),
			Configured:    types.BoolValue(ch.Configured),
			RouterRunning: types.BoolValue(ch.RouterRunning),
			BoundAgents:   agents,
		})
	}

	if state.Channels == nil {
		state.Channels = []channelDataSourceEntry{}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}
