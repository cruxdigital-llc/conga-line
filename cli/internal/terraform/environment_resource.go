package terraform

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	congaprovider "github.com/cruxdigital-llc/conga-line/cli/internal/provider"
)

var (
	_ resource.Resource                = &environmentResource{}
	_ resource.ResourceWithImportState = &environmentResource{}
)

type environmentResource struct {
	prov congaprovider.Provider
}

type environmentResourceModel struct {
	ID            types.String `tfsdk:"id"`
	Image         types.String `tfsdk:"image"`
	InstallDocker types.Bool   `tfsdk:"install_docker"`
}

func NewEnvironmentResource() resource.Resource {
	return &environmentResource{}
}

func (r *environmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_environment"
}

func (r *environmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a CongaLine environment (shared infrastructure for agents).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Environment identifier (provider type name).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"image": schema.StringAttribute{
				Required:    true,
				Description: "Docker image for OpenClaw containers.",
			},
			"install_docker": schema.BoolAttribute{
				Optional:    true,
				Description: "Automatically install Docker if not present (remote/AWS).",
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *environmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.prov = extractProvider(req, resp)
}

func (r *environmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan environmentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	setupCfg := &congaprovider.SetupConfig{
		Image: plan.Image.ValueString(),
	}
	if !plan.InstallDocker.IsNull() {
		setupCfg.InstallDocker = plan.InstallDocker.ValueBool()
	}

	if err := r.prov.Setup(ctx, setupCfg); err != nil {
		resp.Diagnostics.AddError("Failed to set up environment", err.Error())
		return
	}

	plan.ID = types.StringValue(r.prov.Name())
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *environmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state environmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Best-effort existence check — ListAgents succeeding implies the provider
	// backend is reachable, not that the full environment is intact.
	_, err := r.prov.ListAgents(ctx)
	if err != nil {
		if isNotFoundErr(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read environment state", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *environmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan environmentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Setup is idempotent — re-running with updated image updates the config.
	setupCfg := &congaprovider.SetupConfig{
		Image: plan.Image.ValueString(),
	}
	if !plan.InstallDocker.IsNull() {
		setupCfg.InstallDocker = plan.InstallDocker.ValueBool()
	}

	if err := r.prov.Setup(ctx, setupCfg); err != nil {
		resp.Diagnostics.AddError("Failed to update environment", err.Error())
		return
	}

	plan.ID = types.StringValue(r.prov.Name())
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *environmentResource) Delete(ctx context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	if err := r.prov.Teardown(ctx); err != nil {
		resp.Diagnostics.AddError("Failed to tear down environment", err.Error())
	}
}

func (r *environmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
	resp.Diagnostics.AddWarning(
		"Image not imported",
		"The image attribute cannot be read from the provider. Set the correct image in your Terraform configuration and run `terraform apply` to reconcile state.",
	)
}
