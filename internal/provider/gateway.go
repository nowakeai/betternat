package provider

import (
	"fmt"

	"github.com/nowakeai/betternat/internal/config"
)

type GatewaySpec struct {
	Name          string
	Cloud         string
	Region        string
	PrivateCIDRs  []string
	Datapath      DatapathSpec
	HA            HASpec
	Coordination  CoordinationSpec
	Control       ControlSpec
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

type CoordinationSpec struct {
	Backend                        string
	Table                          string
	RegistryRefreshIntervalSeconds int
	RegistryTTLSeconds             int
	HandoverTTLSeconds             int
}

type ControlSpec struct {
	PeerAPIEnabled       bool
	PeerAPIListenAddress string
	PeerAPIListenPort    int
	PeerAPIAuthToken     string
}

type ObservabilitySpec struct {
	PrometheusListenAddress string
	PrometheusListenPort    int
	OutboundProbeURL        string
	OutboundProbeExpectedIP string
}

type NodeSpec struct {
	HAGroupID            string
	InstanceID           string
	AvailabilityZone     string
	PrimaryInterface     string
	RouteTableIDs        []string
	RouteDestinationCIDR string
}

func RenderAgentConfig(gateway GatewaySpec, node NodeSpec) (config.Config, error) {
	if gateway.Name == "" {
		return config.Config{}, fmt.Errorf("gateway name is required")
	}
	if gateway.Cloud == "" {
		return config.Config{}, fmt.Errorf("cloud is required")
	}
	if gateway.Region == "" {
		return config.Config{}, fmt.Errorf("region is required")
	}
	if node.HAGroupID == "" {
		return config.Config{}, fmt.Errorf("node ha group id is required")
	}

	datapathEngine := defaultString(gateway.Datapath.Engine, "loxilb")
	fallbackEngine := defaultString(gateway.Datapath.FallbackEngine, "nftables")
	snatInterface := defaultString(gateway.Datapath.SNATInterface, node.PrimaryInterface)
	if snatInterface == "" {
		return config.Config{}, fmt.Errorf("snat interface is required")
	}
	primaryInterface := defaultString(node.PrimaryInterface, snatInterface)

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
		HAGroupID: node.HAGroupID,
		Cloud:     gateway.Cloud,
		Region:    gateway.Region,
		Local: config.LocalConfig{
			NodeID:           defaultString(node.InstanceID, "auto"),
			AvailabilityZone: defaultString(node.AvailabilityZone, "auto"),
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
				Key:                  node.HAGroupID,
				TTLSeconds:           defaultInt(gateway.HA.TTLSeconds, 10),
				RenewIntervalSeconds: defaultInt(gateway.HA.RenewSeconds, 3),
			},
			RouteFailover: config.RouteFailoverConfig{
				Mode:            defaultString(gateway.HA.RouteMode, "replace_route"),
				RouteTableIDs:   append([]string(nil), node.RouteTableIDs...),
				DestinationCIDR: defaultString(node.RouteDestinationCIDR, defaultString(gateway.HA.RouteDestinationCIDR, "0.0.0.0/0")),
				TargetType:      defaultString(gateway.HA.RouteTargetType, "instance"),
			},
			PublicIdentity: publicIdentity,
		},
		Coordination: config.CoordinationConfig{
			Backend:                        defaultString(gateway.Coordination.Backend, defaultString(gateway.HA.LeaseBackend, "dynamodb")),
			Table:                          gateway.Coordination.Table,
			RegistryRefreshIntervalSeconds: defaultInt(gateway.Coordination.RegistryRefreshIntervalSeconds, 5),
			RegistryTTLSeconds:             defaultInt(gateway.Coordination.RegistryTTLSeconds, 20),
			HandoverTTLSeconds:             defaultInt(gateway.Coordination.HandoverTTLSeconds, 3600),
		},
		Control: config.ControlConfig{
			PeerAPI: config.PeerAPIConfig{
				Enabled:       gateway.Control.PeerAPIEnabled,
				ListenAddress: defaultString(gateway.Control.PeerAPIListenAddress, "0.0.0.0"),
				ListenPort:    defaultInt(gateway.Control.PeerAPIListenPort, 9109),
				AuthToken:     gateway.Control.PeerAPIAuthToken,
			},
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
