package tfprovider

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/types"
	gcompute "google.golang.org/api/compute/v1"

	"github.com/nowakeai/betternat/internal/bootstrap"
	gcpinstall "github.com/nowakeai/betternat/internal/install/gcp"
	"github.com/nowakeai/betternat/internal/provider"
)

var _ resource.Resource = (*GCPGatewayResource)(nil)

type GCPGatewayResource struct{}

type GCPGatewayResourceModel struct {
	ID                          types.String `tfsdk:"id"`
	Name                        types.String `tfsdk:"name"`
	ProjectID                   types.String `tfsdk:"project_id"`
	Region                      types.String `tfsdk:"region"`
	Zone                        types.String `tfsdk:"zone"`
	Network                     types.String `tfsdk:"network"`
	Subnetwork                  types.String `tfsdk:"subnetwork"`
	ClientTag                   types.String `tfsdk:"client_tag"`
	RouteName                   types.String `tfsdk:"route_name"`
	RoutePriority               types.Int64  `tfsdk:"route_priority"`
	RouteDestRange              types.String `tfsdk:"route_destination_cidr"`
	MachineType                 types.String `tfsdk:"machine_type"`
	ImageProject                types.String `tfsdk:"image_project"`
	ImageFamily                 types.String `tfsdk:"image_family"`
	GatewayCount                types.Int64  `tfsdk:"gateway_count"`
	PrivateCIDRs                types.List   `tfsdk:"private_cidrs"`
	ServiceAccountEmail         types.String `tfsdk:"service_account_email"`
	RuntimeServiceAccountID     types.String `tfsdk:"runtime_service_account_id"`
	ManageRuntimeServiceAccount types.Bool   `tfsdk:"manage_runtime_service_account"`
	RuntimeIAMRoleID            types.String `tfsdk:"runtime_iam_role_id"`
	RuntimeIAMPermissions       types.List   `tfsdk:"runtime_iam_permissions"`
	ManageRuntimeIAM            types.Bool   `tfsdk:"manage_runtime_iam"`
	EnableAgentHA               types.Bool   `tfsdk:"enable_agent_ha"`
	BetterNATVersion            types.String `tfsdk:"betternat_version"`
	AgentBinaryURL              types.String `tfsdk:"agent_binary_url"`
	AgentBinarySHA256           types.String `tfsdk:"agent_binary_sha256"`
	CLIBinaryURL                types.String `tfsdk:"cli_binary_url"`
	CLIBinarySHA256             types.String `tfsdk:"cli_binary_sha256"`
	LoxiCMDBinaryURL            types.String `tfsdk:"loxicmd_binary_url"`
	LoxiCMDBinarySHA256         types.String `tfsdk:"loxicmd_binary_sha256"`
	FirestoreDatabaseID         types.String `tfsdk:"firestore_database_id"`
	FirestoreLocationID         types.String `tfsdk:"firestore_location_id"`
	ManageFirestoreDatabase     types.Bool   `tfsdk:"manage_firestore_database"`
	PeerAPIAuthToken            types.String `tfsdk:"peer_api_auth_token"`
	AgentConfigJSON             types.String `tfsdk:"agent_config_json"`
	AgentConfigHash             types.String `tfsdk:"agent_config_hash"`
	StartupScript               types.String `tfsdk:"startup_script"`
	GatewayStatuses             types.Map    `tfsdk:"gateway_statuses"`
	EgressPublicIPs             types.Map    `tfsdk:"egress_public_ips"`
	RouteTarget                 types.String `tfsdk:"route_target"`
	Status                      types.String `tfsdk:"status"`
}

func NewGCPGatewayResource() resource.Resource {
	return &GCPGatewayResource{}
}

func (r *GCPGatewayResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_gcp_gateway"
}

func (r *GCPGatewayResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "BetterNAT GCP alpha gateway resource. By default this manages GCE forwarding gateway VMs and a tagged default route for substrate validation. The experimental enable_agent_ha path renders BetterNAT agent bootstrap for Firestore-backed route-only HA. GCP remains alpha until raw LoxiLB comparison, failure injection, stable public identity, capacity repair, packaging, and release-contract gates are complete.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Base name for provider-owned gateway instances.",
			},
			"project_id": schema.StringAttribute{Required: true},
			"region":     schema.StringAttribute{Required: true},
			"zone":       schema.StringAttribute{Required: true},
			"network": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Existing VPC network name.",
			},
			"subnetwork": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Existing regional subnetwork name for gateway NICs.",
			},
			"client_tag": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Network tag applied to private client VMs that should use the BetterNAT route.",
			},
			"route_name": schema.StringAttribute{
				Optional: true,
				Computed: true,
			},
			"route_priority": schema.Int64Attribute{
				Optional: true,
				Computed: true,
				Default:  int64default.StaticInt64(800),
			},
			"route_destination_cidr": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("0.0.0.0/0"),
			},
			"machine_type": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("e2-small"),
			},
			"image_project": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("debian-cloud"),
			},
			"image_family": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("debian-12"),
			},
			"gateway_count": schema.Int64Attribute{
				Optional: true,
				Computed: true,
				Default:  int64default.StaticInt64(2),
			},
			"private_cidrs": schema.ListAttribute{
				ElementType:         types.StringType,
				Required:            true,
				MarkdownDescription: "Private CIDR ranges to masquerade on gateway instances.",
			},
			"service_account_email": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Runtime service account email attached to GCP gateway VMs. Required when enable_agent_ha is true unless manage_runtime_service_account derives it from runtime_service_account_id.",
			},
			"runtime_service_account_id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Project-local service account ID used when manage_runtime_service_account is true. Defaults to a sanitized name-derived ID.",
			},
			"manage_runtime_service_account": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				MarkdownDescription: "Experimental. When true with enable_agent_ha, the provider creates and deletes the runtime service account used by gateway VMs. Leave false when an infra-admin stack owns the service account.",
			},
			"runtime_iam_permissions": schema.ListAttribute{
				ElementType:         types.StringType,
				Computed:            true,
				MarkdownDescription: "Permissions required by the experimental GCP agent HA runtime service account.",
			},
			"manage_runtime_iam": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				MarkdownDescription: "Experimental. When true with enable_agent_ha, the provider creates or updates a project-level BetterNAT runtime custom role and binds service_account_email to it. The default role ID is derived from the gateway name. Leave false when IAM is managed outside this resource.",
			},
			"runtime_iam_role_id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Project-local custom role ID used when manage_runtime_iam is true. Defaults to a gateway-name-derived role ID so provider-owned IAM lifecycle is isolated per gateway.",
			},
			"enable_agent_ha": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				MarkdownDescription: "Experimental. When true, renders BetterNAT agent config and cloud-init user data for GCP route-only HA using Firestore coordination. This remains an alpha validation path until the remaining GCP release gates are complete.",
			},
			"betternat_version": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "BetterNAT runtime release tag used to derive linux_amd64 agent/CLI artifact URLs and checksums when enable_agent_ha is true.",
			},
			"agent_binary_url": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Sensitive:           true,
				MarkdownDescription: "Optional URL for the betternat-agent binary used by the GCP agent HA bootstrap path.",
			},
			"agent_binary_sha256": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Optional SHA256 checksum for agent_binary_url.",
			},
			"cli_binary_url": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Sensitive:           true,
				MarkdownDescription: "Optional URL for the BetterNAT CLI binary used by the GCP agent HA bootstrap path.",
			},
			"cli_binary_sha256": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Optional SHA256 checksum for cli_binary_url.",
			},
			"loxicmd_binary_url": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
			},
			"loxicmd_binary_sha256": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Optional SHA256 checksum for loxicmd_binary_url.",
			},
			"firestore_database_id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("(default)"),
				MarkdownDescription: "Firestore Native database ID used by the experimental GCP agent HA path.",
			},
			"firestore_location_id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Firestore Native database location used when manage_firestore_database is true. Defaults to region.",
			},
			"manage_firestore_database": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				MarkdownDescription: "Experimental. When true with enable_agent_ha, the provider creates and deletes the Firestore Native database used for GCP HA coordination. Leave false when the database is owned outside this resource.",
			},
			"peer_api_auth_token": schema.StringAttribute{
				Computed:            true,
				Sensitive:           true,
				MarkdownDescription: "Provider-generated bearer token rendered into the experimental GCP agent HA config for authenticated peer handover coordination.",
			},
			"agent_config_json": schema.StringAttribute{
				Computed:  true,
				Sensitive: true,
			},
			"agent_config_hash": schema.StringAttribute{Computed: true},
			"startup_script":    schema.StringAttribute{Computed: true, Sensitive: true},
			"gateway_statuses":  schema.MapAttribute{ElementType: types.StringType, Computed: true},
			"egress_public_ips": schema.MapAttribute{ElementType: types.StringType, Computed: true},
			"route_target":      schema.StringAttribute{Computed: true},
			"status":            schema.StringAttribute{Computed: true},
		},
	}
}

func (r *GCPGatewayResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan GCPGatewayResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	applier, inputs, err := gcpApplierAndInputs(ctx, &plan)
	if err != nil {
		resp.Diagnostics.AddError("Configure GCP gateway", err.Error())
		return
	}
	if err := applyGCPFirestoreDatabase(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Configure GCP Firestore database", err.Error())
		return
	}
	if err := applyGCPRuntimeServiceAccount(ctx, &plan); err != nil {
		if cleanupErr := cleanupGCPFirestoreDatabase(ctx, &plan); cleanupErr != nil {
			err = fmt.Errorf("%w; cleanup GCP Firestore database after failed service account setup: %v", err, cleanupErr)
		}
		resp.Diagnostics.AddError("Configure GCP runtime service account", err.Error())
		return
	}
	if err := applyGCPRuntimeIAM(ctx, &plan); err != nil {
		if cleanupErr := cleanupGCPRuntimeServiceAccount(ctx, &plan); cleanupErr != nil {
			err = fmt.Errorf("%w; cleanup GCP runtime service account after failed IAM setup: %v", err, cleanupErr)
		}
		if cleanupErr := cleanupGCPFirestoreDatabase(ctx, &plan); cleanupErr != nil {
			err = fmt.Errorf("%w; cleanup GCP Firestore database after failed IAM setup: %v", err, cleanupErr)
		}
		resp.Diagnostics.AddError("Configure GCP runtime IAM", err.Error())
		return
	}
	result, err := applier.Apply(ctx, inputs)
	if err != nil {
		if cleanupErr := cleanupGCPRuntimeIAM(ctx, &plan); cleanupErr != nil {
			err = fmt.Errorf("%w; cleanup GCP runtime IAM after failed create: %v", err, cleanupErr)
		}
		if cleanupErr := cleanupGCPRuntimeServiceAccount(ctx, &plan); cleanupErr != nil {
			err = fmt.Errorf("%w; cleanup GCP runtime service account after failed create: %v", err, cleanupErr)
		}
		if cleanupErr := cleanupGCPFirestoreDatabase(ctx, &plan); cleanupErr != nil {
			err = fmt.Errorf("%w; cleanup GCP Firestore database after failed create: %v", err, cleanupErr)
		}
		resp.Diagnostics.AddError("Create GCP gateway", err.Error())
		return
	}
	applyGCPResult(&plan, inputs, gcpinstall.ReadResult{
		GatewayInstances: result.GatewayInstances,
		EgressPublicIPs:  result.EgressPublicIPs,
		RouteTarget:      result.RouteTarget,
		Status:           "active",
	})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *GCPGatewayResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state GCPGatewayResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	applier, inputs, err := gcpApplierAndInputs(ctx, &state)
	if err != nil {
		resp.Diagnostics.AddError("Configure GCP gateway", err.Error())
		return
	}
	result, err := applier.Read(ctx, inputs)
	if err != nil {
		resp.Diagnostics.AddError("Read GCP gateway", err.Error())
		return
	}
	if result.Status == "missing" {
		resp.State.RemoveResource(ctx)
		return
	}
	applyGCPResult(&state, inputs, result)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *GCPGatewayResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"GCP gateway updates are not implemented",
		"Replace the betternat_gcp_gateway resource to change GCP alpha gateway topology. This avoids mutating route and gateway ownership until GCP lease coordination is implemented.",
	)
}

func (r *GCPGatewayResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state GCPGatewayResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	applier, inputs, err := gcpApplierAndInputs(ctx, &state)
	if err != nil {
		resp.Diagnostics.AddError("Configure GCP gateway cleanup", err.Error())
		return
	}
	if err := applier.Cleanup(ctx, inputs); err != nil {
		resp.Diagnostics.AddError("Delete GCP gateway", err.Error())
		return
	}
	if err := cleanupGCPRuntimeIAM(ctx, &state); err != nil {
		resp.Diagnostics.AddError("Delete GCP runtime IAM", err.Error())
		return
	}
	if err := cleanupGCPRuntimeServiceAccount(ctx, &state); err != nil {
		resp.Diagnostics.AddError("Delete GCP runtime service account", err.Error())
	}
	if err := cleanupGCPFirestoreDatabase(ctx, &state); err != nil {
		resp.Diagnostics.AddError("Delete GCP Firestore database", err.Error())
	}
}

func gcpApplierAndInputs(ctx context.Context, model *GCPGatewayResourceModel) (gcpinstall.Applier, gcpinstall.Inputs, error) {
	privateCIDRs, err := listStrings(ctx, model.PrivateCIDRs)
	if err != nil {
		return gcpinstall.Applier{}, gcpinstall.Inputs{}, err
	}
	service, err := gcompute.NewService(ctx)
	if err != nil {
		return gcpinstall.Applier{}, gcpinstall.Inputs{}, fmt.Errorf("create GCP compute service: %w", err)
	}
	if err := prepareGCPRuntimeServiceAccountPlan(model); err != nil {
		return gcpinstall.Applier{}, gcpinstall.Inputs{}, err
	}
	if err := prepareGCPRuntimeIAMPlan(model); err != nil {
		return gcpinstall.Applier{}, gcpinstall.Inputs{}, err
	}
	if err := prepareGCPFirestoreDatabasePlan(model); err != nil {
		return gcpinstall.Applier{}, gcpinstall.Inputs{}, err
	}
	inputs := gcpInputs(*model, privateCIDRs)
	if err := enrichGCPAgentBootstrap(model, &inputs, privateCIDRs); err != nil {
		return gcpinstall.Applier{}, gcpinstall.Inputs{}, err
	}
	return gcpinstall.Applier{Compute: service}, inputs, nil
}

func gcpInputs(model GCPGatewayResourceModel, privateCIDRs []string) gcpinstall.Inputs {
	name := model.Name.ValueString()
	routeName := stringDefault(model.RouteName, name+"-default-via-gateway")
	enableAgentHA := boolDefault(model.EnableAgentHA, false)
	inputs := gcpinstall.Inputs{
		Name:                name,
		ProjectID:           model.ProjectID.ValueString(),
		Region:              model.Region.ValueString(),
		Zone:                model.Zone.ValueString(),
		Network:             model.Network.ValueString(),
		Subnetwork:          model.Subnetwork.ValueString(),
		ClientTag:           model.ClientTag.ValueString(),
		RouteName:           routeName,
		RoutePriority:       int64Default(model.RoutePriority, 800),
		RouteDestRange:      stringDefault(model.RouteDestRange, "0.0.0.0/0"),
		MachineType:         stringDefault(model.MachineType, "e2-small"),
		ImageProject:        stringDefault(model.ImageProject, "debian-cloud"),
		ImageFamily:         stringDefault(model.ImageFamily, "debian-12"),
		GatewayCount:        int64Default(model.GatewayCount, 2),
		PrivateCIDRs:        privateCIDRs,
		ServiceAccountEmail: stringDefault(model.ServiceAccountEmail, ""),
		Labels: map[string]string{
			"betternat_name": sanitizeGCPLabel(name),
			"betternat":      "true",
		},
		OperationPollTime: 2 * time.Second,
	}
	if inputs.RoutePriority == 0 {
		inputs.RoutePriority = 800
	}
	if inputs.GatewayCount == 0 {
		inputs.GatewayCount = 2
	}
	if enableAgentHA {
		inputs.StartupScript = stringDefault(model.StartupScript, "")
	} else {
		inputs.StartupScript = gcpinstall.GatewayStartupScript(gcpinstall.StartupScriptInputs{PrivateCIDRs: privateCIDRs})
	}
	return inputs
}

func applyGCPResult(model *GCPGatewayResourceModel, inputs gcpinstall.Inputs, result gcpinstall.ReadResult) {
	applyGCPComputedPlan(model, inputs)
	model.ID = types.StringValue(fmt.Sprintf("%s/%s/%s", inputs.ProjectID, inputs.Zone, inputs.Name))
	model.GatewayStatuses = mustStringMap(result.GatewayInstances)
	model.EgressPublicIPs = mustStringMap(result.EgressPublicIPs)
	model.RouteTarget = types.StringValue(result.RouteTarget)
	model.Status = types.StringValue(result.Status)
}

func applyGCPComputedPlan(model *GCPGatewayResourceModel, inputs gcpinstall.Inputs) {
	model.RouteName = types.StringValue(inputs.RouteName)
	model.RoutePriority = types.Int64Value(inputs.RoutePriority)
	model.RouteDestRange = types.StringValue(inputs.RouteDestRange)
	model.MachineType = types.StringValue(inputs.MachineType)
	model.ImageProject = types.StringValue(inputs.ImageProject)
	model.ImageFamily = types.StringValue(inputs.ImageFamily)
	model.GatewayCount = types.Int64Value(inputs.GatewayCount)
	model.ServiceAccountEmail = types.StringValue(inputs.ServiceAccountEmail)
	model.RuntimeServiceAccountID = types.StringValue(stringDefault(model.RuntimeServiceAccountID, ""))
	model.RuntimeIAMRoleID = types.StringValue(stringDefault(model.RuntimeIAMRoleID, ""))
	model.ManageRuntimeServiceAccount = types.BoolValue(boolDefault(model.ManageRuntimeServiceAccount, false))
	model.RuntimeIAMPermissions = mustStringList(gcpinstall.RuntimeIAMPermissions())
	model.ManageRuntimeIAM = types.BoolValue(boolDefault(model.ManageRuntimeIAM, false))
	model.EnableAgentHA = types.BoolValue(boolDefault(model.EnableAgentHA, false))
	model.FirestoreDatabaseID = types.StringValue(stringDefault(model.FirestoreDatabaseID, "(default)"))
	model.FirestoreLocationID = types.StringValue(stringDefault(model.FirestoreLocationID, ""))
	model.ManageFirestoreDatabase = types.BoolValue(boolDefault(model.ManageFirestoreDatabase, false))
	model.StartupScript = types.StringValue(inputs.StartupScript)
}

func enrichGCPAgentBootstrap(model *GCPGatewayResourceModel, inputs *gcpinstall.Inputs, privateCIDRs []string) error {
	if !boolDefault(model.EnableAgentHA, false) {
		model.AgentConfigJSON = types.StringValue("")
		model.AgentConfigHash = types.StringValue("")
		return nil
	}
	if stringDefault(model.ServiceAccountEmail, "") == "" {
		return fmt.Errorf("enable_agent_ha requires service_account_email with Firestore and Compute route permissions")
	}
	artifacts, err := resolveGCPBootstrapArtifacts(model)
	if err != nil {
		return err
	}
	if artifacts.AgentBinaryURL == "" || artifacts.CLIBinaryURL == "" {
		return fmt.Errorf("enable_agent_ha requires betternat_version or explicit agent_binary_url and cli_binary_url")
	}
	peerAPIAuthToken, err := gcpPeerAPIAuthTokenForPlan(model)
	if err != nil {
		return err
	}
	agentConfig, err := provider.RenderAgentConfig(provider.GatewaySpec{
		Name:         inputs.Name,
		Cloud:        "gcp",
		Region:       inputs.Region,
		PrivateCIDRs: privateCIDRs,
		GCP: provider.GCPSpec{
			ProjectID:           inputs.ProjectID,
			Zone:                inputs.Zone,
			Network:             inputs.Network,
			ClientTag:           inputs.ClientTag,
			RoutePriority:       inputs.RoutePriority,
			FirestoreDatabaseID: stringDefault(model.FirestoreDatabaseID, "(default)"),
		},
		HA: provider.HASpec{
			Enabled:              true,
			LeaseBackend:         "firestore",
			TTLSeconds:           10,
			RenewSeconds:         1,
			RouteMode:            "replace_route",
			RouteDestinationCIDR: inputs.RouteDestRange,
			RouteTargetType:      "instance",
		},
		Coordination: provider.CoordinationSpec{
			Backend: "firestore",
		},
		Control: provider.ControlSpec{
			PeerAPIEnabled:       true,
			PeerAPIListenAddress: "0.0.0.0",
			PeerAPIListenPort:    9109,
			PeerAPIAuthToken:     peerAPIAuthToken,
		},
		Observability: provider.ObservabilitySpec{
			PrometheusListenAddress: "0.0.0.0",
			PrometheusListenPort:    9108,
			OutboundProbeURL:        "https://checkip.amazonaws.com",
		},
	}, provider.NodeSpec{
		HAGroupID:            inputs.Name + "-" + inputs.Zone,
		InstanceID:           "auto",
		AvailabilityZone:     inputs.Zone,
		PrimaryInterface:     "ens4",
		RouteTableIDs:        []string{inputs.RouteName},
		RouteDestinationCIDR: inputs.RouteDestRange,
	})
	if err != nil {
		return err
	}
	configBytes, err := json.Marshal(agentConfig)
	if err != nil {
		return fmt.Errorf("marshal GCP agent config: %w", err)
	}
	configHash := sha256.Sum256(configBytes)
	userData, err := bootstrap.RenderUserData(bootstrap.Spec{
		AgentConfig:         string(configBytes),
		AgentBinaryURL:      artifacts.AgentBinaryURL,
		AgentBinarySHA256:   artifacts.AgentBinarySHA256,
		CLIBinaryURL:        artifacts.CLIBinaryURL,
		CLIBinarySHA256:     artifacts.CLIBinarySHA256,
		LoxiCMDBinaryURL:    stringDefault(model.LoxiCMDBinaryURL, ""),
		LoxiCMDBinarySHA256: stringDefault(model.LoxiCMDBinarySHA256, ""),
		PrimaryInterface:    "ens4",
	})
	if err != nil {
		return err
	}
	inputs.StartupScript = userData
	model.AgentBinaryURL = types.StringValue(artifacts.AgentBinaryURL)
	model.AgentBinarySHA256 = types.StringValue(artifacts.AgentBinarySHA256)
	model.CLIBinaryURL = types.StringValue(artifacts.CLIBinaryURL)
	model.CLIBinarySHA256 = types.StringValue(artifacts.CLIBinarySHA256)
	model.PeerAPIAuthToken = types.StringValue(peerAPIAuthToken)
	model.AgentConfigJSON = types.StringValue(string(configBytes))
	model.AgentConfigHash = types.StringValue(hex.EncodeToString(configHash[:]))
	return nil
}

func gcpPeerAPIAuthTokenForPlan(model *GCPGatewayResourceModel) (string, error) {
	if !model.PeerAPIAuthToken.IsNull() && !model.PeerAPIAuthToken.IsUnknown() && model.PeerAPIAuthToken.ValueString() != "" {
		return model.PeerAPIAuthToken.ValueString(), nil
	}
	token := make([]byte, 32)
	if _, err := cryptorand.Read(token); err != nil {
		return "", fmt.Errorf("generate GCP peer API auth token: %w", err)
	}
	return hex.EncodeToString(token), nil
}

func resolveGCPBootstrapArtifacts(model *GCPGatewayResourceModel) (bootstrapArtifacts, error) {
	result := bootstrapArtifacts{
		AgentBinaryURL:    stringDefault(model.AgentBinaryURL, ""),
		AgentBinarySHA256: stringDefault(model.AgentBinarySHA256, ""),
		CLIBinaryURL:      stringDefault(model.CLIBinaryURL, ""),
		CLIBinarySHA256:   stringDefault(model.CLIBinarySHA256, ""),
	}
	version := stringDefault(model.BetterNATVersion, "")
	if version == "" {
		return result, nil
	}
	artifactSet, err := runtimeArtifacts(version, "linux", "amd64")
	if err != nil {
		return bootstrapArtifacts{}, err
	}
	if result.AgentBinaryURL == "" {
		result.AgentBinaryURL = artifactSet.AgentBinaryURL
	}
	if result.AgentBinarySHA256 == "" {
		result.AgentBinarySHA256 = artifactSet.AgentBinarySHA256
	}
	if result.CLIBinaryURL == "" {
		result.CLIBinaryURL = artifactSet.CLIBinaryURL
	}
	if result.CLIBinarySHA256 == "" {
		result.CLIBinarySHA256 = artifactSet.CLIBinarySHA256
	}
	return result, nil
}

func sanitizeGCPLabel(value string) string {
	out := make([]rune, 0, len(value))
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			out = append(out, r)
		} else if r >= 'A' && r <= 'Z' {
			out = append(out, r+'a'-'A')
		} else {
			out = append(out, '-')
		}
	}
	if len(out) == 0 {
		return "betternat"
	}
	if len(out) > 63 {
		out = out[:63]
	}
	return string(out)
}
