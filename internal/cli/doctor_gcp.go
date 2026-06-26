package cli

import (
	"context"
	"fmt"

	"github.com/nowakeai/betternat/internal/config"
	"github.com/nowakeai/betternat/internal/doctor"
	"github.com/nowakeai/betternat/internal/probe"
)

func appendGCPLiveDoctorCheckers(ctx context.Context, cfg config.Config, checkers []doctor.Checker) ([]doctor.Checker, error) {
	cloudProvider, err := newLiveGCPCloudProvider(ctx, cfg)
	if err != nil {
		checkers = append(checkers, doctor.StaticErrorChecker{Name: "cloud", Message: err.Error()})
		return checkers, nil
	}

	expectedOwner := ""
	if cfg.HA.Enabled && cfg.HA.Lease.Backend == "firestore" {
		leaseManager, err := newLiveFirestoreLeaseManager(ctx, cfg)
		if err != nil {
			checkers = append(checkers, doctor.StaticErrorChecker{Name: "lease_setup", Message: err.Error()})
		} else {
			record, err := leaseManager.Current(ctx)
			if err != nil {
				checkers = append(checkers, doctor.LeaseChecker{Lease: leaseManager})
			} else {
				expectedOwner = record.OwnerInstanceID
				checkers = append(checkers, doctor.StaticOKChecker{Name: "lease", Message: fmt.Sprintf("lease owner %s generation %d", record.OwnerInstanceID, record.Generation)})
			}
		}
	}

	for _, routeName := range cfg.HA.RouteFailover.RouteTableIDs {
		checkers = append(checkers, doctor.RouteChecker{
			Cloud:           cloudProvider,
			RouteTableID:    routeName,
			DestinationCIDR: cfg.HA.RouteFailover.DestinationCIDR,
			ExpectedTarget:  expectedOwner,
		})
	}

	if cfg.HA.PublicIdentity.Mode == "" {
		checkers = append(checkers, doctor.StaticOKChecker{Name: "public_identity", Message: "GCP route-only HA has no shared public identity configured"})
	} else if cfg.HA.PublicIdentity.Mode == "shared_eip" && cfg.HA.PublicIdentity.AllocationID != "" {
		checkers = append(checkers, doctor.PublicIdentityChecker{
			Cloud:              cloudProvider,
			AllocationID:       cfg.HA.PublicIdentity.AllocationID,
			ExpectedInstanceID: expectedOwner,
		})
	} else {
		checkers = append(checkers, doctor.StaticWarningChecker{Name: "public_identity", Message: "GCP shared public identity is not fully configured"})
	}

	checkers = append(checkers, doctor.PrometheusChecker{
		URL:    prometheusURL(cfg),
		Client: liveDoctorPrometheusClient,
	})

	if cfg.Observability.OutboundProbe.Enabled {
		checkers = append(checkers, doctor.SourceIPProbeChecker{Probe: probe.SourceIPProbe{
			URL:        cfg.Observability.OutboundProbe.URL,
			ExpectedIP: cfg.Observability.OutboundProbe.ExpectedIP,
			Client:     liveDoctorSourceProbeClient,
		}})
	}

	return checkers, nil
}
