package nftables

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/betternat/betternat/internal/config"
	"github.com/betternat/betternat/internal/datapath"
)

type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

type Engine struct {
	runner Runner
}

func New() *Engine {
	return NewWithRunner(ExecRunner{})
}

func NewWithRunner(runner Runner) *Engine {
	if runner == nil {
		runner = ExecRunner{}
	}
	return &Engine{runner: runner}
}

func (e *Engine) Name() string {
	return "nftables"
}

func (e *Engine) EnsureReady(ctx context.Context, _ config.DatapathConfig) error {
	if _, err := e.runner.Run(ctx, "nft", "--version"); err != nil {
		return fmt.Errorf("nft --version: %w", err)
	}
	return nil
}

func (e *Engine) Reconcile(ctx context.Context, cfg config.DatapathConfig) error {
	if err := validateConfig(cfg); err != nil {
		return err
	}
	table := tableName(cfg)
	chain := chainName(cfg)
	if err := e.runIgnoreExists(ctx, "add", "table", "inet", table); err != nil {
		return err
	}
	if err := e.runIgnoreExists(ctx, "add", "chain", "inet", table, chain, "{", "type", "nat", "hook", "postrouting", "priority", "srcnat", ";", "policy", "accept", ";", "}"); err != nil {
		return err
	}
	ruleset, err := e.runner.Run(ctx, "nft", "list", "ruleset")
	if err != nil {
		return fmt.Errorf("nft list ruleset: %w", err)
	}
	for _, cidr := range cfg.PrivateCIDRs {
		comment := ruleComment(cidr)
		if strings.Contains(string(ruleset), `comment "`+comment+`"`) {
			continue
		}
		if _, err := e.runner.Run(ctx, "nft", "add", "rule", "inet", table, chain, "ip", "saddr", cidr, "counter", "masquerade", "comment", strconv.Quote(comment)); err != nil {
			return fmt.Errorf("nft add masquerade rule for %s: %w", cidr, err)
		}
	}
	return nil
}

func (e *Engine) Status(ctx context.Context) (datapath.Status, error) {
	table := "betternat"
	if _, err := e.runner.Run(ctx, "nft", "list", "table", "inet", table); err != nil {
		return datapath.Status{Engine: e.Name(), Ready: false, Message: err.Error()}, nil
	}
	return datapath.Status{Engine: e.Name(), Ready: true, Message: "ready"}, nil
}

func (e *Engine) Counters(ctx context.Context) (datapath.Counters, error) {
	output, err := e.runner.Run(ctx, "nft", "list", "ruleset")
	if err != nil {
		return datapath.Counters{}, fmt.Errorf("nft list ruleset: %w", err)
	}
	return parseCounters(string(output)), nil
}

func (e *Engine) ConntrackSummary(ctx context.Context) (datapath.ConntrackSummary, error) {
	output, err := e.runner.Run(ctx, "conntrack", "-L")
	if err != nil {
		return datapath.ConntrackSummary{}, fmt.Errorf("conntrack -L: %w", err)
	}
	return parseConntrack(string(output)), nil
}

func (e *Engine) Cleanup(ctx context.Context) error {
	if _, err := e.runner.Run(ctx, "nft", "delete", "table", "inet", "betternat"); err != nil {
		return fmt.Errorf("nft delete table: %w", err)
	}
	return nil
}

func (e *Engine) runIgnoreExists(ctx context.Context, args ...string) error {
	output, err := e.runner.Run(ctx, "nft", args...)
	if err == nil {
		return nil
	}
	if strings.Contains(string(output), "File exists") || strings.Contains(err.Error(), "File exists") {
		return nil
	}
	return fmt.Errorf("nft %s: %w", strings.Join(args, " "), err)
}

func validateConfig(cfg config.DatapathConfig) error {
	if len(cfg.PrivateCIDRs) == 0 {
		return fmt.Errorf("private cidrs are required")
	}
	for _, cidr := range cfg.PrivateCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return fmt.Errorf("invalid private cidr %q: %w", cidr, err)
		}
	}
	return nil
}

func tableName(cfg config.DatapathConfig) string {
	if cfg.Nftables.TableName == "" {
		return "betternat"
	}
	return cfg.Nftables.TableName
}

func chainName(cfg config.DatapathConfig) string {
	if cfg.Nftables.ChainPrefix == "" {
		return "betternat_postrouting"
	}
	return cfg.Nftables.ChainPrefix + "_postrouting"
}

func ruleComment(cidr string) string {
	return "betternat:" + cidr
}

var counterPattern = regexp.MustCompile(`counter packets ([0-9]+) bytes ([0-9]+).*comment "betternat:([^"]+)"`)

func parseCounters(ruleset string) datapath.Counters {
	matches := counterPattern.FindAllStringSubmatch(ruleset, -1)
	counters := datapath.Counters{Rules: make([]datapath.RuleCounter, 0, len(matches))}
	for _, match := range matches {
		packets, _ := strconv.ParseUint(match[1], 10, 64)
		bytes, _ := strconv.ParseUint(match[2], 10, 64)
		counters.Rules = append(counters.Rules, datapath.RuleCounter{
			CIDR:    match[3],
			Packets: packets,
			Bytes:   bytes,
		})
	}
	return counters
}

func parseConntrack(output string) datapath.ConntrackSummary {
	summary := datapath.ConntrackSummary{Established: map[string]uint64{}}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		proto := fields[0]
		switch proto {
		case "tcp":
			summary.Entries++
			for _, field := range fields {
				if field == "ESTABLISHED" {
					summary.Established["tcp"]++
					break
				}
			}
		case "udp":
			summary.Entries++
			summary.UDPEntries++
			for _, field := range fields {
				if strings.Contains(field, "ASSURED") {
					summary.Established["udp"]++
					break
				}
			}
		default:
			summary.Entries++
		}
	}
	return summary
}
