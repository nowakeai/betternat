package gcp

import (
	"reflect"
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

func TestGatewayInstanceAttachesRuntimeServiceAccount(t *testing.T) {
	inst := gatewayInstance(Inputs{
		Name:                "prod-egress",
		ProjectID:           "shared-resources-alt",
		Region:              "us-west1",
		Zone:                "us-west1-a",
		Subnetwork:          "public-a",
		ServiceAccountEmail: "betternat-runtime@shared-resources-alt.iam.gserviceaccount.com",
	}, "prod-egress-gw-a")
	if len(inst.ServiceAccounts) != 1 {
		t.Fatalf("expected service account on instance: %#v", inst.ServiceAccounts)
	}
	if got := inst.ServiceAccounts[0].Email; got != "betternat-runtime@shared-resources-alt.iam.gserviceaccount.com" {
		t.Fatalf("unexpected service account: %s", got)
	}
	if len(inst.ServiceAccounts[0].Scopes) != 1 || inst.ServiceAccounts[0].Scopes[0] != "https://www.googleapis.com/auth/cloud-platform" {
		t.Fatalf("unexpected service account scopes: %#v", inst.ServiceAccounts[0].Scopes)
	}
}

func TestRuntimeIAMPermissions(t *testing.T) {
	got := RuntimeIAMPermissions()
	want := []string{
		"compute.globalOperations.get",
		"compute.instances.get",
		"compute.instances.use",
		"compute.networks.get",
		"compute.networks.updatePolicy",
		"compute.routes.create",
		"compute.routes.delete",
		"compute.routes.get",
		"datastore.databases.get",
		"datastore.databases.getMetadata",
		"datastore.databases.list",
		"datastore.entities.allocateIds",
		"datastore.entities.create",
		"datastore.entities.delete",
		"datastore.entities.get",
		"datastore.entities.list",
		"datastore.entities.update",
		"datastore.namespaces.get",
		"datastore.namespaces.list",
		"datastore.schemas.list",
		"datastore.statistics.get",
		"datastore.statistics.list",
		"resourcemanager.projects.get",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected runtime IAM permissions:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestReadStatusKeepsGatewayWhenOnlyRouteIsMissing(t *testing.T) {
	if got := readStatus(0, ""); got != "missing" {
		t.Fatalf("zero instances should be missing, got %q", got)
	}
	if got := readStatus(2, ""); got != "degraded" {
		t.Fatalf("instances without route should be degraded, got %q", got)
	}
	if got := readStatus(2, "prod-gw-a"); got != "active" {
		t.Fatalf("instances with route target should be active, got %q", got)
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
