package cloud

import "context"

type RouteTarget struct {
	RouteTableID    string
	DestinationCIDR string
	Target          string
}

type PublicIdentity struct {
	AllocationID string
	PublicIP     string
	InstanceID   string
	PrivateIP    string
}

type InstanceInfo struct {
	InstanceID             string
	SourceDestCheckEnabled bool
	PrivateIP              string
	PublicIP               string
}

type ASGInstance struct {
	InstanceID       string
	LifecycleState   string
	HealthStatus     string
	AvailabilityZone string
}

type ASGInfo struct {
	Name            string
	MinSize         int32
	DesiredCapacity int32
	MaxSize         int32
	Instances       []ASGInstance
}

type LifecycleAction struct {
	AutoScalingGroupName string
	LifecycleHookName    string
	InstanceID           string
	Result               string
	Reason               string
}

type InstancePreparer interface {
	DisableSourceDestCheck(ctx context.Context, instanceID string) error
}

// Provider wraps cloud control-plane operations used by betternat-agent.
type Provider interface {
	ReplaceRoute(ctx context.Context, target RouteTarget) error
	AssociateEIP(ctx context.Context, allocationID string, instanceID string) (PublicIdentity, error)
	DescribeRoute(ctx context.Context, routeTableID string, destinationCIDR string) (RouteTarget, error)
	DescribePublicIdentity(ctx context.Context, allocationID string) (PublicIdentity, error)
}
