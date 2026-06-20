package config

import (
	"strings"
	"testing"
)

func TestLoadValidJSON(t *testing.T) {
	cfg, err := Load(strings.NewReader(`{
	  "version": "v0",
	  "gateway_id": "prod-egress",
	  "ha_group_id": "prod-egress-a",
	  "cloud": "aws",
	  "region": "us-west-2",
	  "local": {"primary_interface": "ens5"},
	  "datapath": {
	    "engine": "loxilb",
	    "fallback_engine": "nftables",
	    "private_cidrs": ["10.0.0.0/8"],
	    "loxilb": {
	      "api_address": "127.0.0.1",
	      "api_port": 11111,
	      "snat_to": "auto",
	      "snat_interface": "ens5"
	    }
	  },
	  "ha": {},
	  "observability": {},
	  "rollback": {}
	}`))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Datapath.LoxiLB.SNATInterface != "ens5" {
		t.Fatalf("snat interface = %q", cfg.Datapath.LoxiLB.SNATInterface)
	}
}

func TestLoadValidYAML(t *testing.T) {
	cfg, err := Load(strings.NewReader(`
version: v0
gateway_id: prod-egress
ha_group_id: prod-egress-a
cloud: aws
region: us-west-2
local:
  primary_interface: ens5
datapath:
  engine: loxilb
  fallback_engine: nftables
  private_cidrs:
    - 10.0.0.0/8
  loxilb:
    api_address: 127.0.0.1
    api_port: 11111
    snat_to: auto
    snat_interface: ens5
ha: {}
observability: {}
rollback: {}
`))
	if err != nil {
		t.Fatalf("load yaml config: %v", err)
	}
	if cfg.GatewayID != "prod-egress" {
		t.Fatalf("gateway id = %q", cfg.GatewayID)
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	_, err := Load(strings.NewReader(`{
	  "version": "v0",
	  "gateway_id": "prod-egress",
	  "cloud": "aws",
	  "region": "us-west-2",
	  "datapath": {
	    "engine": "loxilb",
	    "private_cidrs": ["10.0.0.0/8"],
	    "loxilb": {"snat_to": "10.0.0.1"}
	  },
	  "unexpected": true
	}`))
	if err == nil {
		t.Fatal("expected unknown field error")
	}
}

func TestLoadYAMLRejectsUnknownField(t *testing.T) {
	_, err := Load(strings.NewReader(`
version: v0
gateway_id: prod-egress
cloud: aws
region: us-west-2
datapath:
  engine: loxilb
  private_cidrs: ["10.0.0.0/8"]
  loxilb:
    snat_to: 10.0.0.1
unexpected: true
`))
	if err == nil {
		t.Fatal("expected yaml unknown field error")
	}
}

func TestValidateRequiresSNATInterfaceForAuto(t *testing.T) {
	err := (Config{
		Version:   "v0",
		GatewayID: "prod-egress",
		Cloud:     "aws",
		Region:    "us-west-2",
		Datapath: DatapathConfig{
			Engine:       "loxilb",
			PrivateCIDRs: []string{"10.0.0.0/8"},
			LoxiLB:       LoxiLBConfig{SNATTo: "auto"},
		},
	}).Validate()
	if err == nil {
		t.Fatal("expected snat interface validation error")
	}
}

func TestValidateRequiresOutboundProbeURLWhenEnabled(t *testing.T) {
	err := (Config{
		Version:   "v0",
		GatewayID: "prod-egress",
		Cloud:     "aws",
		Region:    "us-west-2",
		Datapath: DatapathConfig{
			Engine:       "loxilb",
			PrivateCIDRs: []string{"10.0.0.0/8"},
			LoxiLB:       LoxiLBConfig{SNATTo: "10.0.0.1"},
		},
		Observability: ObservabilityConfig{
			OutboundProbe: OutboundProbeConfig{Enabled: true},
		},
	}).Validate()
	if err == nil {
		t.Fatal("expected outbound probe url validation error")
	}
}
