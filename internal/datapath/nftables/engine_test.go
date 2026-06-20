package nftables

import (
	"context"
	"strings"
	"testing"

	"github.com/betternat/betternat/internal/config"
)

func TestReconcileAddsMissingMasqueradeRules(t *testing.T) {
	runner := &fakeRunner{
		outputs: map[string][]byte{
			"nft list ruleset": []byte(`table inet betternat {}`),
		},
	}
	engine := NewWithRunner(runner)

	err := engine.Reconcile(context.Background(), testConfig())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !runner.hasCall("nft add table inet betternat") {
		t.Fatalf("missing add table call: %#v", runner.calls)
	}
	if !runner.hasCall("nft add rule inet betternat betternat_postrouting ip saddr 10.0.0.0/8 counter masquerade comment \"betternat:10.0.0.0/8\"") {
		t.Fatalf("missing masquerade rule call: %#v", runner.calls)
	}
}

func TestReconcileSkipsExistingRule(t *testing.T) {
	runner := &fakeRunner{
		outputs: map[string][]byte{
			"nft list ruleset": []byte(`counter packets 1 bytes 2 masquerade comment "betternat:10.0.0.0/8"`),
		},
	}
	engine := NewWithRunner(runner)

	err := engine.Reconcile(context.Background(), testConfig())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	for _, call := range runner.calls {
		if strings.Contains(call, "add rule") {
			t.Fatalf("unexpected add rule call: %#v", runner.calls)
		}
	}
}

func TestCountersParsesRuleset(t *testing.T) {
	runner := &fakeRunner{
		outputs: map[string][]byte{
			"nft list ruleset": []byte(`counter packets 12 bytes 3456 masquerade comment "betternat:10.0.0.0/8"`),
		},
	}
	engine := NewWithRunner(runner)

	counters, err := engine.Counters(context.Background())
	if err != nil {
		t.Fatalf("counters: %v", err)
	}
	if len(counters.Rules) != 1 || counters.Rules[0].Packets != 12 || counters.Rules[0].Bytes != 3456 {
		t.Fatalf("unexpected counters: %#v", counters)
	}
}

func TestConntrackSummaryParsesConntrackList(t *testing.T) {
	runner := &fakeRunner{
		outputs: map[string][]byte{
			"conntrack -L": []byte(`tcp      6 431999 ESTABLISHED src=10.0.1.10 dst=93.184.216.34 sport=50000 dport=443 [ASSURED]
udp      17 29 src=10.0.1.20 dst=1.1.1.1 sport=40000 dport=53 [ASSURED]
icmp     1 29 src=10.0.1.30 dst=8.8.8.8 type=8 code=0 id=1234
`),
		},
	}
	engine := NewWithRunner(runner)

	summary, err := engine.ConntrackSummary(context.Background())
	if err != nil {
		t.Fatalf("conntrack summary: %v", err)
	}
	if summary.Entries != 3 {
		t.Fatalf("entries = %d", summary.Entries)
	}
	if summary.Established["tcp"] != 1 {
		t.Fatalf("tcp established = %d", summary.Established["tcp"])
	}
	if summary.Established["udp"] != 1 || summary.UDPEntries != 1 {
		t.Fatalf("unexpected udp summary: %#v", summary)
	}
}

func TestReconcileRejectsInvalidCIDR(t *testing.T) {
	cfg := testConfig()
	cfg.PrivateCIDRs = []string{"nope"}
	err := NewWithRunner(&fakeRunner{}).Reconcile(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected invalid cidr error")
	}
}

type fakeRunner struct {
	calls   []string
	outputs map[string][]byte
	errs    map[string]error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := strings.Join(append([]string{name}, args...), " ")
	f.calls = append(f.calls, call)
	if f.errs != nil && f.errs[call] != nil {
		return f.outputs[call], f.errs[call]
	}
	if f.outputs != nil {
		return f.outputs[call], nil
	}
	return nil, nil
}

func (f *fakeRunner) hasCall(want string) bool {
	for _, call := range f.calls {
		if call == want {
			return true
		}
	}
	return false
}

func testConfig() config.DatapathConfig {
	return config.DatapathConfig{
		PrivateCIDRs: []string{"10.0.0.0/8"},
		Nftables: config.NftablesConfig{
			TableName:   "betternat",
			ChainPrefix: "betternat",
		},
	}
}
