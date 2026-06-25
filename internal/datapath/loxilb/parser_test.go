package loxilb

import "testing"

func TestParseFirewallTreatsEmptyFirewallOutputAsNoRules(t *testing.T) {
	rules, err := parseFirewall([]byte("Error: no firewall rules found\n"))
	if err != nil {
		t.Fatalf("parse empty firewall output: %v", err)
	}
	if len(rules) != 0 {
		t.Fatalf("expected no rules, got %#v", rules)
	}
}

func TestParseFirewallRejectsUnexpectedNonJSON(t *testing.T) {
	if _, err := parseFirewall([]byte("Error: loxilb API unavailable\n")); err == nil {
		t.Fatal("expected unexpected non-json output to fail")
	}
}

func TestParseFirewallCounters(t *testing.T) {
	data := []byte(`{
	  "fwAttr": [
	    {
	      "ruleArguments": {
	        "sourceIP": "10.77.2.0/24",
	        "destinationIP": "0.0.0.0/0",
	        "preference": 100
	      },
	      "opts": {
	        "doSnat": true,
	        "toIP": "10.77.1.65",
	        "counter": "10821:155385172"
	      }
	    }
	  ]
	}`)

	counters, err := parseFirewallCounters(data)
	if err != nil {
		t.Fatalf("parse counters: %v", err)
	}
	if len(counters.Rules) != 1 {
		t.Fatalf("rule counters len = %d", len(counters.Rules))
	}
	got := counters.Rules[0]
	if got.CIDR != "10.77.2.0/24" || got.Packets != 10821 || got.Bytes != 155385172 {
		t.Fatalf("unexpected counter: %+v", got)
	}
}

func TestParseConntrackSummary(t *testing.T) {
	data := []byte(`{
	  "ctAttr": [
	    {"protocol":"tcp","conntrackState":"est","conntrackAct":"snat-10.77.1.65:34436:w0"},
	    {"protocol":"tcp","conntrackState":"est","conntrackAct":"dnat-10.77.2.87:34436:w0"},
	    {"protocol":"udp","conntrackState":"udp-est","conntrackAct":"snat-10.77.1.65:38502:w0"}
	  ]
	}`)

	summary, err := parseConntrackSummary(data)
	if err != nil {
		t.Fatalf("parse conntrack: %v", err)
	}
	if summary.Entries != 3 {
		t.Fatalf("entries = %d", summary.Entries)
	}
	if summary.Established["tcp"] != 2 {
		t.Fatalf("tcp established = %d", summary.Established["tcp"])
	}
	if summary.UDPEntries != 1 {
		t.Fatalf("udp entries = %d", summary.UDPEntries)
	}
}
