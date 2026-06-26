package agent

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"
	"time"

	"github.com/nowakeai/betternat/internal/buildinfo"
	"github.com/nowakeai/betternat/internal/config"
	"github.com/nowakeai/betternat/internal/coordination"
	"github.com/nowakeai/betternat/internal/datapath"
	"github.com/nowakeai/betternat/internal/ha"
)

func coordinationTable(cfg config.Config) string {
	return cfg.Coordination.Table
}

func registryRefreshInterval(cfg config.Config) time.Duration {
	if cfg.Coordination.RegistryRefreshIntervalSeconds > 0 {
		return time.Duration(cfg.Coordination.RegistryRefreshIntervalSeconds) * time.Second
	}
	return 5 * time.Second
}

func registryTTL(cfg config.Config) time.Duration {
	if cfg.Coordination.RegistryTTLSeconds > 0 {
		return time.Duration(cfg.Coordination.RegistryTTLSeconds) * time.Second
	}
	return 20 * time.Second
}

func handoverTTL(cfg config.Config) time.Duration {
	if cfg.Coordination.HandoverTTLSeconds > 0 {
		return time.Duration(cfg.Coordination.HandoverTTLSeconds) * time.Second
	}
	return time.Hour
}

const (
	registryStatusTimeout = 2 * time.Second
	registryPutTimeout    = 2 * time.Second
)

func runAgentRegistry(ctx context.Context, registry coordination.AgentRegistry, cfg config.Config, engine datapath.Engine, haStatus interface{ Snapshot() ha.StatusSnapshot }, metricsAddress string) {
	interval := registryRefreshInterval(cfg)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := publishAgentRecord(ctx, registry, cfg, engine, haStatus, metricsAddress); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "betternat-agent: publish registry record: %v\n", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func publishAgentRecord(ctx context.Context, registry coordination.AgentRegistry, cfg config.Config, engine datapath.Engine, haStatus interface{ Snapshot() ha.StatusSnapshot }, metricsAddress string) error {
	status := datapathStatusForRegistry(ctx, engine, registryStatusTimeout)
	snapshot := haStatus.Snapshot()
	build := buildinfo.Current("betternat-agent")
	now := time.Now()
	putCtx, cancel := context.WithTimeout(ctx, registryPutTimeout)
	defer cancel()
	return registry.PutAgent(putCtx, coordination.AgentRecord{
		GatewayID:           cfg.GatewayID,
		HAGroupID:           cfg.HAGroupID,
		NodeID:              cfg.Local.NodeID,
		Cloud:               cfg.Cloud,
		Region:              cfg.Region,
		AvailabilityZone:    cfg.Local.AvailabilityZone,
		PrivateIP:           localInterfaceIP(cfg.Local.PrimaryInterface),
		PublicIP:            publicIPForRegistry(cfg, snapshot),
		MetricsURL:          registryMetricsURL(metricsAddress),
		ControlURL:          peerControlURL(cfg),
		Version:             build.Version,
		Commit:              build.Commit,
		DatapathEngine:      status.Engine,
		DatapathReady:       status.Ready,
		HAState:             string(snapshot.State),
		LeaseGeneration:     snapshot.Lease.Generation,
		RouteTargetMatch:    snapshot.RouteTargetMatches,
		PublicIdentityMatch: snapshot.PublicIdentityMatches,
		UpdatedAt:           now,
		ExpiresAt:           now.Add(registryTTL(cfg)),
	}, registryTTL(cfg))
}

func peerControlURL(cfg config.Config) string {
	if !cfg.Control.PeerAPI.Enabled || cfg.Control.PeerAPI.AuthToken == "" {
		return ""
	}
	host := cfg.Control.PeerAPI.ListenAddress
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = localInterfaceIP(cfg.Local.PrimaryInterface)
	}
	if host == "" {
		return ""
	}
	port := cfg.Control.PeerAPI.ListenPort
	if port <= 0 {
		port = 9109
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(port))
}

func publicIPForRegistry(cfg config.Config, snapshot ha.StatusSnapshot) string {
	if cfg.HA.PublicIdentity.Mode != "shared_eip" {
		return ""
	}
	if snapshot.PublicIdentity.InstanceID != "" && snapshot.PublicIdentity.InstanceID != cfg.Local.NodeID {
		return ""
	}
	return snapshot.PublicIdentity.PublicIP
}

func datapathStatusForRegistry(ctx context.Context, engine datapath.Engine, timeout time.Duration) datapath.Status {
	if timeout <= 0 {
		timeout = registryStatusTimeout
	}
	statusCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	type result struct {
		status datapath.Status
		err    error
	}
	results := make(chan result, 1)
	go func() {
		status, err := engine.Status(statusCtx)
		results <- result{status: status, err: err}
	}()
	select {
	case <-statusCtx.Done():
		return datapath.Status{Engine: engine.Name(), Ready: false, Message: statusCtx.Err().Error()}
	case result := <-results:
		if result.err != nil {
			return datapath.Status{Engine: engine.Name(), Ready: false, Message: result.err.Error()}
		}
		if result.status.Engine == "" {
			result.status.Engine = engine.Name()
		}
		return result.status
	}
}

func registryMetricsURL(address string) string {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return ""
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = localInterfaceIP("")
	}
	if host == "" {
		return ""
	}
	return "http://" + net.JoinHostPort(host, port) + "/metrics"
}

func localInterfaceIP(name string) string {
	if name != "" {
		if iface, err := net.InterfaceByName(name); err == nil {
			if ip := firstInterfaceIPv4(iface); ip != "" {
				return ip
			}
		}
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for i := range interfaces {
		if ip := firstInterfaceIPv4(&interfaces[i]); ip != "" {
			return ip
		}
	}
	return ""
}

func firstInterfaceIPv4(iface *net.Interface) string {
	if iface == nil || iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
		return ""
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		prefix, err := netip.ParsePrefix(addr.String())
		if err != nil || !prefix.Addr().Is4() || prefix.Addr().IsLoopback() {
			continue
		}
		return prefix.Addr().String()
	}
	return ""
}
