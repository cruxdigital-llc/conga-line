package terraform

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	congaprovider "github.com/cruxdigital-llc/conga-line/cli/internal/provider"

	// Register all provider implementations so provider.Get() can find them.
	_ "github.com/cruxdigital-llc/conga-line/cli/internal/provider/awsprovider"
	_ "github.com/cruxdigital-llc/conga-line/cli/internal/provider/localprovider"
	_ "github.com/cruxdigital-llc/conga-line/cli/internal/provider/remoteprovider"

	// Register all channel implementations so AddChannel() can find them.
	_ "github.com/cruxdigital-llc/conga-line/cli/internal/channels/slack"
)

var _ provider.Provider = &congaProvider{}

type congaProvider struct {
	version string
	prov    congaprovider.Provider
}

type congaProviderModel struct {
	ProviderType types.String `tfsdk:"provider_type"`
	DataDir      types.String `tfsdk:"data_dir"`
	SSHHost      types.String `tfsdk:"ssh_host"`
	SSHUser      types.String `tfsdk:"ssh_user"`
	SSHKeyPath   types.String `tfsdk:"ssh_key_path"`
	SSHPort      types.Int64  `tfsdk:"ssh_port"`
	Region       types.String `tfsdk:"region"`
	Profile      types.String `tfsdk:"profile"`
}

// New returns a factory function for the conga provider.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &congaProvider{version: version}
	}
}

func (p *congaProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "conga"
	resp.Version = p.version
}

func (p *congaProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manage CongaLine AI agent environments declaratively.",
		Attributes: map[string]schema.Attribute{
			"provider_type": schema.StringAttribute{
				Required:    true,
				Description: `Deployment target: "local", "remote", or "aws".`,
			},
			"data_dir": schema.StringAttribute{
				Optional:    true,
				Description: "Override the default data directory (~/.conga/).",
			},
			"ssh_host": schema.StringAttribute{
				Optional:    true,
				Description: "SSH hostname (remote provider).",
			},
			"ssh_user": schema.StringAttribute{
				Optional:    true,
				Description: "SSH user (remote provider, default: root).",
			},
			"ssh_key_path": schema.StringAttribute{
				Optional:    true,
				Description: "Path to SSH private key (remote provider).",
			},
			"ssh_port": schema.Int64Attribute{
				Optional:    true,
				Description: "SSH port (remote provider, default: 22).",
			},
			"region": schema.StringAttribute{
				Optional:    true,
				Description: "AWS region (aws provider).",
			},
			"profile": schema.StringAttribute{
				Optional:    true,
				Description: "AWS profile name (aws provider).",
			},
		},
	}
}

func (p *congaProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config congaProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	providerType := config.ProviderType.ValueString()
	if providerType == "" {
		resp.Diagnostics.AddError("Missing provider_type", "The provider_type attribute is required.")
		return
	}

	cfg := &congaprovider.Config{
		Provider: providerType,
	}
	if !config.DataDir.IsNull() {
		cfg.DataDir = config.DataDir.ValueString()
	}
	if !config.SSHHost.IsNull() {
		cfg.SSHHost = config.SSHHost.ValueString()
	}
	if !config.SSHUser.IsNull() {
		cfg.SSHUser = config.SSHUser.ValueString()
	}
	if !config.SSHKeyPath.IsNull() {
		cfg.SSHKeyPath = config.SSHKeyPath.ValueString()
	}
	if !config.SSHPort.IsNull() {
		cfg.SSHPort = int(config.SSHPort.ValueInt64())
	}
	if !config.Region.IsNull() {
		cfg.Region = config.Region.ValueString()
	}
	if !config.Profile.IsNull() {
		cfg.Profile = config.Profile.ValueString()
	}

	prov, err := congaprovider.Get(providerType, cfg)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to initialize provider",
			fmt.Sprintf("Could not create %q provider: %s", providerType, err),
		)
		return
	}

	p.prov = prov
	resp.DataSourceData = p
	resp.ResourceData = p
}

func (p *congaProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewEnvironmentResource,
		NewAgentResource,
		NewSecretResource,
		NewChannelResource,
		NewChannelBindingResource,
		NewPolicyResource,
	}
}

func (p *congaProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewAgentStatusDataSource,
		NewPolicyDataSource,
		NewChannelsDataSource,
	}
}
