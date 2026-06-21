package doctor

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/betternat/betternat/internal/cloud"
	"github.com/betternat/betternat/internal/config"
	"github.com/betternat/betternat/internal/datapath"
	"github.com/betternat/betternat/internal/iamcheck"
	"github.com/betternat/betternat/internal/lease"
	"github.com/betternat/betternat/internal/probe"
)

func TestRunReportsWorstStatus(t *testing.T) {
	report := Run(context.Background(), []Checker{
		staticChecker{result: CheckResult{Name: "ok", Status: StatusOK}},
		staticChecker{result: CheckResult{Name: "warn", Status: StatusWarning}},
	})
	if report.Status != StatusWarning {
		t.Fatalf("unexpected report status: %#v", report)
	}
}

func TestDatapathCheckerOK(t *testing.T) {
	result := DatapathChecker{Engine: &fakeEngine{ready: true}}.Check(context.Background())
	if result.Status != StatusOK {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestDatapathCheckerWarnsWhenNotReady(t *testing.T) {
	result := DatapathChecker{Engine: &fakeEngine{ready: false}}.Check(context.Background())
	if result.Status != StatusWarning {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestDatapathCheckerCriticalOnCounterFailure(t *testing.T) {
	result := DatapathChecker{Engine: &fakeEngine{ready: true, countersErr: errors.New("no counters")}}.Check(context.Background())
	if result.Status != StatusCritical {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestIAMCheckerOK(t *testing.T) {
	result := IAMChecker{Evaluator: fakeIAMEvaluator{}}.Check(context.Background())
	if result.Status != StatusOK {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestIAMCheckerReportsMissingActions(t *testing.T) {
	result := IAMChecker{Evaluator: fakeIAMEvaluator{missing: []string{"ec2:ReplaceRoute"}}}.Check(context.Background())
	if result.Status != StatusCritical {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestASGCheckerOK(t *testing.T) {
	result := ASGChecker{
		Inspector: fakeASGInspector{info: cloud.ASGInfo{
			Name:            "betternat-prod-us-west-2a",
			DesiredCapacity: 2,
			Instances: []cloud.ASGInstance{
				{InstanceID: "i-a", LifecycleState: "InService", HealthStatus: "Healthy"},
				{InstanceID: "i-b", LifecycleState: "InService", HealthStatus: "Healthy"},
			},
		}},
		Name:      "betternat-prod-us-west-2a",
		HAEnabled: true,
	}.Check(context.Background())
	if result.Status != StatusOK {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestASGCheckerReportsUnhealthyCapacity(t *testing.T) {
	result := ASGChecker{
		Inspector: fakeASGInspector{info: cloud.ASGInfo{
			Name:            "betternat-prod-us-west-2a",
			DesiredCapacity: 2,
			Instances: []cloud.ASGInstance{
				{InstanceID: "i-a", LifecycleState: "InService", HealthStatus: "Healthy"},
				{InstanceID: "i-b", LifecycleState: "Pending", HealthStatus: "Healthy"},
			},
		}},
		Name:      "betternat-prod-us-west-2a",
		HAEnabled: true,
	}.Check(context.Background())
	if result.Status != StatusCritical {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestASGCheckerWarnsWithoutStandbyCapacity(t *testing.T) {
	result := ASGChecker{
		Inspector: fakeASGInspector{info: cloud.ASGInfo{
			Name:            "betternat-prod-us-west-2a",
			DesiredCapacity: 1,
			Instances: []cloud.ASGInstance{
				{InstanceID: "i-a", LifecycleState: "InService", HealthStatus: "Healthy"},
			},
		}},
		Name:      "betternat-prod-us-west-2a",
		HAEnabled: true,
	}.Check(context.Background())
	if result.Status != StatusWarning {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestRouteChecker(t *testing.T) {
	result := RouteChecker{
		Cloud:           fakeCloud{route: cloud.RouteTarget{Target: "i-active"}},
		RouteTableID:    "rtb-a",
		DestinationCIDR: "0.0.0.0/0",
		ExpectedTarget:  "i-active",
	}.Check(context.Background())
	if result.Status != StatusOK {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestRouteCheckerDetectsMismatch(t *testing.T) {
	result := RouteChecker{
		Cloud:           fakeCloud{route: cloud.RouteTarget{Target: "i-other"}},
		RouteTableID:    "rtb-a",
		DestinationCIDR: "0.0.0.0/0",
		ExpectedTarget:  "i-active",
	}.Check(context.Background())
	if result.Status != StatusCritical {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestPublicIdentityChecker(t *testing.T) {
	result := PublicIdentityChecker{
		Cloud:              fakeCloud{identity: cloud.PublicIdentity{InstanceID: "i-active"}},
		AllocationID:       "eipalloc-123",
		ExpectedInstanceID: "i-active",
	}.Check(context.Background())
	if result.Status != StatusOK {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestSourceDestCheckChecker(t *testing.T) {
	result := SourceDestCheckChecker{
		Inspector:  fakeInstanceInspector{info: cloud.InstanceInfo{InstanceID: "i-active", SourceDestCheckEnabled: false}},
		InstanceID: "i-active",
	}.Check(context.Background())
	if result.Status != StatusOK {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestSourceDestCheckCheckerDetectsEnabled(t *testing.T) {
	result := SourceDestCheckChecker{
		Inspector:  fakeInstanceInspector{info: cloud.InstanceInfo{InstanceID: "i-active", SourceDestCheckEnabled: true}},
		InstanceID: "i-active",
	}.Check(context.Background())
	if result.Status != StatusCritical {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestLeaseChecker(t *testing.T) {
	manager := lease.NewMemoryManager("ha-a", 10*time.Second, func() time.Time { return time.Unix(100, 0) })
	if _, err := manager.Acquire(context.Background(), "i-active"); err != nil {
		t.Fatalf("acquire lease: %v", err)
	}
	result := LeaseChecker{Lease: manager, ExpectedOwner: "i-active"}.Check(context.Background())
	if result.Status != StatusOK {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestPrometheusChecker(t *testing.T) {
	result := PrometheusChecker{
		URL:    "http://127.0.0.1:9108/metrics",
		Client: roundTripperClient{status: 200},
	}.Check(context.Background())
	if result.Status != StatusOK {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestPrometheusCheckerDetectsBadStatus(t *testing.T) {
	result := PrometheusChecker{
		URL:    "http://127.0.0.1:9108/metrics",
		Client: roundTripperClient{status: 404},
	}.Check(context.Background())
	if result.Status != StatusCritical {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestSourceIPProbeChecker(t *testing.T) {
	result := SourceIPProbeChecker{Probe: probe.SourceIPProbe{
		URL:        "https://checkip.example",
		ExpectedIP: "203.0.113.10",
		Client:     roundTripperClient{status: 200, body: "203.0.113.10"},
	}}.Check(context.Background())
	if result.Status != StatusOK {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestSourceIPProbeCheckerDetectsMismatch(t *testing.T) {
	result := SourceIPProbeChecker{Probe: probe.SourceIPProbe{
		URL:        "https://checkip.example",
		ExpectedIP: "203.0.113.10",
		Client:     roundTripperClient{status: 200, body: "198.51.100.10"},
	}}.Check(context.Background())
	if result.Status != StatusCritical {
		t.Fatalf("unexpected result: %#v", result)
	}
}

type staticChecker struct {
	result CheckResult
}

func (s staticChecker) Check(context.Context) CheckResult {
	return s.result
}

type fakeEngine struct {
	ready       bool
	countersErr error
}

func (f *fakeEngine) Name() string { return "fake" }

func (f *fakeEngine) EnsureReady(context.Context, config.DatapathConfig) error { return nil }

func (f *fakeEngine) Reconcile(context.Context, config.DatapathConfig) error { return nil }

func (f *fakeEngine) Status(context.Context) (datapath.Status, error) {
	return datapath.Status{Ready: f.ready, Engine: "fake", Message: "checked"}, nil
}

func (f *fakeEngine) Counters(context.Context) (datapath.Counters, error) {
	if f.countersErr != nil {
		return datapath.Counters{}, f.countersErr
	}
	return datapath.Counters{}, nil
}

func (f *fakeEngine) ConntrackSummary(context.Context) (datapath.ConntrackSummary, error) {
	return datapath.ConntrackSummary{}, nil
}

func (f *fakeEngine) Cleanup(context.Context) error { return nil }

type fakeIAMEvaluator struct {
	missing []string
}

func (f fakeIAMEvaluator) Evaluate(_ context.Context, actions []string) (iamcheck.Result, error) {
	return iamcheck.Result{Allowed: actions, Missing: f.missing}, nil
}

type fakeASGInspector struct {
	info cloud.ASGInfo
}

func (f fakeASGInspector) DescribeASG(context.Context, string) (cloud.ASGInfo, error) {
	return f.info, nil
}

type fakeCloud struct {
	route    cloud.RouteTarget
	identity cloud.PublicIdentity
}

func (f fakeCloud) ReplaceRoute(context.Context, cloud.RouteTarget) error {
	return errors.New("not implemented")
}

func (f fakeCloud) AssociateEIP(context.Context, string, string) (cloud.PublicIdentity, error) {
	return cloud.PublicIdentity{}, errors.New("not implemented")
}

func (f fakeCloud) DescribeRoute(context.Context, string, string) (cloud.RouteTarget, error) {
	return f.route, nil
}

func (f fakeCloud) DescribePublicIdentity(context.Context, string) (cloud.PublicIdentity, error) {
	return f.identity, nil
}

type fakeInstanceInspector struct {
	info cloud.InstanceInfo
}

func (f fakeInstanceInspector) DescribeInstance(context.Context, string) (cloud.InstanceInfo, error) {
	return f.info, nil
}

type roundTripperClient struct {
	status int
	body   string
}

func (c roundTripperClient) Do(*http.Request) (*http.Response, error) {
	body := c.body
	if body == "" {
		body = "ok"
	}
	return &http.Response{
		StatusCode: c.status,
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil
}
