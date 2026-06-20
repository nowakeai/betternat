package config

// Config is the top-level betternat-agent configuration.
type Config struct {
	Version       string              `json:"version" yaml:"version"`
	GatewayID     string              `json:"gateway_id" yaml:"gateway_id"`
	HAGroupID     string              `json:"ha_group_id" yaml:"ha_group_id"`
	Cloud         string              `json:"cloud" yaml:"cloud"`
	Region        string              `json:"region" yaml:"region"`
	Local         LocalConfig         `json:"local" yaml:"local"`
	Datapath      DatapathConfig      `json:"datapath" yaml:"datapath"`
	HA            HAConfig            `json:"ha" yaml:"ha"`
	Observability ObservabilityConfig `json:"observability" yaml:"observability"`
	Rollback      RollbackConfig      `json:"rollback" yaml:"rollback"`
}

type LocalConfig struct {
	InstanceID       string `json:"instance_id" yaml:"instance_id"`
	AvailabilityZone string `json:"availability_zone" yaml:"availability_zone"`
	PrimaryInterface string `json:"primary_interface" yaml:"primary_interface"`
}

type DatapathConfig struct {
	Engine         string         `json:"engine" yaml:"engine"`
	FallbackEngine string         `json:"fallback_engine" yaml:"fallback_engine"`
	PrivateCIDRs   []string       `json:"private_cidrs" yaml:"private_cidrs"`
	LoxiLB         LoxiLBConfig   `json:"loxilb" yaml:"loxilb"`
	Nftables       NftablesConfig `json:"nftables" yaml:"nftables"`
}

type LoxiLBConfig struct {
	APIAddress               string `json:"api_address" yaml:"api_address"`
	APIPort                  int    `json:"api_port" yaml:"api_port"`
	SNATTo                   string `json:"snat_to" yaml:"snat_to"`
	SNATInterface            string `json:"snat_interface" yaml:"snat_interface"`
	RulePreferenceBase       int    `json:"rule_preference_base" yaml:"rule_preference_base"`
	ReconcileIntervalSeconds int    `json:"reconcile_interval_seconds" yaml:"reconcile_interval_seconds"`
}

type NftablesConfig struct {
	TableName   string `json:"table_name" yaml:"table_name"`
	ChainPrefix string `json:"chain_prefix" yaml:"chain_prefix"`
}

type HAConfig struct {
	Enabled        bool                 `json:"enabled" yaml:"enabled"`
	Lease          LeaseConfig          `json:"lease" yaml:"lease"`
	RouteFailover  RouteFailoverConfig  `json:"route_failover" yaml:"route_failover"`
	PublicIdentity PublicIdentityConfig `json:"public_identity" yaml:"public_identity"`
}

type LeaseConfig struct {
	Backend              string `json:"backend" yaml:"backend"`
	Table                string `json:"table" yaml:"table"`
	Key                  string `json:"key" yaml:"key"`
	TTLSeconds           int    `json:"ttl_seconds" yaml:"ttl_seconds"`
	RenewIntervalSeconds int    `json:"renew_interval_seconds" yaml:"renew_interval_seconds"`
}

type RouteFailoverConfig struct {
	Mode            string   `json:"mode" yaml:"mode"`
	RouteTableIDs   []string `json:"route_table_ids" yaml:"route_table_ids"`
	DestinationCIDR string   `json:"destination_cidr" yaml:"destination_cidr"`
	TargetType      string   `json:"target_type" yaml:"target_type"`
}

type PublicIdentityConfig struct {
	Mode         string `json:"mode" yaml:"mode"`
	AllocationID string `json:"allocation_id" yaml:"allocation_id"`
}

type ObservabilityConfig struct {
	Prometheus    PrometheusConfig    `json:"prometheus" yaml:"prometheus"`
	Attribution   AttributionConfig   `json:"attribution" yaml:"attribution"`
	OutboundProbe OutboundProbeConfig `json:"outbound_probe" yaml:"outbound_probe"`
}

type PrometheusConfig struct {
	ListenAddress string `json:"listen_address" yaml:"listen_address"`
	ListenPort    int    `json:"listen_port" yaml:"listen_port"`
}

type AttributionConfig struct {
	Owners []OwnerConfig `json:"owners" yaml:"owners"`
}

type OutboundProbeConfig struct {
	Enabled    bool   `json:"enabled" yaml:"enabled"`
	URL        string `json:"url" yaml:"url"`
	ExpectedIP string `json:"expected_ip" yaml:"expected_ip"`
}

type OwnerConfig struct {
	Name  string   `json:"name" yaml:"name"`
	CIDRs []string `json:"cidrs" yaml:"cidrs"`
}

type RollbackConfig struct {
	PreviousRouteTargets map[string]PreviousRouteTarget `json:"previous_route_targets" yaml:"previous_route_targets"`
}

type PreviousRouteTarget struct {
	DestinationCIDR string `json:"destination_cidr" yaml:"destination_cidr"`
	Target          string `json:"target" yaml:"target"`
}
