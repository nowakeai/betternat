package provider

import (
	"fmt"

	"github.com/betternat/betternat/internal/config"
)

type GatewaySpec struct {
	Name          string
	Cloud         string
	Region        string
	PrivateCIDRs  []string
	Datapath      DatapathSpec
	HA            HASpec
	Observability ObservabilitySpec
}

type DatapathSpec struct {
	Engine           string
	FallbackEngine   string
	LoxiLBAPIPort    int
	SNATInterface    string
	PreferenceBase   int
	ReconcileSeconds int
}

type HASpec struct {
	Enabled               bool
	LeaseBackend          string
	LeaseTable            string
	TTLSeconds            int
	RenewSeconds          int
	SharedEIPAllocationID string
	RouteMode             string
	RouteDestinationCIDR  string
	RouteTargetType       string
}

type ObservabilitySpec struct {
	PrometheusListenAddress string
	PrometheusListenPort    int
	OutboundProbeURL        string
	OutboundProbeExpectedIP string
}

type ApplianceSpec struct {
	HAGroupID            string
	InstanceID           string
	AvailabilityZone     string
	PrimaryInterface     string
	RouteTableIDs        []string
	RouteDestinationCIDR string
}

func RenderAgentConfig(gateway GatewaySpec, appliance ApplianceSpec) (config.Config, error) {
	if gateway.Name == "" {
		return config.Config{}, fmt.Errorf("gateway name is required")
	}
	if gateway.Cloud == "" {
		return config.Config{}, fmt.Errorf("cloud is required")
	}
	if gateway.Region == "" {
		return config.Config{}, fmt.Errorf("region is required")
	}
	if appliance.HAGroupID == "" {
		return config.Config{}, fmt.Errorf("appliance ha group id is required")
	}

	datapathEngine := defaultString(gateway.Datapath.Engine, "loxilb")
	fallbackEngine := defaultString(gateway.Datapath.FallbackEngine, "nftables")
	snatInterface := defaultString(gateway.Datapath.SNATInterface, appliance.PrimaryInterface)
	if snatInterface == "" {
		return config.Config{}, fmt.Errorf("snat interface is required")
	}
	primaryInterface := defaultString(appliance.PrimaryInterface, snatInterface)

	publicIdentity := config.PublicIdentityConfig{}
	if gateway.HA.SharedEIPAllocationID != "" {
		publicIdentity = config.PublicIdentityConfig{
			Mode:         "shared_eip",
			AllocationID: gateway.HA.SharedEIPAllocationID,
		}
	}

	cfg := config.Config{
		Version:   "v0",
		GatewayID: gateway.Name,
		HAGroupID: appliance.HAGroupID,
		Cloud:     gateway.Cloud,
		Region:    gateway.Region,
		Local: config.LocalConfig{
			InstanceID:       defaultString(appliance.InstanceID, "auto"),
			AvailabilityZone: defaultString(appliance.AvailabilityZone, "auto"),
			PrimaryInterface: primaryInterface,
		},
		Datapath: config.DatapathConfig{
			Engine:         datapathEngine,
			FallbackEngine: fallbackEngine,
			PrivateCIDRs:   append([]string(nil), gateway.PrivateCIDRs...),
			LoxiLB: config.LoxiLBConfig{
				APIAddress:               "127.0.0.1",
				APIPort:                  defaultInt(gateway.Datapath.LoxiLBAPIPort, 11111),
				SNATTo:                   "auto",
				SNATInterface:            snatInterface,
				RulePreferenceBase:       defaultInt(gateway.Datapath.PreferenceBase, 100),
				ReconcileIntervalSeconds: defaultInt(gateway.Datapath.ReconcileSeconds, 10),
			},
			Nftables: config.NftablesConfig{
				TableName:   "betternat",
				ChainPrefix: "betternat",
			},
		},
		HA: config.HAConfig{
			Enabled: gateway.HA.Enabled,
			Lease: config.LeaseConfig{
				Backend:              defaultString(gateway.HA.LeaseBackend, "dynamodb"),
				Table:                gateway.HA.LeaseTable,
				Key:                  appliance.HAGroupID,
				TTLSeconds:           defaultInt(gateway.HA.TTLSeconds, 10),
				RenewIntervalSeconds: defaultInt(gateway.HA.RenewSeconds, 3),
			},
			RouteFailover: config.RouteFailoverConfig{
				Mode:            defaultString(gateway.HA.RouteMode, "replace_route"),
				RouteTableIDs:   append([]string(nil), appliance.RouteTableIDs...),
				DestinationCIDR: defaultString(appliance.RouteDestinationCIDR, defaultString(gateway.HA.RouteDestinationCIDR, "0.0.0.0/0")),
				TargetType:      defaultString(gateway.HA.RouteTargetType, "instance"),
			},
			PublicIdentity: publicIdentity,
		},
		Observability: config.ObservabilityConfig{
			Prometheus: config.PrometheusConfig{
				ListenAddress: defaultString(gateway.Observability.PrometheusListenAddress, "0.0.0.0"),
				ListenPort:    defaultInt(gateway.Observability.PrometheusListenPort, 9108),
			},
			OutboundProbe: config.OutboundProbeConfig{
				Enabled:    gateway.Observability.OutboundProbeURL != "",
				URL:        gateway.Observability.OutboundProbeURL,
				ExpectedIP: gateway.Observability.OutboundProbeExpectedIP,
			},
		},
		Rollback: config.RollbackConfig{
			PreviousRouteTargets: map[string]config.PreviousRouteTarget{},
		},
	}
	if err := cfg.Validate(); err != nil {
		return config.Config{}, err
	}
	return cfg, nil
}

func defaultString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func defaultInt(value int, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}
