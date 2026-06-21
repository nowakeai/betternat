package ha

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/betternat/betternat/internal/config"
	"github.com/betternat/betternat/internal/lease"
)

type State string

const (
	StateInit       State = "INIT"
	StateStandby    State = "STANDBY"
	StateTakingOver State = "TAKING_OVER"
	StateActive     State = "ACTIVE"
	StateDegraded   State = "DEGRADED"
	StateError      State = "ERROR"
)

type Supervisor struct {
	Controller Controller
	Now        lease.Clock
	Reporter   StatusReporter
}

type StepResult struct {
	State      State
	Lease      lease.Record
	Activation ActivationResult
	Err        error
}

func (s Supervisor) Run(ctx context.Context, cfg config.Config, localInstanceID string, interval time.Duration) error {
	if interval <= 0 {
		interval = renewInterval(cfg)
	}
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		result := s.Step(ctx, cfg, localInstanceID)
		s.report(result)
		if result.Err != nil && result.State == StateError {
			return result.Err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (s Supervisor) Step(ctx context.Context, cfg config.Config, localInstanceID string) StepResult {
	stepCtx, cancel := context.WithTimeout(ctx, haStepTimeout(cfg))
	defer cancel()
	return s.step(stepCtx, cfg, localInstanceID)
}

func (s Supervisor) step(ctx context.Context, cfg config.Config, localInstanceID string) StepResult {
	if !cfg.HA.Enabled {
		if err := s.reconcileDatapath(ctx, cfg); err != nil {
			return StepResult{State: StateError, Err: err}
		}
		return StepResult{State: StateStandby}
	}
	if localInstanceID == "" || localInstanceID == "auto" {
		return StepResult{State: StateError, Err: fmt.Errorf("local instance id is required for HA supervisor")}
	}
	if s.Controller.Lease == nil {
		return StepResult{State: StateError, Err: fmt.Errorf("lease manager is required for HA supervisor")}
	}

	current, err := s.Controller.Lease.Current(ctx)
	now := s.now()
	if err == nil && current.OwnerInstanceID == localInstanceID && now.Before(current.ExpiresAt) {
		renewed, renewErr := s.Controller.Lease.Renew(ctx, current)
		if renewErr != nil {
			return StepResult{State: StateDegraded, Lease: current, Err: fmt.Errorf("renew HA lease: %w", renewErr)}
		}
		if reconcileErr := s.reconcileDatapath(ctx, cfg); reconcileErr != nil {
			return StepResult{State: StateDegraded, Lease: renewed, Err: reconcileErr}
		}
		controller := s.Controller
		if controller.Now == nil {
			controller.Now = s.now
		}
		if leaseErr := controller.VerifyLease(ctx, renewed, localInstanceID); leaseErr != nil {
			return StepResult{State: StateDegraded, Lease: renewed, Err: fmt.Errorf("verify HA lease before ownership repair: %w", leaseErr)}
		}
		ownership, ownershipErr := controller.EnsureOwnership(ctx, cfg, localInstanceID)
		if ownershipErr != nil {
			return StepResult{State: StateDegraded, Lease: renewed, Err: ownershipErr}
		}
		if leaseErr := controller.VerifyLease(ctx, renewed, localInstanceID); leaseErr != nil {
			return StepResult{State: StateDegraded, Lease: renewed, Activation: ownership, Err: fmt.Errorf("verify HA lease after ownership repair: %w", leaseErr)}
		}
		ownership.Lease = renewed
		return StepResult{State: StateActive, Lease: renewed, Activation: ownership}
	}
	if err == nil && current.OwnerInstanceID != "" && current.OwnerInstanceID != localInstanceID && now.Before(current.ExpiresAt) {
		if reconcileErr := s.reconcileDatapath(ctx, cfg); reconcileErr != nil {
			return StepResult{State: StateStandby, Lease: current, Err: reconcileErr}
		}
		return StepResult{State: StateStandby, Lease: current}
	}

	takingOver := StepResult{State: StateTakingOver, Lease: current}
	s.report(takingOver)
	controller := s.Controller
	if controller.Now == nil {
		controller.Now = s.now
	}
	activation, activateErr := controller.Activate(ctx, cfg, localInstanceID)
	if activateErr != nil {
		return StepResult{State: StateStandby, Lease: current, Err: activateErr}
	}
	return StepResult{State: StateActive, Lease: activation.Lease, Activation: activation}
}

func (s Supervisor) report(result StepResult) {
	if s.Reporter != nil {
		s.Reporter.Report(result)
	}
	log.Printf(
		"betternat_ha_step state=%s lease_owner=%s lease_generation=%d lease_expires_at=%s err=%q",
		result.State,
		result.Lease.OwnerInstanceID,
		result.Lease.Generation,
		result.Lease.ExpiresAt.Format(time.RFC3339),
		errorString(result.Err),
	)
}

func (s Supervisor) reconcileDatapath(ctx context.Context, cfg config.Config) error {
	if s.Controller.Datapath == nil {
		return nil
	}
	reconcileCtx, cancel := context.WithTimeout(ctx, datapathReconcileTimeout(cfg))
	defer cancel()
	if err := s.Controller.Datapath.Reconcile(reconcileCtx, cfg.Datapath); err != nil {
		return fmt.Errorf("reconcile standby datapath: %w", err)
	}
	return nil
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (s Supervisor) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func renewInterval(cfg config.Config) time.Duration {
	if cfg.HA.Lease.RenewIntervalSeconds > 0 {
		return time.Duration(cfg.HA.Lease.RenewIntervalSeconds) * time.Second
	}
	if cfg.HA.Lease.TTLSeconds > 0 {
		return time.Duration(cfg.HA.Lease.TTLSeconds) * time.Second / 3
	}
	return 5 * time.Second
}

func datapathReconcileTimeout(cfg config.Config) time.Duration {
	interval := renewInterval(cfg)
	if interval <= time.Second {
		return time.Second
	}
	timeout := interval - time.Second
	if timeout > 5*time.Second {
		return 5 * time.Second
	}
	return timeout
}

func haStepTimeout(cfg config.Config) time.Duration {
	if cfg.HA.Lease.TTLSeconds > 0 {
		timeout := time.Duration(cfg.HA.Lease.TTLSeconds) * time.Second / 2
		if timeout < time.Second {
			return time.Second
		}
		if timeout > 8*time.Second {
			return 8 * time.Second
		}
		return timeout
	}
	return 5 * time.Second
}
