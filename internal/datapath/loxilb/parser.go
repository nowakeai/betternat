package loxilb

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/betternat/betternat/internal/datapath"
)

type firewallResponse struct {
	FirewallRules []firewallRule `json:"fwAttr"`
}

type firewallRule struct {
	Arguments firewallRuleArguments `json:"ruleArguments"`
	Options   firewallRuleOptions   `json:"opts"`
}

type firewallRuleArguments struct {
	SourceIP      string `json:"sourceIP"`
	DestinationIP string `json:"destinationIP"`
	Preference    int    `json:"preference"`
}

type firewallRuleOptions struct {
	DoSNAT    bool   `json:"doSnat"`
	ToIP      string `json:"toIP"`
	ToPort    int    `json:"toPort"`
	OnDefault bool   `json:"onDefault"`
	Counter   string `json:"counter"`
}

type conntrackResponse struct {
	Entries []conntrackEntry `json:"ctAttr"`
}

type conntrackEntry struct {
	Protocol        string `json:"protocol"`
	ConntrackState  string `json:"conntrackState"`
	ConntrackAction string `json:"conntrackAct"`
	Packets         uint64 `json:"packets"`
	Bytes           uint64 `json:"bytes"`
}

func parseFirewall(data []byte) ([]firewallRule, error) {
	var resp firewallResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse loxilb firewall json: %w", err)
	}
	return resp.FirewallRules, nil
}

func parseFirewallCounters(data []byte) (datapath.Counters, error) {
	rules, err := parseFirewall(data)
	if err != nil {
		return datapath.Counters{}, err
	}

	counters := datapath.Counters{Rules: make([]datapath.RuleCounter, 0, len(rules))}
	for _, rule := range rules {
		if !rule.Options.DoSNAT {
			continue
		}
		packets, bytes, err := parseCounter(rule.Options.Counter)
		if err != nil {
			return datapath.Counters{}, err
		}
		counters.Rules = append(counters.Rules, datapath.RuleCounter{
			CIDR:    rule.Arguments.SourceIP,
			Packets: packets,
			Bytes:   bytes,
		})
	}
	return counters, nil
}

func parseCounter(counter string) (uint64, uint64, error) {
	parts := strings.Split(counter, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid loxilb counter %q", counter)
	}
	packets, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse loxilb packet counter %q: %w", counter, err)
	}
	bytes, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse loxilb byte counter %q: %w", counter, err)
	}
	return packets, bytes, nil
}

func parseConntrackSummary(data []byte) (datapath.ConntrackSummary, error) {
	var resp conntrackResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return datapath.ConntrackSummary{}, fmt.Errorf("parse loxilb conntrack json: %w", err)
	}

	summary := datapath.ConntrackSummary{
		Entries:     uint64(len(resp.Entries)),
		Established: map[string]uint64{},
	}
	for _, entry := range resp.Entries {
		proto := strings.ToLower(entry.Protocol)
		state := strings.ToLower(entry.ConntrackState)
		switch {
		case proto == "udp":
			summary.UDPEntries++
		case state == "est":
			summary.Established[proto]++
		}
	}
	return summary, nil
}
