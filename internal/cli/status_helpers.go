package cli

import (
	"fmt"
	"time"

	"github.com/nowakeai/betternat/internal/agentapi"
	"github.com/nowakeai/betternat/internal/config"
	"github.com/nowakeai/betternat/internal/coordination"
)

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

func statusHasRegistry(cfg config.Config) bool {
	if cfg.Coordination.Table != "" {
		return true
	}
	return cfg.Cloud == "gcp" && cfg.HA.Enabled && cfg.HA.Lease.Backend == "firestore"
}

func agentStatusNodeID(row agentapi.StatusInstance) string {
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
