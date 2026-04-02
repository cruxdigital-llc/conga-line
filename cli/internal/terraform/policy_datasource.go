package terraform

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cruxdigital-llc/conga-line/cli/internal/policy"
)

var _ datasource.DataSource = &policyDataSource{}

type policyDataSource struct {
	dataDir string
}

type policyDataSourceModel struct {
	ID                   types.String `tfsdk:"id"`
	EgressMode           types.String `tfsdk:"egress_mode"`
	EgressAllowedDomains types.List   `tfsdk:"egress_allowed_domains"`
	EgressBlockedDomains types.List   `tfsdk:"egress_blocked_domains"`
	RoutingDefaultModel  types.String `tfsdk:"routing_default_model"`
}

func NewPolicyDataSource() datasource.DataSource {
	return &policyDataSource{}
}

func (d *policyDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_policy"
}

func (d *policyDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Reads the current CongaLine policy.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Always 'policy'.",
			},
			"egress_mode": schema.StringAttribute{
				Computed:    true,
				Description: "Egress enforcement mode.",
			},
			"egress_allowed_domains": schema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "Allowed external domains.",
			},
			"egress_blocked_domains": schema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "Blocked domains.",
			},
			"routing_default_model": schema.StringAttribute{
				Computed:    true,
				Description: "Default routing model.",
			},
		},
	}
}

func (d *policyDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	p, ok := req.ProviderData.(*congaProvider)
	if !ok {
		resp.Diagnostics.AddError("Unexpected DataSource Configure Type", fmt.Sprintf("Expected *congaProvider, got %T", req.ProviderData))
		return
	}
	d.dataDir = p.dataDir
}

func (d *policyDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	pf, err := policy.Load(filepath.Join(d.dataDir, "conga-policy.yaml"))
	if err != nil {
		resp.Diagnostics.AddError("Failed to read policy", err.Error())
		return
	}

	var state policyDataSourceModel
	state.ID = types.StringValue("policy")

	if pf == nil {
		state.EgressMode = types.StringNull()
		state.EgressAllowedDomains = types.ListNull(types.StringType)
		state.EgressBlockedDomains = types.ListNull(types.StringType)
		state.RoutingDefaultModel = types.StringNull()
	} else {
		if pf.Egress != nil {
			state.EgressMode = types.StringValue(string(pf.Egress.Mode))
			if len(pf.Egress.AllowedDomains) > 0 {
				list, diags := types.ListValueFrom(ctx, types.StringType, pf.Egress.AllowedDomains)
				resp.Diagnostics.Append(diags...)
				state.EgressAllowedDomains = list
			} else {
				state.EgressAllowedDomains = types.ListNull(types.StringType)
			}
			if len(pf.Egress.BlockedDomains) > 0 {
				list, diags := types.ListValueFrom(ctx, types.StringType, pf.Egress.BlockedDomains)
				resp.Diagnostics.Append(diags...)
				state.EgressBlockedDomains = list
			} else {
				state.EgressBlockedDomains = types.ListNull(types.StringType)
			}
		} else {
			state.EgressMode = types.StringNull()
			state.EgressAllowedDomains = types.ListNull(types.StringType)
			state.EgressBlockedDomains = types.ListNull(types.StringType)
		}

		if pf.Routing != nil {
			state.RoutingDefaultModel = stringOrNull(pf.Routing.DefaultModel)
		} else {
			state.RoutingDefaultModel = types.StringNull()
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}
