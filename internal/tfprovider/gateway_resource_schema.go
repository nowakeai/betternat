package tfprovider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func (r *GatewayResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "BetterNAT AWS gateway resource. Installs AWS gateway node infrastructure and records runtime metadata.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
			},
			"name": schema.StringAttribute{
				Required: true,
			},
			"cloud": schema.StringAttribute{
				Optional:           true,
				Computed:           true,
				Default:            stringdefault.StaticString("aws"),
				DeprecationMessage: "betternat_aws_gateway is AWS-specific; the cloud attribute is retained only as computed compatibility state.",
			},
			"region": schema.StringAttribute{
				Required: true,
			},
			"vpc_id": schema.StringAttribute{
				Required: true,
			},
			"ami_id": schema.StringAttribute{
				MarkdownDescription: "Explicit Linux AMI ID for gateway nodes. Required for the cloud_init install path because BetterNAT does not currently resolve ami_channel into a public AMI.",
				Optional:            true,
			},
			"ami_channel": schema.StringAttribute{
				MarkdownDescription: "Reserved AMI channel selector. Accepted values are stable, candidate, and dev, but current installs should set ami_id explicitly.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("stable"),
			},
			"bootstrap_mode": schema.StringAttribute{
				MarkdownDescription: "Gateway node bootstrap mode. Use cloud_init for ordinary Linux AMIs that install BetterNAT at first boot. Use prebaked_ami only for BetterNAT AMIs that already contain Docker, LoxiLB, betternat, betternat-agent, loxicmd, and systemd units.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("cloud_init"),
			},
			"associate_public_ip_address": schema.BoolAttribute{
				MarkdownDescription: "Whether gateway node network interfaces should receive auto-assigned public IPv4 addresses. Leave unset to let the provider choose: cloud_init uses true, prebaked_ami with stable_egress_ip uses false, and prebaked_ami without stable_egress_ip uses true.",
				Optional:            true,
				Computed:            true,
			},
			"instance_type": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("t3.small"),
			},
			"use_spot": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
			},
			"min_size": schema.Int64Attribute{
				Optional: true,
				Computed: true,
			},
			"desired_capacity": schema.Int64Attribute{
				Optional: true,
				Computed: true,
			},
			"max_size": schema.Int64Attribute{
				Optional: true,
				Computed: true,
			},
			"betternat_version": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "BetterNAT runtime release tag used to derive agent/CLI GitHub Release artifact URLs and checksums for bootstrap installs. Example: v0.2.0. Explicit agent_binary_url, agent_binary_sha256, cli_binary_url, and cli_binary_sha256 values override derived values.",
			},
			"agent_binary_url": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Sensitive:           true,
				MarkdownDescription: "Optional URL for the betternat-agent binary. When betternat_version is set and this field is empty, the provider derives the URL from its built-in release artifact manifest.",
			},
			"agent_binary_sha256": schema.StringAttribute{
				Computed:            true,
				Optional:            true,
				MarkdownDescription: "Optional SHA256 checksum for agent_binary_url. When betternat_version is set and this field is empty, the provider derives the checksum from its built-in release artifact manifest.",
			},
			"cli_binary_url": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Sensitive:           true,
				MarkdownDescription: "Optional URL for the BetterNAT CLI binary installed on each gateway node. When betternat_version is set and this field is empty, the provider derives the URL from its built-in release artifact manifest.",
			},
			"cli_binary_sha256": schema.StringAttribute{
				Computed:            true,
				Optional:            true,
				MarkdownDescription: "Optional SHA256 checksum for cli_binary_url. When betternat_version is set and this field is empty, the provider derives the checksum from its built-in release artifact manifest.",
			},
			"loxicmd_binary_url": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
			},
			"loxicmd_binary_sha256": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Optional SHA256 checksum for loxicmd_binary_url. When set, cloud-init verifies the downloaded loxicmd before execution.",
			},
			"public_subnet_ids": schema.MapAttribute{
				ElementType: types.StringType,
				Required:    true,
			},
			"private_route_table_ids": schema.MapAttribute{
				ElementType: types.ListType{ElemType: types.StringType},
				Required:    true,
			},
			"private_cidrs": schema.ListAttribute{
				ElementType: types.StringType,
				Required:    true,
			},
			"datapath_engine": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("loxilb"),
			},
			"fallback_datapath_engine": schema.StringAttribute{
				MarkdownDescription: "Deprecated legacy compatibility field. Existing nftables code may remain temporarily for diagnostics, but BetterNAT has no supported product fallback datapath and LoxiLB readiness is required.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("nftables"),
			},
			"stable_egress_ip": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
			"ha_profile": schema.StringAttribute{
				MarkdownDescription: "High availability timing profile. Use default. Legacy values stable, balanced, and fast are accepted as aliases for default.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("default"),
			},
			"ha_lease_ttl_seconds": schema.Int64Attribute{
				MarkdownDescription: "Advanced override for the HA lease TTL in seconds. Leave unset to use ha_profile defaults.",
				Optional:            true,
				Computed:            true,
			},
			"ha_renew_interval_seconds": schema.Int64Attribute{
				MarkdownDescription: "Advanced override for the HA lease renew interval in seconds. Leave unset to use ha_profile defaults.",
				Optional:            true,
				Computed:            true,
			},
			"prometheus_enabled": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
			"route_mode": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("replace_route"),
			},
			"route_destination_cidr": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("0.0.0.0/0"),
			},
			"route_target_type": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("instance"),
			},
			"rollback_on_destroy": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
			"allow_destroy_without_rollback": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
			},
			"tags": schema.MapAttribute{
				ElementType: types.StringType,
				Optional:    true,
			},
			"lease_table_name": schema.StringAttribute{
				Computed: true,
			},
			"coordination_table_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Provider-owned coordination table used for HA lease, agent registry, and future backend-mediated agent coordination records.",
			},
			"peer_api_auth_token": schema.StringAttribute{
				Computed:            true,
				Sensitive:           true,
				MarkdownDescription: "Provider-generated peer API bearer token stored in state and rendered into node config for authenticated agent-to-agent handover coordination.",
			},
			"provider_infrastructure_revision": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(providerInfrastructureRevision),
				MarkdownDescription: "Internal provider-owned infrastructure revision. Provider upgrades may change this to trigger an in-place reconciliation of safe supporting resources such as IAM policy and coordination tables.",
			},
			"agent_config_json": schema.StringAttribute{
				Computed:  true,
				Sensitive: true,
			},
			"agent_config_hash": schema.StringAttribute{
				Computed: true,
			},
			"user_data": schema.StringAttribute{
				Computed:  true,
				Sensitive: true,
			},
			"install_plan_json": schema.StringAttribute{
				Computed: true,
			},
			"managed_route_table_ids": schema.ListAttribute{
				ElementType: types.StringType,
				Computed:    true,
			},
			"egress_public_ips": schema.MapAttribute{
				ElementType: types.StringType,
				Computed:    true,
			},
			"active_instance_ids": schema.MapAttribute{
				ElementType: types.StringType,
				Computed:    true,
			},
			"standby_instance_ids": schema.MapAttribute{
				ElementType: types.StringType,
				Computed:    true,
			},
			"rollback_route_targets_json": schema.StringAttribute{
				Computed: true,
			},
			"control_plane_status_json": schema.StringAttribute{
				Computed: true,
			},
			"status": schema.StringAttribute{
				Computed: true,
			},
		},
	}
}
