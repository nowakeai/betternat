package metrics

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/betternat/betternat/internal/datapath"
)

// Exporter serves BetterNAT metrics.
type Exporter interface {
	Run(ctx context.Context) error
}

// Snapshot is a point-in-time view of the local BetterNAT datapath.
type Snapshot struct {
	GatewayID         string
	HAGroupID         string
	Node              string
	Version           string
	Commit            string
	AgentUp           bool
	Active            bool
	HAState           string
	LeaseGeneration   uint64
	Datapath          datapath.Status
	Counters          datapath.Counters
	Conntrack         datapath.ConntrackSummary
	Owners            []OwnerCounter
	Processed         TrafficCounter
	FailoverEvents    []FailoverEventCounter
	FailoverDurations []FailoverDuration
}

type OwnerCounter struct {
	Owner     string
	Direction string
	Packets   uint64
	Bytes     uint64
}

type TrafficCounter struct {
	Direction string
	Packets   uint64
	Bytes     uint64
}

type FailoverEventCounter struct {
	Reason string
	Result string
	Count  uint64
}

type FailoverDuration struct {
	Phase   string
	Seconds float64
}

// RenderPrometheus writes a Prometheus text-format snapshot.
func RenderPrometheus(w io.Writer, snapshot Snapshot) error {
	if w == nil {
		return nil
	}

	baseLabels := map[string]string{
		"gateway":  snapshot.GatewayID,
		"ha_group": snapshot.HAGroupID,
		"engine":   snapshot.Datapath.Engine,
	}
	agentLabels := map[string]string{
		"gateway":  snapshot.GatewayID,
		"ha_group": snapshot.HAGroupID,
		"node":     snapshot.Node,
	}
	agentUp := uint64(0)
	if snapshot.AgentUp {
		agentUp = 1
	}
	if err := writeMetric(w, "betternat_agent_up", agentLabels, agentUp); err != nil {
		return err
	}
	buildLabels := cloneLabels(agentLabels)
	buildLabels["version"] = snapshot.Version
	buildLabels["commit"] = snapshot.Commit
	if err := writeMetric(w, "betternat_agent_build_info", buildLabels, 1); err != nil {
		return err
	}
	active := uint64(0)
	if snapshot.Active {
		active = 1
	}
	if err := writeMetric(w, "betternat_active", agentLabels, active); err != nil {
		return err
	}
	if snapshot.HAState != "" {
		stateLabels := cloneLabels(agentLabels)
		stateLabels["state"] = snapshot.HAState
		if err := writeMetric(w, "betternat_ha_state", stateLabels, 1); err != nil {
			return err
		}
	}
	if err := writeMetric(w, "betternat_lease_generation", agentLabels, snapshot.LeaseGeneration); err != nil {
		return err
	}

	if err := writeMetric(w, "betternat_datapath_engine_info", baseLabels, 1); err != nil {
		return err
	}
	ready := 0
	if snapshot.Datapath.Ready {
		ready = 1
	}
	if err := writeMetric(w, "betternat_datapath_ready", baseLabels, uint64(ready)); err != nil {
		return err
	}
	if err := writeMetric(w, "betternat_conntrack_entries", baseLabels, snapshot.Conntrack.Entries); err != nil {
		return err
	}
	if err := writeMetric(w, "betternat_conntrack_udp_entries", baseLabels, snapshot.Conntrack.UDPEntries); err != nil {
		return err
	}

	protocols := make([]string, 0, len(snapshot.Conntrack.Established))
	for proto := range snapshot.Conntrack.Established {
		protocols = append(protocols, proto)
	}
	sort.Strings(protocols)
	for _, proto := range protocols {
		labels := cloneLabels(baseLabels)
		labels["protocol"] = proto
		if err := writeMetric(w, "betternat_conntrack_established", labels, snapshot.Conntrack.Established[proto]); err != nil {
			return err
		}
	}

	rules := append([]datapath.RuleCounter(nil), snapshot.Counters.Rules...)
	sort.Slice(rules, func(i, j int) bool {
		return rules[i].CIDR < rules[j].CIDR
	})
	for _, rule := range rules {
		labels := cloneLabels(baseLabels)
		labels["cidr"] = rule.CIDR
		if err := writeMetric(w, "betternat_loxilb_rule_present", labels, 1); err != nil {
			return err
		}
		if err := writeMetric(w, "betternat_loxilb_rule_packets_total", labels, rule.Packets); err != nil {
			return err
		}
		if err := writeMetric(w, "betternat_loxilb_rule_bytes_total", labels, rule.Bytes); err != nil {
			return err
		}
	}
	owners := append([]OwnerCounter(nil), snapshot.Owners...)
	sort.Slice(owners, func(i, j int) bool {
		if owners[i].Owner == owners[j].Owner {
			return owners[i].Direction < owners[j].Direction
		}
		return owners[i].Owner < owners[j].Owner
	})
	for _, owner := range owners {
		labels := map[string]string{
			"gateway":   snapshot.GatewayID,
			"ha_group":  snapshot.HAGroupID,
			"owner":     owner.Owner,
			"direction": owner.Direction,
		}
		if err := writeMetric(w, "betternat_owner_packets_total", labels, owner.Packets); err != nil {
			return err
		}
		if err := writeMetric(w, "betternat_owner_bytes_total", labels, owner.Bytes); err != nil {
			return err
		}
	}
	if snapshot.Processed.Direction != "" {
		labels := map[string]string{
			"gateway":   snapshot.GatewayID,
			"ha_group":  snapshot.HAGroupID,
			"direction": snapshot.Processed.Direction,
		}
		if err := writeMetric(w, "betternat_processed_packets_total", labels, snapshot.Processed.Packets); err != nil {
			return err
		}
		if err := writeMetric(w, "betternat_processed_bytes_total", labels, snapshot.Processed.Bytes); err != nil {
			return err
		}
	}
	failoverEvents := append([]FailoverEventCounter(nil), snapshot.FailoverEvents...)
	sort.Slice(failoverEvents, func(i, j int) bool {
		if failoverEvents[i].Reason == failoverEvents[j].Reason {
			return failoverEvents[i].Result < failoverEvents[j].Result
		}
		return failoverEvents[i].Reason < failoverEvents[j].Reason
	})
	for _, event := range failoverEvents {
		labels := map[string]string{
			"gateway":  snapshot.GatewayID,
			"ha_group": snapshot.HAGroupID,
			"reason":   event.Reason,
			"result":   event.Result,
		}
		if err := writeMetric(w, "betternat_failover_events_total", labels, event.Count); err != nil {
			return err
		}
	}
	failoverDurations := append([]FailoverDuration(nil), snapshot.FailoverDurations...)
	sort.Slice(failoverDurations, func(i, j int) bool {
		return failoverDurations[i].Phase < failoverDurations[j].Phase
	})
	for _, duration := range failoverDurations {
		labels := map[string]string{
			"gateway":  snapshot.GatewayID,
			"ha_group": snapshot.HAGroupID,
			"phase":    duration.Phase,
		}
		if err := writeFloatMetric(w, "betternat_failover_duration_seconds", labels, duration.Seconds); err != nil {
			return err
		}
	}
	return nil
}

func writeMetric(w io.Writer, name string, labels map[string]string, value uint64) error {
	_, err := fmt.Fprintf(w, "%s{%s} %d\n", name, formatLabels(labels), value)
	return err
}

func writeFloatMetric(w io.Writer, name string, labels map[string]string, value float64) error {
	_, err := fmt.Fprintf(w, "%s{%s} %.6f\n", name, formatLabels(labels), value)
	return err
}

func formatLabels(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf(`%s="%s"`, key, escapeLabelValue(labels[key])))
	}
	return strings.Join(parts, ",")
}

func escapeLabelValue(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "\n", `\n`)
	return strings.ReplaceAll(value, `"`, `\"`)
}

func cloneLabels(labels map[string]string) map[string]string {
	cloned := make(map[string]string, len(labels)+1)
	for key, value := range labels {
		cloned[key] = value
	}
	return cloned
}
