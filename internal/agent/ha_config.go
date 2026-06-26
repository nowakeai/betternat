package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/nowakeai/betternat/internal/cloud"
	awscloud "github.com/nowakeai/betternat/internal/cloud/aws"
	gcpcloud "github.com/nowakeai/betternat/internal/cloud/gcp"
	"github.com/nowakeai/betternat/internal/config"
	"github.com/nowakeai/betternat/internal/coordination"
	dynamodbcoord "github.com/nowakeai/betternat/internal/coordination/dynamodb"
	firestorecoord "github.com/nowakeai/betternat/internal/coordination/firestore"
	"github.com/nowakeai/betternat/internal/lease"
	dynamodblease "github.com/nowakeai/betternat/internal/lease/dynamodb"
)

func validateHAConfig(cfg config.Config) error {
	if !cfg.HA.Enabled {
		return nil
	}
	switch cfg.Cloud {
	case "aws", "gcp":
	default:
		return fmt.Errorf("ha.enabled requires cloud=aws or cloud=gcp")
	}
	if cfg.Region == "" {
		return fmt.Errorf("ha.enabled requires region")
	}
	if cfg.Local.NodeID == "" || cfg.Local.NodeID == "auto" {
		return fmt.Errorf("ha.enabled requires resolved local.node_id")
	}
	if cfg.Cloud == "aws" {
		if cfg.HA.Lease.Backend != "dynamodb" {
			return fmt.Errorf("unsupported ha.lease.backend %q", cfg.HA.Lease.Backend)
		}
		if cfg.HA.Lease.Table == "" {
			return fmt.Errorf("ha.lease.table is required")
		}
	}
	if cfg.Cloud == "gcp" {
		if cfg.HA.Lease.Backend != "firestore" {
			return fmt.Errorf("unsupported ha.lease.backend %q for cloud=gcp", cfg.HA.Lease.Backend)
		}
		if cfg.GCP.ProjectID == "" {
			return fmt.Errorf("gcp.project_id is required for cloud=gcp HA")
		}
		if gcpZone(cfg) == "" {
			return fmt.Errorf("gcp.zone or local.availability_zone is required for cloud=gcp HA")
		}
		if cfg.GCP.Network == "" {
			return fmt.Errorf("gcp.network is required for cloud=gcp HA")
		}
		if cfg.GCP.ClientTag == "" {
			return fmt.Errorf("gcp.client_tag is required for cloud=gcp HA")
		}
		if cfg.Coordination.Backend != "" && cfg.Coordination.Backend != "firestore" {
			return fmt.Errorf("unsupported coordination.backend %q for cloud=gcp", cfg.Coordination.Backend)
		}
	}
	if leaseKey(cfg) == "" {
		return fmt.Errorf("ha.lease.key or ha_group_id is required")
	}
	if cfg.HA.RouteFailover.Mode == "" {
		return fmt.Errorf("ha.route_failover.mode is required")
	}
	if cfg.HA.RouteFailover.Mode != "replace_route" {
		return fmt.Errorf("unsupported ha.route_failover.mode %q", cfg.HA.RouteFailover.Mode)
	}
	if cfg.HA.RouteFailover.TargetType != "instance" {
		return fmt.Errorf("unsupported ha.route_failover.target_type %q", cfg.HA.RouteFailover.TargetType)
	}
	if cfg.HA.RouteFailover.DestinationCIDR == "" {
		return fmt.Errorf("ha.route_failover.destination_cidr is required")
	}
	if len(cfg.HA.RouteFailover.RouteTableIDs) == 0 {
		return fmt.Errorf("ha.route_failover.route_table_ids is required")
	}
	if cfg.Cloud == "aws" || cfg.Cloud == "gcp" {
		if cfg.HA.PublicIdentity.Mode == "shared_eip" && cfg.HA.PublicIdentity.AllocationID == "" {
			return fmt.Errorf("ha.public_identity.allocation_id is required for shared_eip")
		}
		if cfg.HA.PublicIdentity.Mode != "" && cfg.HA.PublicIdentity.Mode != "shared_eip" {
			return fmt.Errorf("unsupported ha.public_identity.mode %q", cfg.HA.PublicIdentity.Mode)
		}
	}
	return nil
}

func defaultCloudProvider(ctx context.Context, cfg config.Config) (cloud.Provider, error) {
	switch cfg.Cloud {
	case "aws":
		return awscloud.New(ctx, cfg.Region)
	case "gcp":
		return gcpcloud.New(ctx, gcpcloud.Config{
			ProjectID:     cfg.GCP.ProjectID,
			Region:        cfg.Region,
			Zone:          gcpZone(cfg),
			Network:       cfg.GCP.Network,
			ClientTag:     cfg.GCP.ClientTag,
			RoutePriority: cfg.GCP.RoutePriority,
		})
	default:
		return nil, fmt.Errorf("unsupported cloud %q", cfg.Cloud)
	}
}

func defaultLeaseManager(ctx context.Context, cfg config.Config) (lease.Manager, error) {
	switch cfg.Cloud {
	case "aws":
		if coordinationTable(cfg) != "" {
			return dynamodbcoord.New(ctx, cfg.Region, coordinationTable(cfg), leaseKey(cfg), leaseTTL(cfg))
		}
		return dynamodblease.New(ctx, cfg.Region, cfg.HA.Lease.Table, leaseKey(cfg), leaseTTL(cfg))
	case "gcp":
		return firestorecoord.New(ctx, cfg.GCP.ProjectID, cfg.GCP.FirestoreDatabaseID, cfg.GatewayID, leaseKey(cfg), leaseTTL(cfg))
	default:
		return nil, fmt.Errorf("unsupported cloud %q", cfg.Cloud)
	}
}

func defaultCoordinationStore(ctx context.Context, cfg config.Config) (coordination.Store, error) {
	switch cfg.Cloud {
	case "aws":
		if coordinationTable(cfg) == "" {
			return nil, nil
		}
		return dynamodbcoord.New(ctx, cfg.Region, coordinationTable(cfg), leaseKey(cfg), registryTTL(cfg))
	case "gcp":
		if cfg.Coordination.Backend == "" && !cfg.HA.Enabled {
			return nil, nil
		}
		if cfg.Coordination.Backend != "" && cfg.Coordination.Backend != "firestore" {
			return nil, fmt.Errorf("unsupported coordination.backend %q for cloud=gcp", cfg.Coordination.Backend)
		}
		return firestorecoord.New(ctx, cfg.GCP.ProjectID, cfg.GCP.FirestoreDatabaseID, cfg.GatewayID, leaseKey(cfg), registryTTL(cfg))
	default:
		return nil, fmt.Errorf("unsupported cloud %q", cfg.Cloud)
	}
}

func gcpZone(cfg config.Config) string {
	if cfg.GCP.Zone != "" {
		return cfg.GCP.Zone
	}
	if cfg.Local.AvailabilityZone != "" && cfg.Local.AvailabilityZone != "auto" {
		return cfg.Local.AvailabilityZone
	}
	return ""
}

func leaseKey(cfg config.Config) string {
	if cfg.HA.Lease.Key != "" {
		return cfg.HA.Lease.Key
	}
	return cfg.HAGroupID
}

func leaseTTL(cfg config.Config) time.Duration {
	if cfg.HA.Lease.TTLSeconds > 0 {
		return time.Duration(cfg.HA.Lease.TTLSeconds) * time.Second
	}
	return 10 * time.Second
}
