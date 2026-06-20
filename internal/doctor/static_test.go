package doctor

import (
	"context"
	"testing"

	"github.com/betternat/betternat/internal/config"
)

func TestStaticCheckersOKWithWarningForMissingRollback(t *testing.T) {
	cfg := validStaticConfig()
	report := Run(context.Background(), StaticCheckers(cfg))
	if report.Status != StatusWarning {
		t.Fatalf("expected warning due to missing rollback metadata: %#v", report)
	}
	if len(report.Checks) != 6 {
		t.Fatalf("unexpected check count: %#v", report.Checks)
	}
}

func TestStaticHAConfigCheckerRequiresDynamoDB(t *testing.T) {
	cfg := validStaticConfig()
	cfg.HA.Lease.Backend = "memory"
	result := StaticHAConfigChecker{Config: cfg}.Check(context.Background())
	if result.Status != StatusCritical {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func validStaticConfig() config.Config {
	return config.Config{
		Version:   "v0",
		GatewayID: "prod-egress",
		HAGroupID: "prod-egress-a",
		Cloud:     "aws",
		Region:    "us-west-2",
		Datapath: config.DatapathConfig{
			Engine:       "loxilb",
			PrivateCIDRs: []string{"10.0.0.0/8"},
			LoxiLB:       config.LoxiLBConfig{SNATTo: "auto", SNATInterface: "ens5"},
		},
		HA: config.HAConfig{
			Enabled: true,
			Lease: config.LeaseConfig{
				Backend: "dynamodb",
				Table:   "betternat-prod-egress-leases",
			},
			RouteFailover: config.RouteFailoverConfig{
				Mode:            "replace_route",
				RouteTableIDs:   []string{"rtb-a"},
				DestinationCIDR: "0.0.0.0/0",
				TargetType:      "instance",
			},
			PublicIdentity: config.PublicIdentityConfig{
				Mode:         "shared_eip",
				AllocationID: "eipalloc-123",
			},
		},
		Observability: config.ObservabilityConfig{
			Prometheus: config.PrometheusConfig{ListenPort: 9108},
			OutboundProbe: config.OutboundProbeConfig{
				Enabled: true,
				URL:     "https://checkip.amazonaws.com",
			},
		},
	}
}
