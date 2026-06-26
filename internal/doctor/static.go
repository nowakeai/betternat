package doctor

import (
	"context"
	"fmt"

	"github.com/nowakeai/betternat/internal/config"
)

type StaticConfigChecker struct {
	Config config.Config
}

type StaticOKChecker struct {
	Name    string
	Message string
}

func (c StaticOKChecker) Check(context.Context) CheckResult {
	return CheckResult{Name: c.Name, Status: StatusOK, Message: c.Message}
}

type StaticWarningChecker struct {
	Name    string
	Message string
}

func (c StaticWarningChecker) Check(context.Context) CheckResult {
	return CheckResult{Name: c.Name, Status: StatusWarning, Message: c.Message}
}

type StaticErrorChecker struct {
	Name    string
	Message string
}

func (c StaticErrorChecker) Check(context.Context) CheckResult {
	return CheckResult{Name: c.Name, Status: StatusCritical, Message: c.Message}
}

func (c StaticConfigChecker) Check(context.Context) CheckResult {
	if err := c.Config.Validate(); err != nil {
		return CheckResult{Name: "config", Status: StatusCritical, Message: err.Error()}
	}
	return CheckResult{Name: "config", Status: StatusOK, Message: "valid"}
}

type StaticDatapathConfigChecker struct {
	Config config.Config
}

func (c StaticDatapathConfigChecker) Check(context.Context) CheckResult {
	engine := c.Config.Datapath.Engine
	if engine != "loxilb" && engine != "nftables" {
		return CheckResult{Name: "datapath_config", Status: StatusCritical, Message: fmt.Sprintf("unsupported datapath engine %q", engine)}
	}
	if len(c.Config.Datapath.PrivateCIDRs) == 0 {
		return CheckResult{Name: "datapath_config", Status: StatusCritical, Message: "private CIDRs are required"}
	}
	return CheckResult{Name: "datapath_config", Status: StatusOK, Message: fmt.Sprintf("%s configured", engine)}
}

type StaticHAConfigChecker struct {
	Config config.Config
}

func (c StaticHAConfigChecker) Check(context.Context) CheckResult {
	if !c.Config.HA.Enabled {
		return CheckResult{Name: "ha_config", Status: StatusWarning, Message: "HA is disabled"}
	}
	switch c.Config.Cloud {
	case "aws":
		if c.Config.HA.Lease.Backend != "dynamodb" {
			return CheckResult{Name: "ha_config", Status: StatusCritical, Message: "AWS HA requires dynamodb lease backend"}
		}
		if c.Config.HA.Lease.Table == "" {
			return CheckResult{Name: "ha_config", Status: StatusCritical, Message: "lease table is required"}
		}
	case "gcp":
		if c.Config.HA.Lease.Backend != "firestore" {
			return CheckResult{Name: "ha_config", Status: StatusCritical, Message: "GCP HA requires firestore lease backend"}
		}
		if c.Config.GCP.ProjectID == "" {
			return CheckResult{Name: "ha_config", Status: StatusCritical, Message: "gcp.project_id is required"}
		}
	default:
		return CheckResult{Name: "ha_config", Status: StatusCritical, Message: fmt.Sprintf("unsupported HA cloud %q", c.Config.Cloud)}
	}
	if c.Config.HA.Lease.Key == "" && c.Config.HAGroupID == "" {
		return CheckResult{Name: "ha_config", Status: StatusCritical, Message: "ha.lease.key or ha_group_id is required"}
	}
	if c.Config.HA.RouteFailover.Mode != "replace_route" {
		return CheckResult{Name: "ha_config", Status: StatusCritical, Message: "v0 HA requires replace_route failover"}
	}
	if len(c.Config.HA.RouteFailover.RouteTableIDs) == 0 {
		return CheckResult{Name: "ha_config", Status: StatusCritical, Message: "route table ids are required"}
	}
	if (c.Config.Cloud == "aws" || c.Config.Cloud == "gcp") && c.Config.HA.PublicIdentity.Mode == "shared_eip" && c.Config.HA.PublicIdentity.AllocationID == "" {
		return CheckResult{Name: "ha_config", Status: StatusCritical, Message: "shared_eip allocation id is required"}
	}
	return CheckResult{Name: "ha_config", Status: StatusOK, Message: "HA config is complete"}
}

type StaticRollbackConfigChecker struct {
	Config config.Config
}

func (c StaticRollbackConfigChecker) Check(context.Context) CheckResult {
	if len(c.Config.Rollback.PreviousRouteTargets) == 0 {
		return CheckResult{Name: "rollback_config", Status: StatusWarning, Message: "rollback route targets are not captured yet"}
	}
	return CheckResult{Name: "rollback_config", Status: StatusOK, Message: "rollback route targets configured"}
}

type StaticPrometheusConfigChecker struct {
	Config config.Config
}

func (c StaticPrometheusConfigChecker) Check(context.Context) CheckResult {
	port := c.Config.Observability.Prometheus.ListenPort
	if port < 0 || port > 65535 {
		return CheckResult{Name: "prometheus_config", Status: StatusCritical, Message: "invalid prometheus listen port"}
	}
	return CheckResult{Name: "prometheus_config", Status: StatusOK, Message: "prometheus endpoint configured"}
}

type StaticOutboundProbeConfigChecker struct {
	Config config.Config
}

func (c StaticOutboundProbeConfigChecker) Check(context.Context) CheckResult {
	if !c.Config.Observability.OutboundProbe.Enabled {
		return CheckResult{Name: "outbound_probe_config", Status: StatusWarning, Message: "outbound probe is disabled"}
	}
	if c.Config.Observability.OutboundProbe.URL == "" {
		return CheckResult{Name: "outbound_probe_config", Status: StatusCritical, Message: "outbound probe url is required"}
	}
	return CheckResult{Name: "outbound_probe_config", Status: StatusOK, Message: "outbound probe configured"}
}

func StaticCheckers(cfg config.Config) []Checker {
	return []Checker{
		StaticConfigChecker{Config: cfg},
		StaticDatapathConfigChecker{Config: cfg},
		StaticHAConfigChecker{Config: cfg},
		StaticRollbackConfigChecker{Config: cfg},
		StaticPrometheusConfigChecker{Config: cfg},
		StaticOutboundProbeConfigChecker{Config: cfg},
	}
}
