package doctor

import (
	"context"
	"fmt"
	"net/http"

	"github.com/nowakeai/betternat/internal/cloud"
	"github.com/nowakeai/betternat/internal/datapath"
	"github.com/nowakeai/betternat/internal/iamcheck"
	"github.com/nowakeai/betternat/internal/lease"
	"github.com/nowakeai/betternat/internal/probe"
)

type Status string

const (
	StatusOK       Status = "ok"
	StatusWarning  Status = "warning"
	StatusCritical Status = "critical"
	StatusUnknown  Status = "unknown"
)

type CheckResult struct {
	Name    string `json:"name"`
	Status  Status `json:"status"`
	Message string `json:"message"`
}

type Checker interface {
	Check(ctx context.Context) CheckResult
}

type Report struct {
	Status Status        `json:"status"`
	Checks []CheckResult `json:"checks"`
}

func Run(ctx context.Context, checkers []Checker) Report {
	report := Report{Status: StatusOK}
	for _, checker := range checkers {
		result := checker.Check(ctx)
		report.Checks = append(report.Checks, result)
		report.Status = worstStatus(report.Status, result.Status)
	}
	return report
}

type DatapathChecker struct {
	Engine datapath.Engine
}

func (c DatapathChecker) Check(ctx context.Context) CheckResult {
	if c.Engine == nil {
		return CheckResult{Name: "datapath", Status: StatusCritical, Message: "datapath engine is not configured"}
	}
	status, err := c.Engine.Status(ctx)
	if err != nil {
		return CheckResult{Name: "datapath", Status: StatusCritical, Message: fmt.Sprintf("status failed: %v", err)}
	}
	if _, err := c.Engine.Counters(ctx); err != nil {
		return CheckResult{Name: "datapath", Status: StatusCritical, Message: fmt.Sprintf("counters failed: %v", err)}
	}
	if _, err := c.Engine.ConntrackSummary(ctx); err != nil {
		return CheckResult{Name: "datapath", Status: StatusCritical, Message: fmt.Sprintf("conntrack failed: %v", err)}
	}
	if !status.Ready {
		return CheckResult{Name: "datapath", Status: StatusWarning, Message: status.Message}
	}
	return CheckResult{Name: "datapath", Status: StatusOK, Message: status.Message}
}

type IAMChecker struct {
	Evaluator iamcheck.Evaluator
}

func (c IAMChecker) Check(ctx context.Context) CheckResult {
	result, err := iamcheck.Check(ctx, c.Evaluator)
	if err != nil {
		return CheckResult{Name: "iam", Status: StatusCritical, Message: err.Error()}
	}
	if len(result.Missing) > 0 {
		return CheckResult{Name: "iam", Status: StatusCritical, Message: fmt.Sprintf("missing actions: %v", result.Missing)}
	}
	return CheckResult{Name: "iam", Status: StatusOK, Message: "required actions allowed"}
}

type RouteChecker struct {
	Cloud           cloud.Provider
	RouteTableID    string
	DestinationCIDR string
	ExpectedTarget  string
}

func (c RouteChecker) Check(ctx context.Context) CheckResult {
	if c.Cloud == nil {
		return CheckResult{Name: "route", Status: StatusCritical, Message: "cloud provider is not configured"}
	}
	route, err := c.Cloud.DescribeRoute(ctx, c.RouteTableID, c.DestinationCIDR)
	if err != nil {
		return CheckResult{Name: "route", Status: StatusCritical, Message: fmt.Sprintf("describe route failed: %v", err)}
	}
	if c.ExpectedTarget != "" && route.Target != c.ExpectedTarget {
		return CheckResult{Name: "route", Status: StatusCritical, Message: fmt.Sprintf("route target is %q, expected %q", route.Target, c.ExpectedTarget)}
	}
	return CheckResult{Name: "route", Status: StatusOK, Message: "route target matches"}
}

type PublicIdentityChecker struct {
	Cloud              cloud.Provider
	AllocationID       string
	ExpectedInstanceID string
}

func (c PublicIdentityChecker) Check(ctx context.Context) CheckResult {
	if c.Cloud == nil {
		return CheckResult{Name: "public_identity", Status: StatusCritical, Message: "cloud provider is not configured"}
	}
	identity, err := c.Cloud.DescribePublicIdentity(ctx, c.AllocationID)
	if err != nil {
		return CheckResult{Name: "public_identity", Status: StatusCritical, Message: fmt.Sprintf("describe public identity failed: %v", err)}
	}
	if c.ExpectedInstanceID != "" && identity.InstanceID != c.ExpectedInstanceID {
		return CheckResult{Name: "public_identity", Status: StatusCritical, Message: fmt.Sprintf("public identity is on %q, expected %q", identity.InstanceID, c.ExpectedInstanceID)}
	}
	return CheckResult{Name: "public_identity", Status: StatusOK, Message: "public identity matches"}
}

type InstanceInspector interface {
	DescribeInstance(ctx context.Context, instanceID string) (cloud.InstanceInfo, error)
}

type SourceDestCheckInspector interface {
	SourceDestCheckEnabled(ctx context.Context, instanceID string) (bool, error)
}

type SourceDestCheckChecker struct {
	Inspector  SourceDestCheckInspector
	InstanceID string
}

func (c SourceDestCheckChecker) Check(ctx context.Context) CheckResult {
	if c.Inspector == nil {
		return CheckResult{Name: "source_dest_check", Status: StatusCritical, Message: "instance inspector is not configured"}
	}
	enabled, err := c.Inspector.SourceDestCheckEnabled(ctx, c.InstanceID)
	if err != nil {
		return CheckResult{Name: "source_dest_check", Status: StatusCritical, Message: fmt.Sprintf("describe source/destination check failed: %v", err)}
	}
	if enabled {
		return CheckResult{Name: "source_dest_check", Status: StatusCritical, Message: "source/destination check is enabled"}
	}
	return CheckResult{Name: "source_dest_check", Status: StatusOK, Message: "source/destination check is disabled"}
}

type ASGInspector interface {
	DescribeASG(ctx context.Context, name string) (cloud.ASGInfo, error)
}

type ASGChecker struct {
	Inspector ASGInspector
	Name      string
	HAEnabled bool
}

func (c ASGChecker) Check(ctx context.Context) CheckResult {
	if c.Inspector == nil {
		return CheckResult{Name: "asg", Status: StatusCritical, Message: "asg inspector is not configured"}
	}
	info, err := c.Inspector.DescribeASG(ctx, c.Name)
	if err != nil {
		return CheckResult{Name: "asg", Status: StatusCritical, Message: fmt.Sprintf("describe asg failed: %v", err)}
	}
	healthy := 0
	for _, instance := range info.Instances {
		if instance.LifecycleState == "InService" && instance.HealthStatus == "Healthy" {
			healthy++
		}
	}
	message := fmt.Sprintf("healthy %d/%d instances in %s", healthy, info.DesiredCapacity, info.Name)
	if int32(healthy) < info.DesiredCapacity {
		return CheckResult{Name: "asg", Status: StatusCritical, Message: message}
	}
	if c.HAEnabled && info.DesiredCapacity < 2 {
		return CheckResult{Name: "asg", Status: StatusWarning, Message: message + "; HA has no standby capacity"}
	}
	return CheckResult{Name: "asg", Status: StatusOK, Message: message}
}

type LeaseChecker struct {
	Lease         lease.Manager
	ExpectedOwner string
}

func (c LeaseChecker) Check(ctx context.Context) CheckResult {
	if c.Lease == nil {
		return CheckResult{Name: "lease", Status: StatusCritical, Message: "lease manager is not configured"}
	}
	record, err := c.Lease.Current(ctx)
	if err != nil {
		return CheckResult{Name: "lease", Status: StatusCritical, Message: fmt.Sprintf("read lease failed: %v", err)}
	}
	if c.ExpectedOwner != "" && record.OwnerInstanceID != c.ExpectedOwner {
		return CheckResult{Name: "lease", Status: StatusCritical, Message: fmt.Sprintf("lease owner is %q, expected %q", record.OwnerInstanceID, c.ExpectedOwner)}
	}
	return CheckResult{Name: "lease", Status: StatusOK, Message: fmt.Sprintf("lease generation %d", record.Generation)}
}

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type PrometheusChecker struct {
	URL    string
	Client HTTPClient
}

func (c PrometheusChecker) Check(ctx context.Context) CheckResult {
	if c.URL == "" {
		return CheckResult{Name: "prometheus", Status: StatusWarning, Message: "prometheus url is not configured"}
	}
	client := c.Client
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return CheckResult{Name: "prometheus", Status: StatusCritical, Message: fmt.Sprintf("build request failed: %v", err)}
	}
	resp, err := client.Do(req)
	if err != nil {
		return CheckResult{Name: "prometheus", Status: StatusCritical, Message: fmt.Sprintf("request failed: %v", err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return CheckResult{Name: "prometheus", Status: StatusCritical, Message: fmt.Sprintf("unexpected status %d", resp.StatusCode)}
	}
	return CheckResult{Name: "prometheus", Status: StatusOK, Message: "reachable"}
}

type SourceIPProbeChecker struct {
	Probe probe.SourceIPProbe
}

func (c SourceIPProbeChecker) Check(ctx context.Context) CheckResult {
	if c.Probe.URL == "" {
		return CheckResult{Name: "source_ip_probe", Status: StatusWarning, Message: "source IP probe url is not configured"}
	}
	result, err := c.Probe.Run(ctx)
	if err != nil {
		return CheckResult{Name: "source_ip_probe", Status: StatusCritical, Message: err.Error()}
	}
	if !result.Matched {
		return CheckResult{Name: "source_ip_probe", Status: StatusCritical, Message: fmt.Sprintf("observed source IP %s, expected %s", result.ObservedIP, result.ExpectedIP)}
	}
	return CheckResult{Name: "source_ip_probe", Status: StatusOK, Message: fmt.Sprintf("observed source IP %s", result.ObservedIP)}
}

func worstStatus(a Status, b Status) Status {
	if rankStatus(b) > rankStatus(a) {
		return b
	}
	return a
}

func rankStatus(status Status) int {
	switch status {
	case StatusOK:
		return 0
	case StatusUnknown:
		return 1
	case StatusWarning:
		return 2
	case StatusCritical:
		return 3
	default:
		return 1
	}
}
