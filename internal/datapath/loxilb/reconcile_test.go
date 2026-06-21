package loxilb

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/nowakeai/betternat/internal/config"
)

type fakeRunner struct {
	outputs map[string][]byte
	calls   [][]string
}

type fakeIPResolver struct {
	ips map[string]string
}

func (r fakeIPResolver) IPv4ByInterface(name string) (string, error) {
	ip, ok := r.ips[name]
	if !ok {
		return "", fmt.Errorf("missing fake IP for %s", name)
	}
	return ip, nil
}

func (r *fakeRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	copied := append([]string(nil), args...)
	r.calls = append(r.calls, copied)
	key := strings.Join(args, " ")
	if out, ok := r.outputs[key]; ok {
		return out, nil
	}
	return []byte(`{"ok":true}`), nil
}

func TestReconcileCreatesMissingRule(t *testing.T) {
	runner := &fakeRunner{outputs: map[string][]byte{
		"get lbversion -o json": []byte(`{"version":"0.9.8.6-beta"}`),
		"get firewall -o json":  []byte(`{"fwAttr":[]}`),
	}}
	engine := NewWithRunner(runner)
	cfg := config.DatapathConfig{
		PrivateCIDRs: []string{"10.77.2.0/24"},
		LoxiLB: config.LoxiLBConfig{
			SNATTo:             "10.77.1.65",
			RulePreferenceBase: 100,
		},
	}

	if err := engine.Reconcile(context.Background(), cfg); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	wantLast := []string{
		"create", "firewall",
		"--firewallRule=sourceIP:10.77.2.0/24,preference:100",
		"--snat=10.77.1.65",
		"--egress",
	}
	if !reflect.DeepEqual(runner.calls[len(runner.calls)-1], wantLast) {
		t.Fatalf("last call = %#v, want %#v", runner.calls[len(runner.calls)-1], wantLast)
	}
}

func TestReconcileSkipsExistingRule(t *testing.T) {
	runner := &fakeRunner{outputs: map[string][]byte{
		"get lbversion -o json": []byte(`{"version":"0.9.8.6-beta"}`),
		"get firewall -o json": []byte(`{
		  "fwAttr": [{
		    "ruleArguments": {"sourceIP":"10.77.2.0/24","preference":100},
		    "opts": {"doSnat":true,"toIP":"10.77.1.65","counter":"1:2"}
		  }]
		}`),
	}}
	engine := NewWithRunner(runner)
	cfg := config.DatapathConfig{
		PrivateCIDRs: []string{"10.77.2.0/24"},
		LoxiLB: config.LoxiLBConfig{
			SNATTo:             "10.77.1.65",
			RulePreferenceBase: 100,
		},
	}

	if err := engine.Reconcile(context.Background(), cfg); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	for _, call := range runner.calls {
		if len(call) > 0 && call[0] == "create" {
			t.Fatalf("unexpected create call: %#v", call)
		}
	}
}

func TestDesiredRulesRejectsAutoSNAT(t *testing.T) {
	_, err := desiredRules(config.DatapathConfig{
		PrivateCIDRs: []string{"10.77.2.0/24"},
		LoxiLB:       config.LoxiLBConfig{SNATTo: "auto"},
	})
	if err == nil {
		t.Fatal("expected auto snat_to error")
	}
}

func TestReconcileResolvesAutoSNAT(t *testing.T) {
	runner := &fakeRunner{outputs: map[string][]byte{
		"get lbversion -o json": []byte(`{"version":"0.9.8.6-beta"}`),
		"get firewall -o json":  []byte(`{"fwAttr":[]}`),
	}}
	engine := NewWithDeps(runner, fakeIPResolver{ips: map[string]string{"ens5": "10.77.1.65"}})
	cfg := config.DatapathConfig{
		PrivateCIDRs: []string{"10.77.2.0/24"},
		LoxiLB: config.LoxiLBConfig{
			SNATTo:             "auto",
			SNATInterface:      "ens5",
			RulePreferenceBase: 100,
		},
	}

	if err := engine.Reconcile(context.Background(), cfg); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	wantLast := []string{
		"create", "firewall",
		"--firewallRule=sourceIP:10.77.2.0/24,preference:100",
		"--snat=10.77.1.65",
		"--egress",
	}
	if !reflect.DeepEqual(runner.calls[len(runner.calls)-1], wantLast) {
		t.Fatalf("last call = %#v, want %#v", runner.calls[len(runner.calls)-1], wantLast)
	}
}
