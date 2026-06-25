package provider

import "testing"

func TestRenderAgentConfig(t *testing.T) {
	cfg, err := RenderAgentConfig(GatewaySpec{
		Name:         "prod-egress",
		Cloud:        "aws",
		Region:       "us-west-2",
		PrivateCIDRs: []string{"10.0.0.0/8"},
		HA: HASpec{
			Enabled:               true,
			LeaseTable:            "betternat-prod-leases",
			SharedEIPAllocationID: "eipalloc-123",
		},
		Coordination: CoordinationSpec{
			Table:              "betternat-prod-coordination",
			HandoverTTLSeconds: 120,
		},
		Control: ControlSpec{
			PeerAPIEnabled:       true,
			PeerAPIListenAddress: "0.0.0.0",
			PeerAPIListenPort:    9109,
			PeerAPIAuthToken:     "secret",
		},
		Observability: ObservabilitySpec{
			OutboundProbeURL:        "https://checkip.amazonaws.com",
			OutboundProbeExpectedIP: "203.0.113.10",
		},
	}, NodeSpec{
		HAGroupID:            "prod-egress-us-west-2a",
		InstanceID:           "auto",
		AvailabilityZone:     "us-west-2a",
		PrimaryInterface:     "ens5",
		RouteTableIDs:        []string{"rtb-a", "rtb-b"},
		RouteDestinationCIDR: "0.0.0.0/0",
	})
	if err != nil {
		t.Fatalf("render config: %v", err)
	}

	if cfg.GatewayID != "prod-egress" || cfg.HAGroupID != "prod-egress-us-west-2a" {
		t.Fatalf("unexpected ids: %#v", cfg)
	}
	if cfg.Datapath.Engine != "loxilb" || cfg.Datapath.FallbackEngine != "nftables" {
		t.Fatalf("unexpected datapath defaults: %#v", cfg.Datapath)
	}
	if cfg.Datapath.LoxiLB.SNATInterface != "ens5" || cfg.Datapath.LoxiLB.APIPort != 11111 {
		t.Fatalf("unexpected loxilb defaults: %#v", cfg.Datapath.LoxiLB)
	}
	if cfg.HA.Lease.Table != "betternat-prod-leases" || cfg.HA.Lease.Key != "prod-egress-us-west-2a" {
		t.Fatalf("unexpected lease config: %#v", cfg.HA.Lease)
	}
	if cfg.Coordination.Table != "betternat-prod-coordination" || cfg.Coordination.Backend != "dynamodb" {
		t.Fatalf("unexpected coordination config: %#v", cfg.Coordination)
	}
	if cfg.Coordination.HandoverTTLSeconds != 120 {
		t.Fatalf("unexpected handover ttl: %#v", cfg.Coordination)
	}
	if !cfg.Control.PeerAPI.Enabled || cfg.Control.PeerAPI.AuthToken != "secret" || cfg.Control.PeerAPI.ListenPort != 9109 {
		t.Fatalf("unexpected peer control config: %#v", cfg.Control.PeerAPI)
	}
	if cfg.HA.PublicIdentity.Mode != "shared_eip" || cfg.HA.PublicIdentity.AllocationID != "eipalloc-123" {
		t.Fatalf("unexpected public identity: %#v", cfg.HA.PublicIdentity)
	}
	if len(cfg.HA.RouteFailover.RouteTableIDs) != 2 {
		t.Fatalf("unexpected route table ids: %#v", cfg.HA.RouteFailover.RouteTableIDs)
	}
	if !cfg.Observability.OutboundProbe.Enabled || cfg.Observability.OutboundProbe.ExpectedIP != "203.0.113.10" {
		t.Fatalf("unexpected outbound probe config: %#v", cfg.Observability.OutboundProbe)
	}
}

func TestRenderAgentConfigForGCPRouteOnlyHA(t *testing.T) {
	cfg, err := RenderAgentConfig(GatewaySpec{
		Name:         "gcp-egress",
		Cloud:        "gcp",
		Region:       "us-west1",
		PrivateCIDRs: []string{"10.91.0.0/24"},
		GCP: GCPSpec{
			ProjectID:           "shared-resources-alt",
			Zone:                "us-west1-a",
			Network:             "bnat-net",
			ClientTag:           "bnat-client",
			RoutePriority:       800,
			FirestoreDatabaseID: "(default)",
		},
		HA: HASpec{
			Enabled:              true,
			LeaseBackend:         "firestore",
			TTLSeconds:           10,
			RenewSeconds:         1,
			RouteMode:            "replace_route",
			RouteDestinationCIDR: "0.0.0.0/0",
			RouteTargetType:      "instance",
		},
		Coordination: CoordinationSpec{
			Backend: "firestore",
		},
	}, NodeSpec{
		HAGroupID:            "gcp-egress-us-west1-a",
		InstanceID:           "auto",
		AvailabilityZone:     "us-west1-a",
		PrimaryInterface:     "ens4",
		RouteTableIDs:        []string{"gcp-egress-default-via-gateway"},
		RouteDestinationCIDR: "0.0.0.0/0",
	})
	if err != nil {
		t.Fatalf("render config: %v", err)
	}

	if cfg.Cloud != "gcp" || cfg.GCP.ProjectID != "shared-resources-alt" || cfg.GCP.Zone != "us-west1-a" {
		t.Fatalf("unexpected GCP config: %#v", cfg.GCP)
	}
	if cfg.HA.Lease.Backend != "firestore" || cfg.Coordination.Backend != "firestore" {
		t.Fatalf("unexpected coordination config: lease=%#v coordination=%#v", cfg.HA.Lease, cfg.Coordination)
	}
	if len(cfg.HA.RouteFailover.RouteTableIDs) != 1 || cfg.HA.RouteFailover.RouteTableIDs[0] != "gcp-egress-default-via-gateway" {
		t.Fatalf("unexpected GCP route targets: %#v", cfg.HA.RouteFailover)
	}
	if cfg.HA.PublicIdentity.Mode != "" {
		t.Fatalf("GCP route-only HA must not configure public identity: %#v", cfg.HA.PublicIdentity)
	}
	if cfg.Datapath.LoxiLB.SNATInterface != "ens4" {
		t.Fatalf("unexpected GCP primary interface: %#v", cfg.Datapath.LoxiLB)
	}
}

func TestRenderAgentConfigWithoutStablePublicIdentity(t *testing.T) {
	cfg, err := RenderAgentConfig(GatewaySpec{
		Name:         "prod-egress",
		Cloud:        "aws",
		Region:       "us-west-2",
		PrivateCIDRs: []string{"10.0.0.0/8"},
		HA: HASpec{
			Enabled:    true,
			LeaseTable: "betternat-prod-leases",
		},
	}, NodeSpec{
		HAGroupID:        "prod-egress-us-west-2a",
		PrimaryInterface: "ens5",
		RouteTableIDs:    []string{"rtb-a"},
	})
	if err != nil {
		t.Fatalf("render config: %v", err)
	}
	if cfg.HA.PublicIdentity.Mode != "" || cfg.HA.PublicIdentity.AllocationID != "" {
		t.Fatalf("non-stable egress should not configure public identity: %#v", cfg.HA.PublicIdentity)
	}
}

func TestRenderAgentConfigRequiresPrivateCIDRs(t *testing.T) {
	_, err := RenderAgentConfig(GatewaySpec{
		Name:   "prod-egress",
		Cloud:  "aws",
		Region: "us-west-2",
	}, NodeSpec{
		HAGroupID:        "prod-egress-us-west-2a",
		PrimaryInterface: "ens5",
	})
	if err == nil {
		t.Fatal("expected private CIDR validation error")
	}
}
