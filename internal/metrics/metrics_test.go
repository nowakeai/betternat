package metrics

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nowakeai/betternat/internal/datapath"
)

func TestRenderPrometheus(t *testing.T) {
	snapshot := Snapshot{
		GatewayID:          `gw"one`,
		HAGroupID:          "ha\\a",
		Node:               "i-123",
		Version:            "v0",
		Commit:             "abc123",
		AgentUp:            true,
		Active:             true,
		HAState:            "active",
		HAStatusAgeSeconds: 1.5,
		LeaseGeneration:    42,
		Datapath:           datapath.Status{Ready: true, Engine: "loxilb"},
		Counters: datapath.Counters{
			Rules: []datapath.RuleCounter{
				{CIDR: "10.1.0.0/16", Packets: 20, Bytes: 2000},
				{CIDR: "10.0.0.0/16", Packets: 10, Bytes: 1000},
			},
		},
		Conntrack: datapath.ConntrackSummary{
			Entries:     7,
			Established: map[string]uint64{"udp": 2, "tcp": 4},
			UDPEntries:  2,
		},
		Owners: []OwnerCounter{
			{Owner: "crawler", Direction: "egress", Packets: 30, Bytes: 3000},
		},
		Processed: TrafficCounter{Direction: "egress", Packets: 30, Bytes: 3000},
		FailoverEvents: []FailoverEventCounter{
			{Reason: "lease_expired", Result: "success", Count: 1},
		},
		FailoverDurations: []FailoverDuration{
			{Phase: "replace_route", Seconds: 1.25},
		},
	}

	var out bytes.Buffer
	if err := RenderPrometheus(&out, snapshot); err != nil {
		t.Fatalf("render prometheus: %v", err)
	}
	text := out.String()

	assertContains(t, text, `betternat_datapath_ready{engine="loxilb",gateway="gw\"one",ha_group="ha\\a"} 1`)
	assertContains(t, text, `betternat_agent_up{gateway="gw\"one",ha_group="ha\\a",node="i-123"} 1`)
	assertContains(t, text, `betternat_agent_build_info{commit="abc123",gateway="gw\"one",ha_group="ha\\a",node="i-123",version="v0"} 1`)
	assertContains(t, text, `betternat_active{gateway="gw\"one",ha_group="ha\\a",node="i-123"} 1`)
	assertContains(t, text, `betternat_ha_state{gateway="gw\"one",ha_group="ha\\a",node="i-123",state="active"} 1`)
	assertContains(t, text, `betternat_ha_status_age_seconds{gateway="gw\"one",ha_group="ha\\a",node="i-123"} 1.500000`)
	assertContains(t, text, `betternat_ha_status_stale{gateway="gw\"one",ha_group="ha\\a",node="i-123"} 0`)
	assertContains(t, text, `betternat_lease_generation{gateway="gw\"one",ha_group="ha\\a",node="i-123"} 42`)
	assertContains(t, text, `betternat_datapath_engine_info{engine="loxilb",gateway="gw\"one",ha_group="ha\\a"} 1`)
	assertContains(t, text, `betternat_conntrack_entries{engine="loxilb",gateway="gw\"one",ha_group="ha\\a"} 7`)
	assertContains(t, text, `betternat_conntrack_established{engine="loxilb",gateway="gw\"one",ha_group="ha\\a",protocol="tcp"} 4`)
	assertContains(t, text, `betternat_loxilb_rule_present{cidr="10.0.0.0/16",engine="loxilb",gateway="gw\"one",ha_group="ha\\a"} 1`)
	assertContains(t, text, `betternat_loxilb_rule_packets_total{cidr="10.0.0.0/16",engine="loxilb",gateway="gw\"one",ha_group="ha\\a"} 10`)
	assertContains(t, text, `betternat_owner_packets_total{direction="egress",gateway="gw\"one",ha_group="ha\\a",owner="crawler"} 30`)
	assertContains(t, text, `betternat_owner_bytes_total{direction="egress",gateway="gw\"one",ha_group="ha\\a",owner="crawler"} 3000`)
	assertContains(t, text, `betternat_processed_packets_total{direction="egress",gateway="gw\"one",ha_group="ha\\a"} 30`)
	assertContains(t, text, `betternat_processed_bytes_total{direction="egress",gateway="gw\"one",ha_group="ha\\a"} 3000`)
	assertContains(t, text, `betternat_failover_events_total{gateway="gw\"one",ha_group="ha\\a",reason="lease_expired",result="success"} 1`)
	assertContains(t, text, `betternat_failover_duration_seconds{gateway="gw\"one",ha_group="ha\\a",phase="replace_route"} 1.250000`)

	firstRule := strings.Index(text, `cidr="10.0.0.0/16"`)
	secondRule := strings.Index(text, `cidr="10.1.0.0/16"`)
	if firstRule == -1 || secondRule == -1 || firstRule > secondRule {
		t.Fatalf("rules are not rendered in CIDR order:\n%s", text)
	}
}

func TestRenderPrometheusNilWriter(t *testing.T) {
	if err := RenderPrometheus(nil, Snapshot{}); err != nil {
		t.Fatalf("nil writer should be ignored: %v", err)
	}
}

func assertContains(t *testing.T, text string, want string) {
	t.Helper()
	if !strings.Contains(text, want) {
		t.Fatalf("missing %q in:\n%s", want, text)
	}
}
