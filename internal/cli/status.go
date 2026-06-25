package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"

	"github.com/nowakeai/betternat/internal/agentapi"
	"github.com/nowakeai/betternat/internal/config"
	"github.com/nowakeai/betternat/internal/coordination"
	dynamodbcoord "github.com/nowakeai/betternat/internal/coordination/dynamodb"
	"github.com/nowakeai/betternat/internal/doctor"
)

type statusOptions struct {
	configPath string
	host       string
	direct     bool
	output     outputFormat
	sample     time.Duration
	timeout    time.Duration
	watch      bool
	interval   time.Duration
}

type statusOutput struct {
	SchemaVersion    string              `json:"schema_version"`
	GatewayID        string              `json:"gateway_id"`
	HAGroupID        string              `json:"ha_group_id"`
	Cloud            string              `json:"cloud"`
	Region           string              `json:"region"`
	AvailabilityZone string              `json:"availability_zone"`
	HAEnabled        bool                `json:"ha_enabled"`
	Datapath         string              `json:"datapath"`
	MetricsAddr      string              `json:"metrics_addr"`
	PublicIP         string              `json:"public_ip,omitempty"`
	RouteTarget      string              `json:"route_target,omitempty"`
	LeaseGeneration  uint64              `json:"lease_generation,omitempty"`
	LeaseExpiresIn   float64             `json:"lease_expires_in_seconds,omitempty"`
	RouteTargetMatch *bool               `json:"route_target_match,omitempty"`
	PublicIPMatch    *bool               `json:"public_ip_match,omitempty"`
	CacheMode        string              `json:"cache_mode,omitempty"`
	CacheAgeSeconds  float64             `json:"cache_age_seconds,omitempty"`
	CacheFresh       *bool               `json:"cache_fresh,omitempty"`
	InstanceCount    int                 `json:"instance_count"`
	DesiredCount     int32               `json:"desired_count,omitempty"`
	Instances        []statusInstanceRow `json:"instances"`
	Warnings         []string            `json:"warnings,omitempty"`
}

type statusInstanceRow struct {
	NodeID         string  `json:"node_id"`
	InstanceID     string  `json:"instance_id,omitempty"`
	Role           string  `json:"role"`
	Health         string  `json:"health,omitempty"`
	LifecycleState string  `json:"lifecycle_state,omitempty"`
	PrivateIP      string  `json:"private_ip,omitempty"`
	PublicIP       string  `json:"public_ip,omitempty"`
	ControlURL     string  `json:"control_url,omitempty"`
	Version        string  `json:"version,omitempty"`
	RXMbps         float64 `json:"rx_mbps,omitempty"`
	TXMbps         float64 `json:"tx_mbps,omitempty"`
	Metrics        string  `json:"metrics,omitempty"`
	Fresh          bool    `json:"fresh,omitempty"`
	AgeSeconds     float64 `json:"age_seconds,omitempty"`
}

type scrapedMetrics struct {
	Node    string
	Version string
	Commit  string
	Active  *bool
	RXBytes *uint64
	TXBytes *uint64
}

var (
	statusHTTPClient  doctor.HTTPClient = &http.Client{Timeout: 2 * time.Second}
	newStatusRegistry                   = func(ctx context.Context, cfg config.Config) (coordination.AgentReader, error) {
		return dynamodbcoord.New(ctx, cfg.Region, cfg.Coordination.Table, doctorLeaseKey(cfg), doctorLeaseTTL(cfg))
	}
)

func newStatusCommand(ctx context.Context, out io.Writer) *cobra.Command {
	opts := statusOptions{configPath: defaultConfigPath, host: defaultAgentHost(), output: outputTable, sample: time.Second, timeout: 2 * time.Second, interval: 2 * time.Second}
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show gateway, node, failover, version, and traffic status",
		RunE: func(*cobra.Command, []string) error {
			return runStatusWithOptions(ctx, opts, out)
		},
	}
	cmd.Flags().StringVar(&opts.configPath, "config", opts.configPath, "agent config path")
	cmd.Flags().StringVar(&opts.host, "host", opts.host, "agent daemon endpoint")
	cmd.Flags().BoolVar(&opts.direct, "direct", opts.direct, "bypass the local daemon and use config/backend directly")
	cmd.Flags().VarP((*outputFlag)(&opts.output), "output", "o", "output format: table or json")
	cmd.Flags().DurationVar(&opts.sample, "sample", opts.sample, "metrics sampling window for bandwidth")
	cmd.Flags().DurationVar(&opts.timeout, "timeout", opts.timeout, "daemon request timeout")
	cmd.Flags().BoolVarP(&opts.watch, "watch", "w", opts.watch, "refresh status until interrupted")
	cmd.Flags().DurationVar(&opts.interval, "interval", opts.interval, "watch refresh interval")
	return cmd
}

func runStatus(ctx context.Context, args []string, out io.Writer) error {
	opts, err := parseStatusArgs(args)
	if err != nil {
		return err
	}
	return runStatusWithOptions(ctx, opts, out)
}

func parseStatusArgs(args []string) (statusOptions, error) {
	opts := statusOptions{configPath: defaultConfigPath, host: defaultAgentHost(), output: outputTable, sample: time.Second, timeout: 2 * time.Second, interval: 2 * time.Second}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config":
			if i+1 >= len(args) {
				return statusOptions{}, fmt.Errorf("--config requires a path")
			}
			opts.configPath = args[i+1]
			i++
		case "--host":
			if i+1 >= len(args) {
				return statusOptions{}, fmt.Errorf("--host requires a daemon endpoint")
			}
			opts.host = args[i+1]
			i++
		case "--direct":
			opts.direct = true
		case "--output", "-o":
			if i+1 >= len(args) {
				return statusOptions{}, fmt.Errorf("--output requires table or json")
			}
			if err := (*outputFlag)(&opts.output).Set(args[i+1]); err != nil {
				return statusOptions{}, err
			}
			i++
		case "--sample":
			if i+1 >= len(args) {
				return statusOptions{}, fmt.Errorf("--sample requires a duration")
			}
			sample, err := time.ParseDuration(args[i+1])
			if err != nil {
				return statusOptions{}, fmt.Errorf("parse --sample: %w", err)
			}
			opts.sample = sample
			i++
		case "--timeout":
			if i+1 >= len(args) {
				return statusOptions{}, fmt.Errorf("--timeout requires a duration")
			}
			timeout, err := time.ParseDuration(args[i+1])
			if err != nil {
				return statusOptions{}, fmt.Errorf("parse --timeout: %w", err)
			}
			opts.timeout = timeout
			i++
		case "--watch", "-w":
			opts.watch = true
		case "--interval":
			if i+1 >= len(args) {
				return statusOptions{}, fmt.Errorf("--interval requires a duration")
			}
			interval, err := time.ParseDuration(args[i+1])
			if err != nil {
				return statusOptions{}, fmt.Errorf("parse --interval: %w", err)
			}
			opts.interval = interval
			i++
		default:
			return statusOptions{}, fmt.Errorf("unknown argument %q", args[i])
		}
	}
	return opts, nil
}

func runStatusWithOptions(ctx context.Context, opts statusOptions, out io.Writer) error {
	if opts.watch {
		return runStatusWatch(ctx, opts, out)
	}
	return runStatusOnce(ctx, opts, out)
}

func runStatusWatch(ctx context.Context, opts statusOptions, out io.Writer) error {
	if opts.interval <= 0 {
		opts.interval = 2 * time.Second
	}
	for first := true; ; first = false {
		if err := ctx.Err(); err != nil {
			return nil
		}
		if !first && opts.output == outputTable {
			_, _ = fmt.Fprint(out, "\033[H\033[2J")
		}
		if err := runStatusOnce(ctx, opts, out); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if opts.output == outputJSON {
			_, _ = fmt.Fprintln(out)
		}
		timer := time.NewTimer(opts.interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func runStatusOnce(ctx context.Context, opts statusOptions, out io.Writer) error {
	if !opts.direct {
		status, err := requestDaemonStatus(ctx, opts.host, opts.timeout)
		if err != nil {
			return fmt.Errorf("betternat-agent daemon is not reachable at %s: %w\nTry:\n  sudo systemctl status betternat-agent\n  sudo betternat status --direct --config %s", opts.host, err, opts.configPath)
		}
		normalized := statusOutputFromDaemon(status)
		if opts.output == outputJSON {
			return json.NewEncoder(out).Encode(normalized)
		}
		renderStatusTable(out, normalized)
		return nil
	}
	cfg, err := config.LoadFile(opts.configPath)
	if err != nil {
		return err
	}
	status := collectStatus(ctx, cfg, opts.sample)
	if opts.output == outputJSON {
		return json.NewEncoder(out).Encode(status)
	}
	renderStatusTable(out, status)
	return nil
}

func defaultAgentHost() string {
	if host := os.Getenv("BETTERNAT_HOST"); host != "" {
		return host
	}
	return agentapi.DefaultHost
}

func requestDaemonStatus(ctx context.Context, host string, timeout time.Duration) (agentapi.StatusResponse, error) {
	if host == "" {
		host = agentapi.DefaultHost
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client, baseURL, err := daemonHTTPClient(host, timeout)
	if err != nil {
		return agentapi.StatusResponse{}, err
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, baseURL+agentapi.StatusPath, nil)
	if err != nil {
		return agentapi.StatusResponse{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return agentapi.StatusResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return agentapi.StatusResponse{}, fmt.Errorf("daemon returned HTTP %d", resp.StatusCode)
	}
	var status agentapi.StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return agentapi.StatusResponse{}, err
	}
	return status, nil
}

func daemonHTTPClient(host string, timeout time.Duration) (*http.Client, string, error) {
	parsed, err := url.Parse(host)
	if err != nil {
		return nil, "", err
	}
	if parsed.Scheme == "unix" {
		socketPath := parsed.Path
		if socketPath == "" {
			socketPath = parsed.Opaque
		}
		if socketPath == "" {
			return nil, "", fmt.Errorf("unix socket path is required")
		}
		transport := &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socketPath)
			},
		}
		return &http.Client{Transport: transport, Timeout: timeout}, "http://unix", nil
	}
	if parsed.Scheme == "http" || parsed.Scheme == "https" {
		return &http.Client{Timeout: timeout}, strings.TrimRight(host, "/"), nil
	}
	return nil, "", fmt.Errorf("unsupported daemon endpoint scheme %q", parsed.Scheme)
}

func statusOutputFromDaemon(status agentapi.StatusResponse) statusOutput {
	out := statusOutput{
		SchemaVersion:    status.SchemaVersion,
		GatewayID:        status.GatewayID,
		HAGroupID:        status.HAGroupID,
		Cloud:            status.Cloud,
		Region:           status.Region,
		AvailabilityZone: status.AvailabilityZone,
		HAEnabled:        status.HAEnabled,
		Datapath:         status.Datapath,
		MetricsAddr:      status.MetricsAddr,
		PublicIP:         status.PublicIP,
		RouteTarget:      status.RouteTarget,
		LeaseGeneration:  status.LeaseGeneration,
		LeaseExpiresIn:   status.LeaseExpiresIn,
		RouteTargetMatch: status.RouteTargetMatch,
		PublicIPMatch:    status.PublicIPMatch,
		CacheMode:        status.Cache.Mode,
		CacheAgeSeconds:  status.Cache.AgeSeconds,
		InstanceCount:    status.InstanceCount,
		DesiredCount:     status.DesiredCount,
		Warnings:         append([]string(nil), status.Warnings...),
	}
	out.CacheFresh = &status.Cache.Fresh
	if status.Cache.Mode != "" && !status.Cache.Fresh {
		out.Warnings = append(out.Warnings, fmt.Sprintf("daemon cache: stale age %.1fs", status.Cache.AgeSeconds))
	}
	for _, instance := range status.Instances {
		out.Instances = append(out.Instances, statusInstanceRow{
			NodeID:         agentStatusNodeID(instance),
			Role:           instance.Role,
			Health:         instance.Health,
			LifecycleState: instance.LifecycleState,
			PrivateIP:      instance.PrivateIP,
			PublicIP:       instance.PublicIP,
			ControlURL:     instance.ControlURL,
			Version:        instance.Version,
			RXMbps:         instance.RXMbps,
			TXMbps:         instance.TXMbps,
			Metrics:        instance.Metrics,
			Fresh:          instance.Fresh,
			AgeSeconds:     instance.AgeSeconds,
		})
	}
	return out
}

func collectStatus(ctx context.Context, cfg config.Config, sample time.Duration) statusOutput {
	status := statusOutput{
		SchemaVersion:    "v1",
		GatewayID:        cfg.GatewayID,
		HAGroupID:        cfg.HAGroupID,
		Cloud:            cfg.Cloud,
		Region:           cfg.Region,
		AvailabilityZone: cfg.Local.AvailabilityZone,
		HAEnabled:        cfg.HA.Enabled,
		Datapath:         cfg.Datapath.Engine,
		MetricsAddr:      metricsAddress(cfg),
	}

	localInstanceID := cfg.Local.NodeID
	if localInstanceID == "auto" {
		resolved, err := resolveLocalInstanceID(ctx, cfg.Region)
		if err != nil {
			status.Warnings = append(status.Warnings, "local instance: "+err.Error())
			localInstanceID = ""
		} else {
			localInstanceID = resolved
		}
	}

	localMetrics, localSecond, err := scrapeMetricsSample(ctx, prometheusURL(cfg), sample)
	if err != nil {
		status.Warnings = append(status.Warnings, "local metrics: "+err.Error())
	}

	if cfg.Coordination.Table != "" {
		if collectRegistryStatus(ctx, cfg, sample, &status) {
			return status
		}
	}

	if cfg.Cloud == "aws" {
		collectAWSStatus(ctx, cfg, localInstanceID, localMetrics, localSecond, sample, &status)
	} else if localInstanceID != "" {
		status.Instances = append(status.Instances, statusInstanceRow{
			NodeID:  localInstanceID,
			Role:    roleFromMetrics(localMetrics),
			Version: localMetrics.Version,
			RXMbps:  rateMbps(localMetrics.RXBytes, localSecond.RXBytes, sample),
			TXMbps:  rateMbps(localMetrics.TXBytes, localSecond.TXBytes, sample),
			Metrics: metricsState(localMetrics),
		})
	}

	if len(status.Instances) == 0 && localInstanceID != "" {
		status.Instances = append(status.Instances, statusInstanceRow{
			NodeID:  localInstanceID,
			Role:    roleFromMetrics(localMetrics),
			Version: localMetrics.Version,
			RXMbps:  rateMbps(localMetrics.RXBytes, localSecond.RXBytes, sample),
			TXMbps:  rateMbps(localMetrics.TXBytes, localSecond.TXBytes, sample),
			Metrics: metricsState(localMetrics),
		})
	}
	status.InstanceCount = len(status.Instances)
	return status
}

func collectRegistryStatus(ctx context.Context, cfg config.Config, sample time.Duration, status *statusOutput) bool {
	registry, err := newStatusRegistry(ctx, cfg)
	if err != nil {
		status.Warnings = append(status.Warnings, "registry setup: "+err.Error())
		return false
	}
	lease, err := registry.Current(ctx)
	if err != nil {
		status.Warnings = append(status.Warnings, "registry lease: "+err.Error())
	}
	agents, err := registry.ListAgents(ctx)
	if err != nil {
		status.Warnings = append(status.Warnings, "registry agents: "+err.Error())
		return false
	}
	if len(agents) == 0 {
		status.Warnings = append(status.Warnings, "registry agents: no fresh records")
		return false
	}
	status.RouteTarget = lease.OwnerInstanceID
	status.LeaseGeneration = lease.Generation
	if !lease.ExpiresAt.IsZero() {
		status.LeaseExpiresIn = time.Until(lease.ExpiresAt).Seconds()
	}
	for _, agent := range agents {
		nodeID := statusAgentRecordNodeID(agent)
		row := statusInstanceRow{
			NodeID:         nodeID,
			Role:           routeRole(nodeID, lease.OwnerInstanceID),
			Health:         healthFromAgent(agent),
			LifecycleState: agent.HAState,
			PrivateIP:      agent.PrivateIP,
			PublicIP:       agent.PublicIP,
			ControlURL:     agent.ControlURL,
			Version:        agent.Version,
			Metrics:        "registry",
			Fresh:          true,
		}
		if !agent.UpdatedAt.IsZero() {
			row.AgeSeconds = time.Since(agent.UpdatedAt).Seconds()
		}
		if row.Role == "active" && status.PublicIP == "" {
			status.PublicIP = agent.PublicIP
		}
		if agent.MetricsURL != "" {
			first, second, err := scrapeMetricsSample(ctx, agent.MetricsURL, sample)
			if err != nil {
				row.Metrics = "unreachable"
			} else {
				if first.Version != "" {
					row.Version = first.Version
				}
				row.RXMbps = rateMbps(first.RXBytes, second.RXBytes, sample)
				row.TXMbps = rateMbps(first.TXBytes, second.TXBytes, sample)
				row.Metrics = metricsState(first)
			}
		}
		status.Instances = append(status.Instances, row)
	}
	status.InstanceCount = len(status.Instances)
	return true
}

func collectAWSStatus(ctx context.Context, cfg config.Config, localInstanceID string, localMetrics scrapedMetrics, localSecond scrapedMetrics, sample time.Duration, status *statusOutput) {
	cloudProvider, err := newLiveCloudProvider(ctx, cfg.Region)
	if err != nil {
		status.Warnings = append(status.Warnings, "cloud: "+err.Error())
		return
	}
	status.RouteTarget = describeRouteTarget(ctx, cfg, cloudProvider, status)
	status.PublicIP = describePublicIP(ctx, cfg, cloudProvider, status)

	if cfg.Local.AvailabilityZone == "" || cfg.Local.AvailabilityZone == "auto" {
		if localInstanceID != "" {
			status.Instances = append(status.Instances, localStatusRow(localInstanceID, status.RouteTarget, localMetrics, localSecond, sample))
		}
		return
	}

	asgInspector, err := newLiveASGInspector(ctx, cfg.Region)
	if err != nil {
		status.Warnings = append(status.Warnings, "asg: "+err.Error())
		return
	}
	asg, err := asgInspector.DescribeASG(ctx, doctorASGName(cfg))
	if err != nil {
		status.Warnings = append(status.Warnings, "asg: "+err.Error())
		if localInstanceID != "" {
			status.Instances = append(status.Instances, localStatusRow(localInstanceID, status.RouteTarget, localMetrics, localSecond, sample))
		}
		return
	}
	status.DesiredCount = asg.DesiredCapacity
	for _, instance := range asg.Instances {
		row := statusInstanceRow{
			NodeID:         instance.InstanceID,
			Role:           routeRole(instance.InstanceID, status.RouteTarget),
			Health:         instance.HealthStatus,
			LifecycleState: instance.LifecycleState,
		}
		info, err := cloudProvider.DescribeInstance(ctx, instance.InstanceID)
		if err != nil {
			status.Warnings = append(status.Warnings, fmt.Sprintf("instance %s: %v", instance.InstanceID, err))
		} else {
			row.PrivateIP = info.PrivateIP
			row.PublicIP = info.PublicIP
		}
		if instance.InstanceID == localInstanceID {
			row.Version = localMetrics.Version
			row.RXMbps = rateMbps(localMetrics.RXBytes, localSecond.RXBytes, sample)
			row.TXMbps = rateMbps(localMetrics.TXBytes, localSecond.TXBytes, sample)
			row.Metrics = metricsState(localMetrics)
			if row.Role == "standby" {
				row.Role = roleFromMetrics(localMetrics)
			}
		} else if row.PrivateIP != "" {
			remoteMetrics, remoteSecond, err := scrapeMetricsSample(ctx, fmt.Sprintf("http://%s:%d/metrics", row.PrivateIP, metricsPort(cfg)), sample)
			if err != nil {
				row.Metrics = "unreachable"
			} else {
				row.Version = remoteMetrics.Version
				row.RXMbps = rateMbps(remoteMetrics.RXBytes, remoteSecond.RXBytes, sample)
				row.TXMbps = rateMbps(remoteMetrics.TXBytes, remoteSecond.TXBytes, sample)
				row.Metrics = metricsState(remoteMetrics)
			}
		}
		status.Instances = append(status.Instances, row)
	}
}

func describeRouteTarget(ctx context.Context, cfg config.Config, cloudProvider liveCloudProvider, status *statusOutput) string {
	if !cfg.HA.Enabled || len(cfg.HA.RouteFailover.RouteTableIDs) == 0 {
		return ""
	}
	target, err := cloudProvider.DescribeRoute(ctx, cfg.HA.RouteFailover.RouteTableIDs[0], cfg.HA.RouteFailover.DestinationCIDR)
	if err != nil {
		status.Warnings = append(status.Warnings, "route: "+err.Error())
		return ""
	}
	return target.Target
}

func describePublicIP(ctx context.Context, cfg config.Config, cloudProvider liveCloudProvider, status *statusOutput) string {
	allocationID := cfg.HA.PublicIdentity.AllocationID
	var err error
	if cfg.HA.PublicIdentity.Mode == "shared_eip" && allocationID == "auto" && cfg.Local.AvailabilityZone != "" && cfg.Local.AvailabilityZone != "auto" {
		allocationID, err = resolveSharedEIPAllocation(ctx, cfg.Region, cfg.GatewayID, cfg.Local.AvailabilityZone)
		if err != nil {
			status.Warnings = append(status.Warnings, "public identity: "+err.Error())
			return ""
		}
	}
	if cfg.HA.PublicIdentity.Mode != "shared_eip" || allocationID == "" || allocationID == "auto" {
		return ""
	}
	identity, err := cloudProvider.DescribePublicIdentity(ctx, allocationID)
	if err != nil {
		status.Warnings = append(status.Warnings, "public identity: "+err.Error())
		return ""
	}
	return identity.PublicIP
}

func localStatusRow(instanceID string, routeTarget string, first scrapedMetrics, second scrapedMetrics, sample time.Duration) statusInstanceRow {
	return statusInstanceRow{
		NodeID:  instanceID,
		Role:    routeRole(instanceID, routeTarget),
		Version: first.Version,
		RXMbps:  rateMbps(first.RXBytes, second.RXBytes, sample),
		TXMbps:  rateMbps(first.TXBytes, second.TXBytes, sample),
		Metrics: metricsState(first),
	}
}

func agentStatusNodeID(row agentapi.StatusInstance) string {
	if row.NodeID != "" {
		return row.NodeID
	}
	return row.InstanceID
}

func statusRowNodeID(row statusInstanceRow) string {
	if row.NodeID != "" {
		return row.NodeID
	}
	return row.InstanceID
}

func statusAgentRecordNodeID(record coordination.AgentRecord) string {
	if record.NodeID != "" {
		return record.NodeID
	}
	return record.InstanceID
}

func scrapeMetricsSample(ctx context.Context, url string, sample time.Duration) (scrapedMetrics, scrapedMetrics, error) {
	first, err := scrapeMetrics(ctx, url)
	if err != nil {
		return scrapedMetrics{}, scrapedMetrics{}, err
	}
	if sample <= 0 {
		return first, first, nil
	}
	timer := time.NewTimer(sample)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return first, scrapedMetrics{}, ctx.Err()
	case <-timer.C:
	}
	second, err := scrapeMetrics(ctx, url)
	if err != nil {
		return first, scrapedMetrics{}, err
	}
	return first, second, nil
}

func scrapeMetrics(ctx context.Context, url string) (scrapedMetrics, error) {
	if url == "" {
		return scrapedMetrics{}, fmt.Errorf("metrics url is not configured")
	}
	client := statusHTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return scrapedMetrics{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return scrapedMetrics{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return scrapedMetrics{}, fmt.Errorf("metrics returned HTTP %d", resp.StatusCode)
	}
	return parseMetrics(resp.Body)
}

func parseMetrics(r io.Reader) (scrapedMetrics, error) {
	var metrics scrapedMetrics
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, labels, value := splitMetricLine(line)
		switch name {
		case "betternat_agent_build_info":
			metrics.Node = labels["node"]
			metrics.Version = labels["version"]
			metrics.Commit = labels["commit"]
		case "betternat_active":
			active := value == "1"
			metrics.Active = &active
			if metrics.Node == "" {
				metrics.Node = labels["node"]
			}
		case "betternat_interface_rx_bytes_total":
			if parsed, ok := parseUintMetric(value); ok {
				metrics.RXBytes = &parsed
			}
		case "betternat_interface_tx_bytes_total":
			if parsed, ok := parseUintMetric(value); ok {
				metrics.TXBytes = &parsed
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return scrapedMetrics{}, fmt.Errorf("scan metrics: %w", err)
	}
	return metrics, nil
}

func splitMetricLine(line string) (string, map[string]string, string) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return line, nil, ""
	}
	head := fields[0]
	value := fields[1]
	labels := map[string]string{}
	open := strings.IndexByte(head, '{')
	if open == -1 {
		return head, labels, value
	}
	name := head[:open]
	close := strings.LastIndexByte(head, '}')
	if close <= open {
		return name, labels, value
	}
	for _, part := range splitLabelParts(head[open+1 : close]) {
		key, val, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		unquoted, err := strconv.Unquote(val)
		if err != nil {
			unquoted = strings.Trim(val, `"`)
		}
		labels[key] = unquoted
	}
	return name, labels, value
}

func splitLabelParts(s string) []string {
	var parts []string
	start := 0
	inQuotes := false
	escaped := false
	for i, r := range s {
		switch {
		case escaped:
			escaped = false
		case r == '\\':
			escaped = true
		case r == '"':
			inQuotes = !inQuotes
		case r == ',' && !inQuotes:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	if start <= len(s) {
		parts = append(parts, s[start:])
	}
	return parts
}

func parseUintMetric(value string) (uint64, bool) {
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err == nil {
		return parsed, true
	}
	floatValue, err := strconv.ParseFloat(value, 64)
	if err != nil || floatValue < 0 {
		return 0, false
	}
	return uint64(floatValue), true
}

func renderStatusTable(out io.Writer, status statusOutput) {
	summary := table.NewWriter()
	summary.SetOutputMirror(out)
	summary.SetStyle(statusTableStyle())
	summary.AppendHeader(table.Row{"Gateway", "HA Group", "Region", "AZ", "Public IP", "Datapath", "Nodes", "Desired", "Lease", "TTL", "Cache"})
	summary.AppendRow(table.Row{
		valueOrUnknown(status.GatewayID),
		valueOrUnknown(status.HAGroupID),
		valueOrUnknown(status.Region),
		valueOrUnknown(status.AvailabilityZone),
		valueOrUnknown(status.PublicIP),
		valueOrUnknown(status.Datapath),
		status.InstanceCount,
		desiredCountValue(status.DesiredCount),
		leaseGenerationValue(status.LeaseGeneration),
		leaseTTLValue(status.LeaseExpiresIn),
		cacheValue(status),
	})
	summary.Render()
	_, _ = fmt.Fprintln(out)

	instances := table.NewWriter()
	instances.SetOutputMirror(out)
	instances.SetStyle(statusTableStyle())
	instances.AppendHeader(table.Row{"Node", "Role", "Health", "State", "Age", "Version", "Private IP", "Public IP", "RX Mbps", "TX Mbps", "Metrics", "Control"})
	for _, row := range status.Instances {
		instances.AppendRow(table.Row{
			valueOrUnknown(statusRowNodeID(row)),
			valueOrUnknown(row.Role),
			valueOrUnknown(row.Health),
			valueOrUnknown(row.LifecycleState),
			ageValue(row.AgeSeconds, row.Fresh),
			valueOrUnknown(row.Version),
			valueOrUnknown(row.PrivateIP),
			valueOrUnknown(row.PublicIP),
			formatMbps(row.RXMbps),
			formatMbps(row.TXMbps),
			valueOrUnknown(row.Metrics),
			controlValue(row.ControlURL),
		})
	}
	instances.Render()
	if len(status.Warnings) > 0 {
		_, _ = fmt.Fprintln(out, "\nWarnings:")
		for _, warning := range status.Warnings {
			_, _ = fmt.Fprintf(out, "- %s\n", warning)
		}
	}
}

func statusTableStyle() table.Style {
	style := table.StyleDefault
	style.Name = "BetterNATStatus"
	style.Options = table.OptionsNoBordersAndSeparators
	return style
}

func metricsAddress(cfg config.Config) string {
	addr := cfg.Observability.Prometheus.ListenAddress
	if addr == "" {
		addr = "0.0.0.0"
	}
	return fmt.Sprintf("%s:%d", addr, metricsPort(cfg))
}

func metricsPort(cfg config.Config) int {
	if cfg.Observability.Prometheus.ListenPort == 0 {
		return 9108
	}
	return cfg.Observability.Prometheus.ListenPort
}

func routeRole(instanceID string, routeTarget string) string {
	if routeTarget == "" {
		return "unknown"
	}
	if instanceID == routeTarget {
		return "active"
	}
	return "standby"
}

func roleFromMetrics(metrics scrapedMetrics) string {
	if metrics.Active == nil {
		return "unknown"
	}
	if *metrics.Active {
		return "active"
	}
	return "standby"
}

func metricsState(metrics scrapedMetrics) string {
	if metrics.Version == "" && metrics.Active == nil && metrics.RXBytes == nil && metrics.TXBytes == nil {
		return "unavailable"
	}
	return "ok"
}

func desiredCountValue(value int32) string {
	if value == 0 {
		return "unknown"
	}
	return fmt.Sprintf("%d", value)
}

func leaseGenerationValue(value uint64) string {
	if value == 0 {
		return "unknown"
	}
	return fmt.Sprintf("%d", value)
}

func leaseTTLValue(value float64) string {
	if value == 0 {
		return "unknown"
	}
	return fmt.Sprintf("%.0fs", value)
}

func cacheValue(status statusOutput) string {
	if status.CacheMode == "" {
		return "unknown"
	}
	fresh := ""
	if status.CacheFresh != nil && !*status.CacheFresh {
		fresh = "/stale"
	}
	if status.CacheAgeSeconds > 0 {
		return fmt.Sprintf("%s%s %.1fs", status.CacheMode, fresh, status.CacheAgeSeconds)
	}
	return status.CacheMode + fresh
}

func ageValue(seconds float64, fresh bool) string {
	if seconds == 0 && !fresh {
		return "unknown"
	}
	suffix := ""
	if !fresh {
		suffix = " stale"
	}
	return fmt.Sprintf("%.1fs%s", seconds, suffix)
}

func controlValue(url string) string {
	if url == "" {
		return "unknown"
	}
	return "ok"
}

func healthFromAgent(agent coordination.AgentRecord) string {
	if agent.DatapathReady {
		return "Healthy"
	}
	return "Degraded"
}

func rateMbps(first *uint64, second *uint64, sample time.Duration) float64 {
	if first == nil || second == nil || sample <= 0 || *second < *first {
		return 0
	}
	return float64(*second-*first) * 8 / sample.Seconds() / 1_000_000
}

func formatMbps(value float64) string {
	if value == 0 {
		return "0.00"
	}
	if math.Abs(value) < 0.01 {
		return "<0.01"
	}
	return fmt.Sprintf("%.2f", value)
}

type outputFlag outputFormat

func (f *outputFlag) String() string {
	if f == nil || *f == "" {
		return string(outputTable)
	}
	return string(*f)
}

func (f *outputFlag) Set(value string) error {
	switch outputFormat(value) {
	case outputTable, outputJSON:
		*f = outputFlag(value)
		return nil
	default:
		return fmt.Errorf("unsupported output format %q", value)
	}
}

func (*outputFlag) Type() string {
	return "format"
}
