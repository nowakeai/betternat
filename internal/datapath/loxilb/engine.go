package loxilb

import (
	"context"
	"fmt"
	"time"

	"github.com/nowakeai/betternat/internal/config"
	"github.com/nowakeai/betternat/internal/datapath"
	"github.com/nowakeai/betternat/internal/localnet"
)

var (
	reconcileAttempts = 8
	reconcileBackoff  = time.Second
)

type Engine struct {
	runner     Runner
	ipResolver localnet.IPResolver
}

func New() *Engine {
	return NewWithDeps(NewExecRunner(), localnet.NetIPResolver{})
}

func NewWithRunner(runner Runner) *Engine {
	return NewWithDeps(runner, localnet.NetIPResolver{})
}

func NewWithDeps(runner Runner, ipResolver localnet.IPResolver) *Engine {
	return &Engine{runner: runner, ipResolver: ipResolver}
}

func (e *Engine) Name() string {
	return "loxilb"
}

func (e *Engine) EnsureReady(ctx context.Context, _ config.DatapathConfig) error {
	if _, err := e.runner.Run(ctx, "get", "lbversion", "-o", "json"); err != nil {
		return fmt.Errorf("loxilb is not ready: %w", err)
	}
	return nil
}

func (e *Engine) Reconcile(ctx context.Context, cfg config.DatapathConfig) error {
	resolved, err := e.resolveSNATTo(cfg)
	if err != nil {
		return err
	}
	cfg = resolved
	if _, err := desiredRules(cfg); err != nil {
		return err
	}
	var lastErr error
	attempts := reconcileAttempts
	if attempts <= 0 {
		attempts = 1
	}
	for attempt := 0; attempt < attempts; attempt++ {
		if err := e.reconcileOnce(ctx, cfg); err != nil {
			lastErr = err
			if attempt+1 == attempts {
				break
			}
			if sleepErr := sleepContext(ctx, reconcileBackoff); sleepErr != nil {
				return lastErr
			}
			continue
		}
		return nil
	}
	return lastErr
}

func (e *Engine) reconcileOnce(ctx context.Context, cfg config.DatapathConfig) error {
	if err := e.EnsureReady(ctx, cfg); err != nil {
		return err
	}
	out, err := e.runner.Run(ctx, "get", "firewall", "-o", "json")
	if err != nil {
		return fmt.Errorf("get loxilb firewall rules: %w", err)
	}
	current, err := parseFirewall(out)
	if err != nil {
		return err
	}
	return e.applyMissingRules(ctx, cfg, current)
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (e *Engine) resolveSNATTo(cfg config.DatapathConfig) (config.DatapathConfig, error) {
	if cfg.LoxiLB.SNATTo != "auto" {
		return cfg, nil
	}
	if e.ipResolver == nil {
		return cfg, fmt.Errorf("loxilb snat_to is auto but no IP resolver is configured")
	}
	ip, err := e.ipResolver.IPv4ByInterface(cfg.LoxiLB.SNATInterface)
	if err != nil {
		return cfg, fmt.Errorf("resolve loxilb snat_to from interface %q: %w", cfg.LoxiLB.SNATInterface, err)
	}
	cfg.LoxiLB.SNATTo = ip
	return cfg, nil
}

func (e *Engine) Status(ctx context.Context) (datapath.Status, error) {
	if err := e.EnsureReady(ctx, config.DatapathConfig{}); err != nil {
		return datapath.Status{Engine: e.Name(), Ready: false, Message: err.Error()}, nil
	}
	return datapath.Status{Engine: e.Name(), Ready: true, Message: "ready"}, nil
}

func (e *Engine) Counters(ctx context.Context) (datapath.Counters, error) {
	out, err := e.runner.Run(ctx, "get", "firewall", "-o", "json")
	if err != nil {
		return datapath.Counters{}, fmt.Errorf("get loxilb firewall counters: %w", err)
	}
	return parseFirewallCounters(out)
}

func (e *Engine) ConntrackSummary(ctx context.Context) (datapath.ConntrackSummary, error) {
	out, err := e.runner.Run(ctx, "get", "conntrack", "-o", "json")
	if err != nil {
		return datapath.ConntrackSummary{}, fmt.Errorf("get loxilb conntrack: %w", err)
	}
	return parseConntrackSummary(out)
}

func (e *Engine) Cleanup(context.Context) error {
	return nil
}
