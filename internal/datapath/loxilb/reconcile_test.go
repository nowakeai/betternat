package loxilb

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/nowakeai/betternat/internal/config"
)

type fakeRunner struct {
	outputs         map[string][]byte
	outputSequences map[string][][]byte
	resultSequences map[string][]fakeRunResult
	calls           [][]string
}

type fakeRunResult struct {
	out []byte
	err error
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
	if sequence := r.resultSequences[key]; len(sequence) > 0 {
		result := sequence[0]
		r.resultSequences[key] = sequence[1:]
		return result.out, result.err
	}
	if sequence := r.outputSequences[key]; len(sequence) > 0 {
		out := sequence[0]
		r.outputSequences[key] = sequence[1:]
		return out, nil
	}
	if out, ok := r.outputs[key]; ok {
		return out, nil
	}
	return []byte(`{"ok":true}`), nil
}

func TestReconcileRetriesTransientLoxiLBRestartErrors(t *testing.T) {
	restoreAttempts := reconcileAttempts
	restoreBackoff := reconcileBackoff
	defer func() {
		reconcileAttempts = restoreAttempts
		reconcileBackoff = restoreBackoff
	}()
	reconcileAttempts = 4
	reconcileBackoff = 0

	runner := &fakeRunner{
		outputs: map[string][]byte{
			"get lbversion -o json": []byte(`{"version":"0.9.8.6-beta"}`),
		},
		outputSequences: map[string][][]byte{
			"get firewall -o json": {
				[]byte("Error: loxilb firewall API warming up\n"),
				[]byte(`{"fwAttr":[]}`),
				[]byte(`{"fwAttr":[]}`),
			},
		},
		resultSequences: map[string][]fakeRunResult{
			"create firewall --firewallRule=sourceIP:10.77.2.0/24,preference:100 --snat=10.77.1.65 --egress": {
				{err: errors.New("signal: killed")},
				{out: []byte(`{"ok":true}`)},
			},
		},
	}
	engine := NewWithRunner(runner)
	cfg := config.DatapathConfig{
		PrivateCIDRs: []string{"10.77.2.0/24"},
		LoxiLB: config.LoxiLBConfig{
			SNATTo:             "10.77.1.65",
			RulePreferenceBase: 100,
		},
	}

	if err := engine.Reconcile(context.Background(), cfg); err != nil {
		t.Fatalf("reconcile should retry transient restart errors: %v", err)
	}
	if got := createCalls(runner.calls); got != 2 {
		t.Fatalf("create calls = %d, want 2: %#v", got, runner.calls)
	}
}

func TestReconcileReplaysRulesAfterLoxiLBRestartRuleLoss(t *testing.T) {
	runner := &fakeRunner{
		outputs: map[string][]byte{
			"get lbversion -o json": []byte(`{"version":"0.9.8.6-beta"}`),
		},
		outputSequences: map[string][][]byte{
			"get firewall -o json": {
				[]byte(`{
				  "fwAttr": [
				    {
				      "ruleArguments": {"sourceIP":"10.77.2.0/24","preference":100},
				      "opts": {"doSnat":true,"toIP":"10.77.1.65","counter":"1:2"}
				    },
				    {
				      "ruleArguments": {"sourceIP":"10.77.3.0/24","preference":101},
				      "opts": {"doSnat":true,"toIP":"10.77.1.65","counter":"3:4"}
				    }
				  ]
				}`),
				[]byte(`{"fwAttr":[]}`),
			},
		},
	}
	engine := NewWithRunner(runner)
	cfg := config.DatapathConfig{
		PrivateCIDRs: []string{"10.77.2.0/24", "10.77.3.0/24"},
		LoxiLB: config.LoxiLBConfig{
			SNATTo:             "10.77.1.65",
			RulePreferenceBase: 100,
		},
	}

	if err := engine.Reconcile(context.Background(), cfg); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if createCalls(runner.calls) != 0 {
		t.Fatalf("first reconcile should not create existing rules: %#v", runner.calls)
	}

	if err := engine.Reconcile(context.Background(), cfg); err != nil {
		t.Fatalf("second reconcile after restart rule loss: %v", err)
	}

	gotCreates := createCallArgs(runner.calls)
	wantCreates := [][]string{
		{
			"create", "firewall",
			"--firewallRule=sourceIP:10.77.2.0/24,preference:100",
			"--snat=10.77.1.65",
			"--egress",
		},
		{
			"create", "firewall",
			"--firewallRule=sourceIP:10.77.3.0/24,preference:101",
			"--snat=10.77.1.65",
			"--egress",
		},
	}
	if !reflect.DeepEqual(gotCreates, wantCreates) {
		t.Fatalf("create calls = %#v, want %#v", gotCreates, wantCreates)
	}
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

func createCalls(calls [][]string) int {
	return len(createCallArgs(calls))
}

func createCallArgs(calls [][]string) [][]string {
	var result [][]string
	for _, call := range calls {
		if len(call) > 0 && call[0] == "create" {
			result = append(result, call)
		}
	}
	return result
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
