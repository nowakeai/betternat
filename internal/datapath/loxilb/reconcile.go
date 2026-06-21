package loxilb

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"github.com/nowakeai/betternat/internal/config"
)

type desiredRule struct {
	CIDR       string
	SNATTo     string
	Preference int
}

func desiredRules(cfg config.DatapathConfig) ([]desiredRule, error) {
	if len(cfg.PrivateCIDRs) == 0 {
		return nil, fmt.Errorf("loxilb private CIDR list is empty")
	}
	if cfg.LoxiLB.SNATTo == "" || cfg.LoxiLB.SNATTo == "auto" {
		return nil, fmt.Errorf("loxilb snat_to must be resolved before reconcile")
	}
	if ip := net.ParseIP(cfg.LoxiLB.SNATTo); ip == nil {
		return nil, fmt.Errorf("invalid loxilb snat_to IP %q", cfg.LoxiLB.SNATTo)
	}

	base := cfg.LoxiLB.RulePreferenceBase
	if base == 0 {
		base = 100
	}

	rules := make([]desiredRule, 0, len(cfg.PrivateCIDRs))
	for i, cidr := range cfg.PrivateCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return nil, fmt.Errorf("invalid private CIDR %q: %w", cidr, err)
		}
		rules = append(rules, desiredRule{
			CIDR:       cidr,
			SNATTo:     cfg.LoxiLB.SNATTo,
			Preference: base + i,
		})
	}
	return rules, nil
}

func (e *Engine) applyMissingRules(ctx context.Context, cfg config.DatapathConfig, current []firewallRule) error {
	desired, err := desiredRules(cfg)
	if err != nil {
		return err
	}
	for _, rule := range desired {
		if hasRule(current, rule) {
			continue
		}
		firewallRuleArg := "sourceIP:" + rule.CIDR + ",preference:" + strconv.Itoa(rule.Preference)
		if _, err := e.runner.Run(ctx,
			"create", "firewall",
			"--firewallRule="+firewallRuleArg,
			"--snat="+rule.SNATTo,
			"--egress",
		); err != nil {
			return fmt.Errorf("create loxilb egress SNAT rule for %s: %w", rule.CIDR, err)
		}
	}
	return nil
}

func hasRule(current []firewallRule, desired desiredRule) bool {
	for _, rule := range current {
		if rule.Arguments.SourceIP != desired.CIDR {
			continue
		}
		if !rule.Options.DoSNAT {
			continue
		}
		if rule.Options.ToIP != desired.SNATTo {
			continue
		}
		return true
	}
	return false
}
