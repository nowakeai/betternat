package gcp

import (
	"strings"
	"testing"
)

func TestGatewayStartupScriptConfiguresForwardingAndMasquerade(t *testing.T) {
	script := GatewayStartupScript(StartupScriptInputs{
		PrivateCIDRs: []string{"10.91.0.0/24", "10.92.0.0/24"},
	})
	for _, want := range []string{
		"net.ipv4.ip_forward=1",
		"apt-get install -y nftables curl",
		"nft add rule ip nat postrouting ip saddr 10.91.0.0/24 masquerade",
		"nft add rule ip nat postrouting ip saddr 10.92.0.0/24 masquerade",
		"checkip.amazonaws.com",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("startup script missing %q:\n%s", want, script)
		}
	}
}

func TestClientVerifyStartupScriptWritesSerialEvidence(t *testing.T) {
	script := ClientVerifyStartupScript()
	for _, want := range []string{
		"exec >/dev/console 2>&1",
		"BETTERNAT_GCP_VERIFY_START",
		"BETTERNAT_GCP_EGRESS_IP=",
		"BETTERNAT_GCP_VERIFY_OK",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("verify startup script missing %q:\n%s", want, script)
		}
	}
}

func TestGatewayNamesDefaultToTwoGateways(t *testing.T) {
	got := gatewayNames("prod-egress", 0)
	want := []string{"prod-egress-gw-a", "prod-egress-gw-b"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected gateway names: %#v", got)
	}
}

func TestInputsValidateRequiredFields(t *testing.T) {
	err := (Inputs{Name: "prod"}).validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	for _, want := range []string{"project_id", "region", "zone", "network", "subnetwork", "client_tag", "route_name"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("validation error missing %q: %v", want, err)
		}
	}
}
