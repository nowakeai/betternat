package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/nowakeai/betternat/internal/buildinfo"
	"github.com/nowakeai/betternat/internal/cloud"
	awscloud "github.com/nowakeai/betternat/internal/cloud/aws"
	"github.com/nowakeai/betternat/internal/config"
	"github.com/nowakeai/betternat/internal/cost"
	"github.com/nowakeai/betternat/internal/datapath"
	"github.com/nowakeai/betternat/internal/datapath/loxilb"
	"github.com/nowakeai/betternat/internal/datapath/nftables"
	"github.com/nowakeai/betternat/internal/doctor"
	awsiamcheck "github.com/nowakeai/betternat/internal/iamcheck/aws"
	"github.com/nowakeai/betternat/internal/lease"
	dynamodblease "github.com/nowakeai/betternat/internal/lease/dynamodb"
	"github.com/nowakeai/betternat/internal/probe"
)

const usage = `BetterNAT CLI

Usage:
  betternat doctor [--live] --config <path>
  betternat cost estimate --gb <processed-gb>
  betternat status --config <path>
  betternat datapath status --config <path>
  betternat datapath ready --config <path>
  betternat failover status --config <path>
  betternat version
`

// Run executes the user-facing BetterNAT CLI.
func Run(ctx context.Context, args []string) error {
	return run(ctx, args, os.Stdout)
}

func run(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		_, _ = fmt.Fprint(out, usage)
		return nil
	}

	switch args[0] {
	case "version":
		_, _ = fmt.Fprintln(out, buildinfo.Current("betternat").String())
		return nil
	case "doctor":
		return runDoctor(args[1:], out)
	case "cost":
		return runCost(args[1:], out)
	case "status":
		return runStatus(args[1:], out)
	case "datapath":
		return runDatapath(ctx, args[1:], out)
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

type datapathReadyOutput struct {
	Engine               string   `json:"engine"`
	Ready                bool     `json:"ready"`
	Message              string   `json:"message"`
	ExpectedPrivateCIDRs []string `json:"expected_private_cidrs"`
	PresentSNATCIDRs     []string `json:"present_snat_cidrs"`
	MissingSNATCIDRs     []string `json:"missing_snat_cidrs"`
}

func runDatapath(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("datapath requires subcommand status or ready")
	}
	configPath, err := parseConfigPath(args[1:])
	if err != nil {
		return err
	}
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return err
	}
	switch args[0] {
	case "status":
		return json.NewEncoder(out).Encode(datapathStatusOutput{
			Engine:         cfg.Datapath.Engine,
			FallbackEngine: cfg.Datapath.FallbackEngine,
			PrivateCIDRs:   cfg.Datapath.PrivateCIDRs,
			LoxiLBSNATTo:   cfg.Datapath.LoxiLB.SNATTo,
			SNATInterface:  cfg.Datapath.LoxiLB.SNATInterface,
		})
	case "ready":
		engine, err := newDatapathEngine(cfg.Datapath)
		if err != nil {
			return err
		}
		ready, err := datapathReadiness(ctx, cfg.Datapath, engine)
		if encodeErr := json.NewEncoder(out).Encode(ready); encodeErr != nil {
			return encodeErr
		}
		if err != nil {
			return err
		}
		if !ready.Ready {
			return fmt.Errorf("datapath is not ready: %s", ready.Message)
		}
		return nil
	default:
		return fmt.Errorf("unknown datapath subcommand %q", args[0])
	}
}

var newDatapathEngine = func(cfg config.DatapathConfig) (datapath.Engine, error) {
	switch cfg.Engine {
	case "loxilb":
		return loxilb.New(), nil
	case "nftables":
		return nftables.New(), nil
	default:
		return nil, fmt.Errorf("unsupported datapath engine %q", cfg.Engine)
	}
}

func datapathReadiness(ctx context.Context, cfg config.DatapathConfig, engine datapath.Engine) (datapathReadyOutput, error) {
	output := datapathReadyOutput{
		Engine:               engine.Name(),
		ExpectedPrivateCIDRs: append([]string(nil), cfg.PrivateCIDRs...),
	}
	status, err := engine.Status(ctx)
	if err != nil {
		output.Message = err.Error()
		return output, err
	}
	output.Ready = status.Ready
	output.Message = status.Message
	if !status.Ready {
		return output, nil
	}
	counters, err := engine.Counters(ctx)
	if err != nil {
		output.Ready = false
		output.Message = err.Error()
		return output, err
	}
	present := map[string]bool{}
	for _, rule := range counters.Rules {
		if rule.CIDR == "" {
			continue
		}
		present[rule.CIDR] = true
		output.PresentSNATCIDRs = append(output.PresentSNATCIDRs, rule.CIDR)
	}
	for _, cidr := range cfg.PrivateCIDRs {
		if !present[cidr] {
			output.MissingSNATCIDRs = append(output.MissingSNATCIDRs, cidr)
		}
	}
	if len(output.MissingSNATCIDRs) > 0 {
		output.Ready = false
		output.Message = "missing expected SNAT rules"
	}
	return output, nil
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
	opts, err := parseDoctorArgs(args)
	if err != nil {
		return err
	}
	cfg, err := config.LoadFile(opts.configPath)
	if err != nil {
		report := doctor.Report{Status: doctor.StatusCritical}
		report.Checks = []doctor.CheckResult{{
			Name:    "config",
			Status:  doctor.StatusCritical,
			Message: err.Error(),
		}}
		if encodeErr := json.NewEncoder(out).Encode(report); encodeErr != nil {
			return encodeErr
		}
		return fmt.Errorf("doctor status critical")
	}
	checkers := doctor.StaticCheckers(cfg)
	if opts.live {
		liveCheckers, err := liveDoctorCheckers(context.Background(), cfg)
		if err != nil {
			checkers = append(checkers, doctor.StaticErrorChecker{Name: "live_setup", Message: err.Error()})
		} else {
			checkers = append(checkers, liveCheckers...)
		}
	}
	report := doctor.Run(context.Background(), checkers)
	if err := json.NewEncoder(out).Encode(report); err != nil {
		return err
	}
	if report.Status == doctor.StatusCritical {
		return fmt.Errorf("doctor status critical")
	}
	return nil
}

type doctorOptions struct {
	configPath string
	live       bool
}

func parseDoctorArgs(args []string) (doctorOptions, error) {
	opts := doctorOptions{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config":
			if i+1 >= len(args) {
				return doctorOptions{}, fmt.Errorf("--config requires a path")
			}
			opts.configPath = args[i+1]
			i++
		case "--live":
			opts.live = true
		default:
			return doctorOptions{}, fmt.Errorf("unknown doctor argument %q", args[i])
		}
	}
	if opts.configPath == "" {
		return doctorOptions{}, fmt.Errorf("--config <path> is required")
	}
	return opts, nil
}

type liveCloudProvider interface {
	cloud.Provider
	doctor.InstanceInspector
}

var (
	newLiveCloudProvider = func(ctx context.Context, region string) (liveCloudProvider, error) {
		return awscloud.New(ctx, region)
	}
	newLiveASGInspector = func(ctx context.Context, region string) (doctor.ASGInspector, error) {
		return awscloud.NewASGProvider(ctx, region)
	}
	newLiveIAMEvaluator = func(ctx context.Context, region string, policySourceARN string) (doctor.IAMChecker, error) {
		evaluator, err := awsiamcheck.New(ctx, region, policySourceARN)
		if err != nil {
			return doctor.IAMChecker{}, err
		}
		return doctor.IAMChecker{Evaluator: evaluator}, nil
	}
	newLiveLeaseManager = func(ctx context.Context, region string, table string, haGroupID string, ttl time.Duration) (lease.Manager, error) {
		return dynamodblease.New(ctx, region, table, haGroupID, ttl)
	}
	resolveLocalInstanceID      = awscloud.ResolveLocalInstanceID
	resolveCurrentRoleARN       = awsiamcheck.ResolveCurrentRoleARN
	resolveSharedEIPAllocation  = awscloud.ResolveSharedEIPAllocationID
	liveDoctorPrometheusClient  doctor.HTTPClient
	liveDoctorSourceProbeClient probe.HTTPClient
)

func liveDoctorCheckers(ctx context.Context, cfg config.Config) ([]doctor.Checker, error) {
	engine, err := newDatapathEngine(cfg.Datapath)
	if err != nil {
		return nil, err
	}
	checkers := []doctor.Checker{doctor.DatapathChecker{Engine: engine}}

	localInstanceID := cfg.Local.InstanceID
	if localInstanceID == "auto" {
		localInstanceID, err = resolveLocalInstanceID(ctx, cfg.Region)
		if err != nil {
			checkers = append(checkers, doctor.StaticErrorChecker{Name: "local_instance", Message: err.Error()})
		}
	}

	if cfg.Cloud != "aws" {
		checkers = append(checkers, doctor.StaticWarningChecker{Name: "cloud", Message: "live doctor currently supports cloud=aws only"})
		return checkers, nil
	}

	cloudProvider, err := newLiveCloudProvider(ctx, cfg.Region)
	if err != nil {
		checkers = append(checkers, doctor.StaticErrorChecker{Name: "cloud", Message: err.Error()})
		return checkers, nil
	}

	policySourceARN, err := resolveCurrentRoleARN(ctx, cfg.Region)
	if err != nil {
		checkers = append(checkers, doctor.StaticWarningChecker{Name: "iam", Message: err.Error()})
	} else {
		iamChecker, err := newLiveIAMEvaluator(ctx, cfg.Region, policySourceARN)
		if err != nil {
			checkers = append(checkers, doctor.StaticErrorChecker{Name: "iam", Message: err.Error()})
		} else {
			checkers = append(checkers, iamChecker)
		}
	}

	if cfg.Local.AvailabilityZone == "" || cfg.Local.AvailabilityZone == "auto" {
		checkers = append(checkers, doctor.StaticWarningChecker{Name: "asg", Message: "local availability zone is not resolved"})
	} else {
		asgInspector, err := newLiveASGInspector(ctx, cfg.Region)
		if err != nil {
			checkers = append(checkers, doctor.StaticErrorChecker{Name: "asg", Message: err.Error()})
		} else {
			checkers = append(checkers, doctor.ASGChecker{
				Inspector: asgInspector,
				Name:      doctorASGName(cfg),
				HAEnabled: cfg.HA.Enabled,
			})
		}
	}

	expectedOwner := ""
	if cfg.HA.Enabled && cfg.HA.Lease.Backend == "dynamodb" && cfg.HA.Lease.Table != "" {
		leaseManager, err := newLiveLeaseManager(ctx, cfg.Region, cfg.HA.Lease.Table, doctorLeaseKey(cfg), doctorLeaseTTL(cfg))
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

	for _, routeTableID := range cfg.HA.RouteFailover.RouteTableIDs {
		checkers = append(checkers, doctor.RouteChecker{
			Cloud:           cloudProvider,
			RouteTableID:    routeTableID,
			DestinationCIDR: cfg.HA.RouteFailover.DestinationCIDR,
			ExpectedTarget:  expectedOwner,
		})
	}

	allocationID := cfg.HA.PublicIdentity.AllocationID
	if cfg.HA.PublicIdentity.Mode == "shared_eip" && allocationID == "auto" {
		if cfg.Local.AvailabilityZone == "" || cfg.Local.AvailabilityZone == "auto" {
			checkers = append(checkers, doctor.StaticWarningChecker{Name: "public_identity", Message: "shared EIP allocation id is auto and availability zone is not resolved"})
		} else {
			allocationID, err = resolveSharedEIPAllocation(ctx, cfg.Region, cfg.GatewayID, cfg.Local.AvailabilityZone)
			if err != nil {
				checkers = append(checkers, doctor.StaticErrorChecker{Name: "public_identity", Message: err.Error()})
			}
		}
	}
	if cfg.HA.PublicIdentity.Mode == "shared_eip" && allocationID != "" && allocationID != "auto" {
		checkers = append(checkers, doctor.PublicIdentityChecker{
			Cloud:              cloudProvider,
			AllocationID:       allocationID,
			ExpectedInstanceID: expectedOwner,
		})
	}

	if localInstanceID != "" && localInstanceID != "auto" {
		checkers = append(checkers, doctor.SourceDestCheckChecker{Inspector: cloudProvider, InstanceID: localInstanceID})
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

func prometheusURL(cfg config.Config) string {
	port := cfg.Observability.Prometheus.ListenPort
	if port == 0 {
		return ""
	}
	address := cfg.Observability.Prometheus.ListenAddress
	if address == "" || address == "0.0.0.0" || address == "::" {
		address = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s:%d/metrics", address, port)
}

func doctorASGName(cfg config.Config) string {
	return fmt.Sprintf("betternat-%s-%s", cfg.GatewayID, cfg.Local.AvailabilityZone)
}

func doctorLeaseKey(cfg config.Config) string {
	if cfg.HA.Lease.Key != "" {
		return cfg.HA.Lease.Key
	}
	return cfg.HAGroupID
}

func doctorLeaseTTL(cfg config.Config) time.Duration {
	if cfg.HA.Lease.TTLSeconds > 0 {
		return time.Duration(cfg.HA.Lease.TTLSeconds) * time.Second
	}
	return 15 * time.Second
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
			return "", fmt.Errorf("unknown argument %q", args[i])
		}
	}
	return "", fmt.Errorf("--config <path> is required")
}
