package terraform

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	congaprovider "github.com/cruxdigital-llc/conga-line/cli/internal/provider"
)

var _ datasource.DataSource = &agentStatusDataSource{}

type agentStatusDataSource struct {
	prov congaprovider.Provider
}

type agentStatusDataSourceModel struct {
	Name           types.String `tfsdk:"name"`
	Type           types.String `tfsdk:"type"`
	ServiceState   types.String `tfsdk:"service_state"`
	ReadyPhase     types.String `tfsdk:"ready_phase"`
	ContainerState types.String `tfsdk:"container_state"`
	MemoryUsage    types.String `tfsdk:"memory_usage"`
	CPUPercent     types.String `tfsdk:"cpu_percent"`
	RestartCount   types.Int64  `tfsdk:"restart_count"`
	GatewayPort    types.Int64  `tfsdk:"gateway_port"`
	Paused         types.Bool   `tfsdk:"paused"`
}

func NewAgentStatusDataSource() datasource.DataSource {
	return &agentStatusDataSource{}
}

func (d *agentStatusDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_agent_status"
}

func (d *agentStatusDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Reads the current status of a CongaLine agent.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Agent name to query.",
			},
			"type": schema.StringAttribute{
				Computed:    true,
				Description: "Agent type (user or team).",
			},
			"service_state": schema.StringAttribute{
				Computed:    true,
				Description: "Service state (running, stopped, not-found).",
			},
			"ready_phase": schema.StringAttribute{
				Computed:    true,
				Description: "Ready phase (starting, gateway up, slack loading, ready).",
			},
			"container_state": schema.StringAttribute{
				Computed:    true,
				Description: "Docker container state.",
			},
			"memory_usage": schema.StringAttribute{
				Computed:    true,
				Description: "Memory usage (human-readable).",
			},
			"cpu_percent": schema.StringAttribute{
				Computed:    true,
				Description: "CPU usage percentage.",
			},
			"restart_count": schema.Int64Attribute{
				Computed:    true,
				Description: "Container restart count.",
			},
			"gateway_port": schema.Int64Attribute{
				Computed:    true,
				Description: "Gateway port on host.",
			},
			"paused": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the agent is paused.",
			},
		},
	}
}

func (d *agentStatusDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *agentStatusDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config agentStatusDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := config.Name.ValueString()

	agent, err := d.prov.GetAgent(ctx, name)
	if err != nil {
		resp.Diagnostics.AddError("Failed to get agent", err.Error())
		return
	}

	status, err := d.prov.GetStatus(ctx, name)
	if err != nil {
		resp.Diagnostics.AddError("Failed to get agent status", err.Error())
		return
	}

	config.Type = types.StringValue(string(agent.Type))
	config.ServiceState = types.StringValue(status.ServiceState)
	config.ReadyPhase = types.StringValue(status.ReadyPhase)
	config.ContainerState = types.StringValue(status.Container.State)
	config.MemoryUsage = types.StringValue(status.Container.MemoryUsage)
	config.CPUPercent = types.StringValue(status.Container.CPUPercent)
	config.RestartCount = types.Int64Value(int64(status.Container.RestartCount))
	config.GatewayPort = types.Int64Value(int64(agent.GatewayPort))
	config.Paused = types.BoolValue(agent.Paused)

	resp.Diagnostics.Append(resp.State.Set(ctx, config)...)
}
