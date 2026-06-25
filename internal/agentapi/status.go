package agentapi

import "time"

const (
	DefaultHost         = "unix:///run/betternat/agent.sock"
	DefaultSocketPath   = "/run/betternat/agent.sock"
	StatusPath          = "/v1/status"
	HandoverPath        = "/v1/handover"
	HandoverPreparePath = "/v1/handover/prepare"
)

type CacheInfo struct {
	Mode       string    `json:"mode"`
	AgeSeconds float64   `json:"age_seconds"`
	Fresh      bool      `json:"fresh"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
}

type StatusResponse struct {
	SchemaVersion    string           `json:"schema_version"`
	GeneratedAt      time.Time        `json:"generated_at"`
	Cache            CacheInfo        `json:"cache"`
	GatewayID        string           `json:"gateway_id"`
	HAGroupID        string           `json:"ha_group_id"`
	Cloud            string           `json:"cloud"`
	Region           string           `json:"region"`
	AvailabilityZone string           `json:"availability_zone"`
	HAEnabled        bool             `json:"ha_enabled"`
	Datapath         string           `json:"datapath"`
	MetricsAddr      string           `json:"metrics_addr"`
	PublicIP         string           `json:"public_ip,omitempty"`
	RouteTarget      string           `json:"route_target,omitempty"`
	LeaseGeneration  uint64           `json:"lease_generation,omitempty"`
	LeaseExpiresIn   float64          `json:"lease_expires_in_seconds,omitempty"`
	RouteTargetMatch *bool            `json:"route_target_match,omitempty"`
	PublicIPMatch    *bool            `json:"public_ip_match,omitempty"`
	InstanceCount    int              `json:"instance_count"`
	DesiredCount     int32            `json:"desired_count,omitempty"`
	Instances        []StatusInstance `json:"instances"`
	Warnings         []string         `json:"warnings,omitempty"`
}

type StatusInstance struct {
	NodeID           string  `json:"node_id"`
	InstanceID       string  `json:"instance_id,omitempty"`
	Role             string  `json:"role"`
	Health           string  `json:"health,omitempty"`
	LifecycleState   string  `json:"lifecycle_state,omitempty"`
	PrivateIP        string  `json:"private_ip,omitempty"`
	PublicIP         string  `json:"public_ip,omitempty"`
	ControlURL       string  `json:"control_url,omitempty"`
	Version          string  `json:"version,omitempty"`
	RXMbps           float64 `json:"rx_mbps,omitempty"`
	TXMbps           float64 `json:"tx_mbps,omitempty"`
	Metrics          string  `json:"metrics,omitempty"`
	Fresh            bool    `json:"fresh,omitempty"`
	AgeSeconds       float64 `json:"age_seconds,omitempty"`
	LeaseGeneration  uint64  `json:"lease_generation,omitempty"`
	RouteTargetMatch *bool   `json:"route_target_match,omitempty"`
}

type HandoverRequest struct {
	RequestID        string `json:"request_id,omitempty"`
	TargetNodeID     string `json:"target_node_id"`
	TargetInstanceID string `json:"target_instance_id,omitempty"`
	Reason           string `json:"reason,omitempty"`
}

type HandoverResponse struct {
	SchemaVersion    string `json:"schema_version"`
	RequestID        string `json:"request_id,omitempty"`
	Status           string `json:"status"`
	SourceNodeID     string `json:"source_node_id"`
	TargetNodeID     string `json:"target_node_id,omitempty"`
	SourceInstanceID string `json:"source_instance_id,omitempty"`
	TargetInstanceID string `json:"target_instance_id,omitempty"`
	LeaseGeneration  uint64 `json:"lease_generation,omitempty"`
	Message          string `json:"message,omitempty"`
	Error            string `json:"error,omitempty"`
}

type HandoverPrepareRequest struct {
	RequestID        string `json:"request_id"`
	SourceNodeID     string `json:"source_node_id"`
	TargetNodeID     string `json:"target_node_id"`
	SourceInstanceID string `json:"source_instance_id,omitempty"`
	TargetInstanceID string `json:"target_instance_id,omitempty"`
	LeaseGeneration  uint64 `json:"lease_generation"`
	Reason           string `json:"reason,omitempty"`
}
