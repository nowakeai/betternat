package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"

	"github.com/nowakeai/betternat/internal/buildinfo"
	"github.com/nowakeai/betternat/internal/cloud"
	awscloud "github.com/nowakeai/betternat/internal/cloud/aws"
	gcpcloud "github.com/nowakeai/betternat/internal/cloud/gcp"
	"github.com/nowakeai/betternat/internal/config"
	dynamodbcoord "github.com/nowakeai/betternat/internal/coordination/dynamodb"
	firestorecoord "github.com/nowakeai/betternat/internal/coordination/firestore"
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

var defaultConfigPath = "/etc/betternat/agent.json"

// Run executes the user-facing BetterNAT CLI.
func Run(ctx context.Context, args []string) error {
	return run(ctx, args, os.Stdout)
}

func run(ctx context.Context, args []string, out io.Writer) error {
	cmd := newRootCommand(ctx, out)
	cmd.SetArgs(args)
	return cmd.Execute()
}

func newRootCommand(ctx context.Context, out io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "betternat",
		Short:         "Operate a BetterNAT gateway",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.SetOut(out)
	cmd.SetErr(out)

	cmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the BetterNAT CLI version",
		RunE: func(*cobra.Command, []string) error {
			_, _ = fmt.Fprintln(out, buildinfo.Current("betternat").String())
			return nil
		},
	})
	cmd.AddCommand(newDoctorCommand(out))
	cmd.AddCommand(newStatusCommand(ctx, out))
	cmd.AddCommand(newCostCommand(out))
	cmd.AddCommand(newDatapathCommand(ctx, out))
	cmd.AddCommand(newFailoverCommand(out))
	cmd.AddCommand(newHandoverCommand(ctx, out))
	cmd.AddCommand(newSupportCommand(ctx, out))
	return cmd
}

func newDoctorCommand(out io.Writer) *cobra.Command {
	opts := doctorOptions{configPath: defaultConfigPath}
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run static or live gateway diagnostics",
		RunE: func(*cobra.Command, []string) error {
			args := []string{"--config", opts.configPath}
			if opts.live {
				args = append(args, "--live")
			}
			return runDoctor(args, out)
		},
	}
	cmd.Flags().StringVar(&opts.configPath, "config", opts.configPath, "agent config path")
	cmd.Flags().BoolVar(&opts.live, "live", opts.live, "include live AWS, datapath, lease, and metrics checks")
	return cmd
}

func newCostCommand(out io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cost",
		Short: "Estimate BetterNAT cost savings",
	}
	input := cost.DefaultInput()
	estimate := &cobra.Command{
		Use:   "estimate",
		Short: "Estimate monthly NAT Gateway replacement savings",
		RunE: func(_ *cobra.Command, args []string) error {
			estimate, err := cost.EstimateMonthly(input)
			if err != nil {
				return err
			}
			return json.NewEncoder(out).Encode(estimate)
		},
	}
	estimate.Flags().Float64Var(&input.ProcessedGB, "gb", input.ProcessedGB, "monthly processed GB")
	estimate.Flags().Float64Var(&input.Hours, "hours", input.Hours, "monthly gateway hours")
	estimate.Flags().Float64Var(&input.NATGatewayHourlyUSD, "nat-gateway-hourly", input.NATGatewayHourlyUSD, "NAT Gateway hourly price in USD")
	estimate.Flags().Float64Var(&input.NATGatewayProcessingUSDGB, "nat-gateway-processing-per-gb", input.NATGatewayProcessingUSDGB, "NAT Gateway processing price per GB in USD")
	estimate.Flags().Float64Var(&input.NodeHourlyUSD, "node-hourly", input.NodeHourlyUSD, "BetterNAT node hourly price in USD")
	estimate.Flags().IntVar(&input.NodeCount, "nodes", input.NodeCount, "BetterNAT node count")
	estimate.Flags().Float64Var(&input.NodeHourlyUSD, "appliance-hourly", input.NodeHourlyUSD, "deprecated alias for --node-hourly")
	estimate.Flags().IntVar(&input.NodeCount, "appliances", input.NodeCount, "deprecated alias for --nodes")
	_ = estimate.Flags().MarkHidden("appliance-hourly")
	_ = estimate.Flags().MarkHidden("appliances")
	cmd.AddCommand(estimate)
	return cmd
}

func newDatapathCommand(ctx context.Context, out io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "datapath",
		Short: "Inspect datapath configuration and readiness",
	}
	cmd.AddCommand(newDatapathStatusCommand(ctx, out))
	cmd.AddCommand(newDatapathReadyCommand(ctx, out))
	return cmd
}

func newDatapathStatusCommand(ctx context.Context, out io.Writer) *cobra.Command {
	opts := configOutputOptions{configPath: defaultConfigPath, output: outputTable}
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show configured datapath state",
		RunE: func(*cobra.Command, []string) error {
			return runDatapath(ctx, []string{"status", "--config", opts.configPath, "--output", string(opts.output)}, out)
		},
	}
	cmd.Flags().StringVar(&opts.configPath, "config", opts.configPath, "agent config path")
	cmd.Flags().VarP((*outputFlag)(&opts.output), "output", "o", "output format: table or json")
	return cmd
}

func newDatapathReadyCommand(ctx context.Context, out io.Writer) *cobra.Command {
	opts := configOutputOptions{configPath: defaultConfigPath, output: outputJSON}
	cmd := &cobra.Command{
		Use:   "ready",
		Short: "Check live datapath readiness",
		RunE: func(*cobra.Command, []string) error {
			return runDatapath(ctx, []string{"ready", "--config", opts.configPath, "--output", string(opts.output)}, out)
		},
	}
	cmd.Flags().StringVar(&opts.configPath, "config", opts.configPath, "agent config path")
	cmd.Flags().VarP((*outputFlag)(&opts.output), "output", "o", "output format: table or json")
	return cmd
}

func newFailoverCommand(out io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "failover",
		Short: "Inspect failover configuration",
	}
	opts := configOutputOptions{configPath: defaultConfigPath, output: outputTable}
	status := &cobra.Command{
		Use:   "status",
		Short: "Show failover configuration",
		RunE: func(*cobra.Command, []string) error {
			return runFailover([]string{"status", "--config", opts.configPath, "--output", string(opts.output)}, out)
		},
	}
	status.Flags().StringVar(&opts.configPath, "config", opts.configPath, "agent config path")
	status.Flags().VarP((*outputFlag)(&opts.output), "output", "o", "output format: table or json")
	cmd.AddCommand(status)
	return cmd
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
	opts, err := parseConfigOutputArgs(args[1:])
	if err != nil {
		return err
	}
	cfg, err := config.LoadFile(opts.configPath)
	if err != nil {
		return err
	}
	status := failoverStatusOutput{
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
	}
	if opts.output == outputJSON {
		return json.NewEncoder(out).Encode(status)
	}
	renderFailoverStatusTable(out, status)
	return nil
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
	opts, err := parseConfigOutputArgs(args[1:])
	if err != nil {
		return err
	}
	cfg, err := config.LoadFile(opts.configPath)
	if err != nil {
		return err
	}
	switch args[0] {
	case "status":
		status := datapathStatusOutput{
			Engine:         cfg.Datapath.Engine,
			FallbackEngine: cfg.Datapath.FallbackEngine,
			PrivateCIDRs:   cfg.Datapath.PrivateCIDRs,
			LoxiLBSNATTo:   cfg.Datapath.LoxiLB.SNATTo,
			SNATInterface:  cfg.Datapath.LoxiLB.SNATInterface,
		}
		if opts.output == outputJSON {
			return json.NewEncoder(out).Encode(status)
		}
		renderDatapathStatusTable(out, status)
		return nil
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
	flags.Float64Var(&input.NodeHourlyUSD, "node-hourly", input.NodeHourlyUSD, "BetterNAT node hourly price in USD")
	flags.IntVar(&input.NodeCount, "nodes", input.NodeCount, "BetterNAT node count")
	flags.Float64Var(&input.NodeHourlyUSD, "appliance-hourly", input.NodeHourlyUSD, "deprecated alias for --node-hourly")
	flags.IntVar(&input.NodeCount, "appliances", input.NodeCount, "deprecated alias for --nodes")
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
	opts := doctorOptions{configPath: defaultConfigPath}
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
	return opts, nil
}

type liveCloudProvider interface {
	cloud.Provider
	doctor.InstanceInspector
	doctor.SourceDestCheckInspector
}

var (
	newLiveCloudProvider = func(ctx context.Context, region string) (liveCloudProvider, error) {
		return awscloud.New(ctx, region)
	}
	newLiveGCPCloudProvider = func(ctx context.Context, cfg config.Config) (cloud.Provider, error) {
		return gcpcloud.New(ctx, gcpcloud.Config{
			ProjectID:     cfg.GCP.ProjectID,
			Region:        cfg.Region,
			Zone:          doctorGCPZone(cfg),
			Network:       cfg.GCP.Network,
			ClientTag:     cfg.GCP.ClientTag,
			RoutePriority: cfg.GCP.RoutePriority,
		})
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
	newLiveFirestoreLeaseManager = func(ctx context.Context, cfg config.Config) (lease.Manager, error) {
		return firestorecoord.New(ctx, cfg.GCP.ProjectID, cfg.GCP.FirestoreDatabaseID, cfg.GatewayID, doctorLeaseKey(cfg), doctorLeaseTTL(cfg))
	}
	resolveLocalInstanceID      = awscloud.ResolveLocalInstanceID
	resolveGCPLocalInstanceID   = gcpcloud.ResolveLocalInstanceID
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

	localInstanceID := cfg.Local.NodeID
	if localInstanceID == "auto" {
		resolver := resolveLocalInstanceID
		if cfg.Cloud == "gcp" {
			resolver = resolveGCPLocalInstanceID
		}
		localInstanceID, err = resolver(ctx, cfg.Region)
		if err != nil {
			checkers = append(checkers, doctor.StaticErrorChecker{Name: "local_instance", Message: err.Error()})
		}
	}

	switch cfg.Cloud {
	case "aws":
		return appendAWSLiveDoctorCheckers(ctx, cfg, checkers, localInstanceID)
	case "gcp":
		return appendGCPLiveDoctorCheckers(ctx, cfg, checkers)
	default:
		checkers = append(checkers, doctor.StaticWarningChecker{Name: "cloud", Message: fmt.Sprintf("live doctor does not support cloud=%s", cfg.Cloud)})
		return checkers, nil
	}
}

func appendAWSLiveDoctorCheckers(ctx context.Context, cfg config.Config, checkers []doctor.Checker, localInstanceID string) ([]doctor.Checker, error) {
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

	if cfg.Coordination.Table != "" {
		checkers = append(checkers, doctor.StaticWarningChecker{Name: "asg", Message: "ASG discovery skipped because coordination registry is configured; use betternat status for fleet health"})
	} else if cfg.Local.AvailabilityZone == "" || cfg.Local.AvailabilityZone == "auto" {
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
	if cfg.HA.Enabled && cfg.HA.Lease.Backend == "dynamodb" && doctorLeaseTable(cfg) != "" {
		leaseManager, err := liveDoctorLeaseManager(ctx, cfg)
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

func doctorLeaseTable(cfg config.Config) string {
	if cfg.Coordination.Table != "" {
		return cfg.Coordination.Table
	}
	return cfg.HA.Lease.Table
}

func liveDoctorLeaseManager(ctx context.Context, cfg config.Config) (lease.Manager, error) {
	if cfg.Coordination.Table != "" {
		return dynamodbcoord.New(ctx, cfg.Region, cfg.Coordination.Table, doctorLeaseKey(cfg), doctorLeaseTTL(cfg))
	}
	return newLiveLeaseManager(ctx, cfg.Region, cfg.HA.Lease.Table, doctorLeaseKey(cfg), doctorLeaseTTL(cfg))
}

func doctorGCPZone(cfg config.Config) string {
	if cfg.GCP.Zone != "" {
		return cfg.GCP.Zone
	}
	if cfg.Local.AvailabilityZone != "" && cfg.Local.AvailabilityZone != "auto" {
		return cfg.Local.AvailabilityZone
	}
	return ""
}

func doctorLeaseTTL(cfg config.Config) time.Duration {
	if cfg.HA.Lease.TTLSeconds > 0 {
		return time.Duration(cfg.HA.Lease.TTLSeconds) * time.Second
	}
	return 15 * time.Second
}

type outputFormat string

const (
	outputTable outputFormat = "table"
	outputJSON  outputFormat = "json"
)

type configOutputOptions struct {
	configPath string
	output     outputFormat
}

func parseConfigOutputArgs(args []string) (configOutputOptions, error) {
	opts := configOutputOptions{configPath: defaultConfigPath, output: outputTable}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config":
			if i+1 >= len(args) {
				return configOutputOptions{}, fmt.Errorf("--config requires a path")
			}
			opts.configPath = args[i+1]
			i++
		case "--output", "-o":
			if i+1 >= len(args) {
				return configOutputOptions{}, fmt.Errorf("--output requires table or json")
			}
			switch outputFormat(args[i+1]) {
			case outputTable, outputJSON:
				opts.output = outputFormat(args[i+1])
			default:
				return configOutputOptions{}, fmt.Errorf("unsupported output format %q", args[i+1])
			}
			i++
		default:
			return configOutputOptions{}, fmt.Errorf("unknown argument %q", args[i])
		}
	}
	return opts, nil
}

func renderDatapathStatusTable(out io.Writer, status datapathStatusOutput) {
	t := table.NewWriter()
	t.SetOutputMirror(out)
	t.SetStyle(table.StyleLight)
	t.AppendHeader(table.Row{"Field", "Value"})
	t.AppendRows([]table.Row{
		{"Engine", valueOrUnknown(status.Engine)},
		{"Private CIDRs", stringsJoin(status.PrivateCIDRs)},
		{"SNAT interface", valueOrUnknown(status.SNATInterface)},
		{"LoxiLB SNAT to", valueOrUnknown(status.LoxiLBSNATTo)},
	})
	t.Render()
}

func renderFailoverStatusTable(out io.Writer, status failoverStatusOutput) {
	t := table.NewWriter()
	t.SetOutputMirror(out)
	t.SetStyle(table.StyleLight)
	t.AppendHeader(table.Row{"Field", "Value"})
	t.AppendRows([]table.Row{
		{"Enabled", status.Enabled},
		{"Lease backend", valueOrUnknown(status.LeaseBackend)},
		{"Lease table", valueOrUnknown(status.LeaseTable)},
		{"Route mode", valueOrUnknown(status.RouteFailoverMode)},
		{"Route tables", stringsJoin(status.RouteTableIDs)},
		{"Destination", valueOrUnknown(status.DestinationCIDR)},
		{"Public identity", valueOrUnknown(status.PublicIdentityMode)},
		{"Stable egress IP", status.StableEgressIPLikely},
		{"Outbound probe", status.OutboundProbeEnabled},
	})
	t.Render()
}

func valueOrUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

func stringsJoin(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ", ")
}
