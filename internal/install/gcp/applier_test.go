package gcp

import (
	"reflect"
	"strings"
	"testing"
	"time"
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

func TestGatewayInstanceTemplateMatchesGatewayBootstrap(t *testing.T) {
	template := gatewayInstanceTemplate(Inputs{
		Name:                "prod-egress",
		ProjectID:           "shared-resources-alt",
		Region:              "us-west1",
		Zone:                "us-west1-a",
		Subnetwork:          "public-a",
		MachineType:         "e2-medium",
		ServiceAccountEmail: "betternat-runtime@shared-resources-alt.iam.gserviceaccount.com",
		StartupScript:       "#!/bin/bash\necho bnat\n",
		Labels:              map[string]string{"betternat": "true"},
	})
	if template.Name != "prod-egress-template" {
		t.Fatalf("unexpected template name: %s", template.Name)
	}
	props := template.Properties
	if props == nil || !props.CanIpForward || props.MachineType != "e2-medium" {
		t.Fatalf("unexpected template properties: %#v", props)
	}
	if len(props.NetworkInterfaces) != 1 || props.NetworkInterfaces[0].Subnetwork == "" {
		t.Fatalf("missing template network interface: %#v", props.NetworkInterfaces)
	}
	if props.Tags != nil {
		for _, tag := range props.Tags.Items {
			if tag == "" {
				t.Fatalf("template should not include empty network tags: %#v", props.Tags)
			}
		}
	}
	if len(props.ServiceAccounts) != 1 || props.ServiceAccounts[0].Email == "" {
		t.Fatalf("missing template service account: %#v", props.ServiceAccounts)
	}
}

func TestGatewayInstanceGroupManagerUsesTemplateAndTargetSize(t *testing.T) {
	mig := gatewayInstanceGroupManager(Inputs{
		Name:         "prod-egress",
		ProjectID:    "shared-resources-alt",
		Zone:         "us-west1-a",
		GatewayCount: 2,
	})
	if mig.Name != "prod-egress-mig" || mig.TargetSize != 2 {
		t.Fatalf("unexpected mig: %#v", mig)
	}
	if !strings.Contains(mig.InstanceTemplate, "global/instanceTemplates/prod-egress-template") {
		t.Fatalf("unexpected template link: %s", mig.InstanceTemplate)
	}
	if !strings.Contains(mig.Zone, "zones/us-west1-a") {
		t.Fatalf("unexpected zone link: %s", mig.Zone)
	}
}

func TestInputsValidateCapacityRepairMode(t *testing.T) {
	inputs := validInputs()
	inputs.CapacityRepairMode = "mig"
	if err := inputs.validate(); err != nil {
		t.Fatalf("mig mode should validate: %v", err)
	}
	inputs.CapacityRepairMode = "custom"
	if err := inputs.validate(); err == nil || !strings.Contains(err.Error(), "capacity_repair_mode") {
		t.Fatalf("expected capacity repair validation error, got %v", err)
	}
}

func TestRuntimeIAMPermissions(t *testing.T) {
	got := RuntimeIAMPermissions()
	want := []string{
		"compute.addresses.get",
		"compute.globalOperations.get",
		"compute.instances.addAccessConfig",
		"compute.instances.deleteAccessConfig",
		"compute.instances.get",
		"compute.instances.use",
		"compute.networks.get",
		"compute.networks.updatePolicy",
		"compute.routes.create",
		"compute.routes.delete",
		"compute.routes.get",
		"compute.zoneOperations.get",
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

func validInputs() Inputs {
	return Inputs{
		Name:              "prod-egress",
		ProjectID:         "shared-resources-alt",
		Region:            "us-west1",
		Zone:              "us-west1-a",
		Network:           "bnat-net",
		Subnetwork:        "public-a",
		ClientTag:         "private-client",
		RouteName:         "prod-default",
		GatewayCount:      2,
		PrivateCIDRs:      []string{"10.0.0.0/8"},
		OperationPollTime: time.Nanosecond,
	}
}
