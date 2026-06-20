package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/betternat/betternat/internal/config"
	"github.com/betternat/betternat/internal/cost"
	"github.com/betternat/betternat/internal/doctor"
)

const usage = `BetterNAT CLI

Usage:
  betternat doctor --config <path>
  betternat cost estimate --gb <processed-gb>
  betternat status --config <path>
  betternat datapath status --config <path>
  betternat failover status --config <path>
  betternat version
`

// Run executes the user-facing BetterNAT CLI.
func Run(ctx context.Context, args []string) error {
	return run(ctx, args, os.Stdout)
}

func run(_ context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		_, _ = fmt.Fprint(out, usage)
		return nil
	}

	switch args[0] {
	case "version":
		_, _ = fmt.Fprintln(out, "betternat dev")
		return nil
	case "doctor":
		return runDoctor(args[1:], out)
	case "cost":
		return runCost(args[1:], out)
	case "status":
		return runStatus(args[1:], out)
	case "datapath":
		return runDatapath(args[1:], out)
	case "failover":
		return runFailover(args[1:], out)
	case "help", "-h", "--help":
		_, _ = fmt.Fprint(out, usage)
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

type failoverStatusOutput struct {
	Enabled              bool     `json:"enabled"`
	LeaseBackend         string   `json:"lease_backend"`
	LeaseTable           string   `json:"lease_table"`
	RouteFailoverMode    string   `json:"route_failover_mode"`
	RouteTableIDs        []string `json:"route_table_ids"`
	DestinationCIDR      string   `json:"destination_cidr"`
	RouteTargetType      string   `json:"route_target_type"`
	PublicIdentityMode   string   `json:"public_identity_mode"`
	PublicIdentityID     string   `json:"public_identity_id"`
	StableEgressIPLikely bool     `json:"stable_egress_ip_likely"`
	OutboundProbeEnabled bool     `json:"outbound_probe_enabled"`
	OutboundProbeURL     string   `json:"outbound_probe_url"`
}

func runFailover(args []string, out io.Writer) error {
	if len(args) == 0 || args[0] != "status" {
		return fmt.Errorf("failover requires subcommand status")
	}
	configPath, err := parseConfigPath(args[1:])
	if err != nil {
		return err
	}
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(failoverStatusOutput{
		Enabled:              cfg.HA.Enabled,
		LeaseBackend:         cfg.HA.Lease.Backend,
		LeaseTable:           cfg.HA.Lease.Table,
		RouteFailoverMode:    cfg.HA.RouteFailover.Mode,
		RouteTableIDs:        cfg.HA.RouteFailover.RouteTableIDs,
		DestinationCIDR:      cfg.HA.RouteFailover.DestinationCIDR,
		RouteTargetType:      cfg.HA.RouteFailover.TargetType,
		PublicIdentityMode:   cfg.HA.PublicIdentity.Mode,
		PublicIdentityID:     cfg.HA.PublicIdentity.AllocationID,
		StableEgressIPLikely: cfg.HA.PublicIdentity.Mode == "shared_eip" && cfg.HA.PublicIdentity.AllocationID != "",
		OutboundProbeEnabled: cfg.Observability.OutboundProbe.Enabled,
		OutboundProbeURL:     cfg.Observability.OutboundProbe.URL,
	})
}

type statusOutput struct {
	GatewayID   string `json:"gateway_id"`
	HAGroupID   string `json:"ha_group_id"`
	Cloud       string `json:"cloud"`
	Region      string `json:"region"`
	HAEnabled   bool   `json:"ha_enabled"`
	Datapath    string `json:"datapath"`
	MetricsAddr string `json:"metrics_addr"`
}

func runStatus(args []string, out io.Writer) error {
	configPath, err := parseConfigPath(args)
	if err != nil {
		return err
	}
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return err
	}
	addr := cfg.Observability.Prometheus.ListenAddress
	if addr == "" {
		addr = "0.0.0.0"
	}
	port := cfg.Observability.Prometheus.ListenPort
	if port == 0 {
		port = 9108
	}
	return json.NewEncoder(out).Encode(statusOutput{
		GatewayID:   cfg.GatewayID,
		HAGroupID:   cfg.HAGroupID,
		Cloud:       cfg.Cloud,
		Region:      cfg.Region,
		HAEnabled:   cfg.HA.Enabled,
		Datapath:    cfg.Datapath.Engine,
		MetricsAddr: fmt.Sprintf("%s:%d", addr, port),
	})
}

type datapathStatusOutput struct {
	Engine         string   `json:"engine"`
	FallbackEngine string   `json:"fallback_engine"`
	PrivateCIDRs   []string `json:"private_cidrs"`
	LoxiLBSNATTo   string   `json:"loxilb_snat_to"`
	SNATInterface  string   `json:"snat_interface"`
}

func runDatapath(args []string, out io.Writer) error {
	if len(args) == 0 || args[0] != "status" {
		return fmt.Errorf("datapath requires subcommand status")
	}
	configPath, err := parseConfigPath(args[1:])
	if err != nil {
		return err
	}
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(datapathStatusOutput{
		Engine:         cfg.Datapath.Engine,
		FallbackEngine: cfg.Datapath.FallbackEngine,
		PrivateCIDRs:   cfg.Datapath.PrivateCIDRs,
		LoxiLBSNATTo:   cfg.Datapath.LoxiLB.SNATTo,
		SNATInterface:  cfg.Datapath.LoxiLB.SNATInterface,
	})
}

func runCost(args []string, out io.Writer) error {
	if len(args) == 0 || args[0] != "estimate" {
		return fmt.Errorf("cost requires subcommand estimate")
	}
	input := cost.DefaultInput()
	flags := flag.NewFlagSet("betternat cost estimate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.Float64Var(&input.ProcessedGB, "gb", input.ProcessedGB, "monthly processed GB")
	flags.Float64Var(&input.Hours, "hours", input.Hours, "monthly gateway hours")
	flags.Float64Var(&input.NATGatewayHourlyUSD, "nat-gateway-hourly", input.NATGatewayHourlyUSD, "NAT Gateway hourly price in USD")
	flags.Float64Var(&input.NATGatewayProcessingUSDGB, "nat-gateway-processing-per-gb", input.NATGatewayProcessingUSDGB, "NAT Gateway processing price per GB in USD")
	flags.Float64Var(&input.ApplianceHourlyUSD, "appliance-hourly", input.ApplianceHourlyUSD, "BetterNAT appliance hourly price in USD")
	flags.IntVar(&input.ApplianceCount, "appliances", input.ApplianceCount, "BetterNAT appliance count")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	estimate, err := cost.EstimateMonthly(input)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(estimate)
}

func runDoctor(args []string, out io.Writer) error {
	configPath, err := parseConfigPath(args)
	if err != nil {
		return err
	}
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		report := doctor.Report{Status: doctor.StatusCritical}
		report.Checks = []doctor.CheckResult{{
			Name:    "config",
			Status:  doctor.StatusCritical,
			Message: err.Error(),
		}}
		return json.NewEncoder(out).Encode(report)
	}
	report := doctor.Run(context.Background(), doctor.StaticCheckers(cfg))
	return json.NewEncoder(out).Encode(report)
}

func parseConfigPath(args []string) (string, error) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config":
			if i+1 >= len(args) {
				return "", fmt.Errorf("--config requires a path")
			}
			return args[i+1], nil
		default:
			return "", fmt.Errorf("unknown doctor argument %q", args[i])
		}
	}
	return "", fmt.Errorf("--config <path> is required")
}
