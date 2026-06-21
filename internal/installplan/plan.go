package installplan

import (
	"fmt"
	"sort"
)

type Input struct {
	Name                 string
	Region               string
	VPCID                string
	PublicSubnetIDs      map[string]string
	PrivateRouteTableIDs map[string][]string
	PrivateCIDRs         []string
	StableEgressIP       bool
	LeaseTableName       string
	AgentConfigHash      string
	AMIID                string
	AMIChannel           string
	InstanceType         string
	UseSpot              bool
	MinSize              int32
	DesiredCapacity      int32
	MaxSize              int32
	RouteDestinationCIDR string
	RouteTargetType      string
	Tags                 map[string]string
}

type Plan struct {
	Name                string            `json:"name"`
	Region              string            `json:"region"`
	VPCID               string            `json:"vpc_id"`
	PrivateCIDRs        []string          `json:"private_cidrs"`
	AMIID               string            `json:"ami_id,omitempty"`
	AMIChannel          string            `json:"ami_channel,omitempty"`
	InstanceType        string            `json:"instance_type"`
	UseSpot             bool              `json:"use_spot,omitempty"`
	MinSize             int32             `json:"min_size"`
	DesiredCapacity     int32             `json:"desired_capacity"`
	MaxSize             int32             `json:"max_size"`
	IAMRoleName         string            `json:"iam_role_name"`
	InstanceProfileName string            `json:"instance_profile_name"`
	SecurityGroupName   string            `json:"security_group_name"`
	LeaseTableName      string            `json:"lease_table_name"`
	EIPAllocationNames  map[string]string `json:"eip_allocation_names"`
	Pools               []Pool            `json:"pools"`
	Appliances          []Appliance       `json:"appliances"`
	ManagedRoutes       []ManagedRoute    `json:"managed_routes"`
	RequiredIAMActions  []string          `json:"required_iam_actions"`
	Tags                map[string]string `json:"tags"`
}

type Pool struct {
	Name               string `json:"name"`
	AvailabilityZone   string `json:"availability_zone"`
	SubnetID           string `json:"subnet_id"`
	MinSize            int32  `json:"min_size"`
	DesiredCapacity    int32  `json:"desired_capacity"`
	MaxSize            int32  `json:"max_size"`
	LaunchTemplateName string `json:"launch_template_name"`
	ASGName            string `json:"asg_name"`
}

type Appliance struct {
	Name             string `json:"name"`
	AvailabilityZone string `json:"availability_zone"`
	SubnetID         string `json:"subnet_id"`
	Role             string `json:"role"`
	SourceDestCheck  bool   `json:"source_dest_check"`
}

type ManagedRoute struct {
	RouteTableID      string `json:"route_table_id"`
	AvailabilityZone  string `json:"availability_zone"`
	DestinationCIDR   string `json:"destination_cidr"`
	InitialTargetRole string `json:"initial_target_role"`
}

func Build(input Input) (Plan, error) {
	if input.Name == "" {
		return Plan{}, fmt.Errorf("name is required")
	}
	if input.Region == "" {
		return Plan{}, fmt.Errorf("region is required")
	}
	if input.VPCID == "" {
		return Plan{}, fmt.Errorf("vpc id is required")
	}
	if len(input.PublicSubnetIDs) == 0 {
		return Plan{}, fmt.Errorf("public subnets are required")
	}
	if len(input.PrivateRouteTableIDs) == 0 {
		return Plan{}, fmt.Errorf("private route tables are required")
	}
	leaseTable := input.LeaseTableName
	if leaseTable == "" {
		leaseTable = "betternat-" + input.Name + "-leases"
	}
	instanceType := input.InstanceType
	if instanceType == "" {
		instanceType = "t3.small"
	}
	amiChannel := input.AMIChannel
	if amiChannel == "" {
		amiChannel = "stable"
	}
	routeDestinationCIDR := input.RouteDestinationCIDR
	if routeDestinationCIDR == "" {
		routeDestinationCIDR = "0.0.0.0/0"
	}
	routeTargetType := input.RouteTargetType
	if routeTargetType == "" {
		routeTargetType = "instance"
	}
	if routeTargetType != "instance" {
		return Plan{}, fmt.Errorf("unsupported route target type %q", routeTargetType)
	}
	minSize := input.MinSize
	if minSize == 0 {
		minSize = 1
	}
	desiredCapacity := input.DesiredCapacity
	if desiredCapacity == 0 {
		desiredCapacity = 2
	}
	maxSize := input.MaxSize
	if maxSize == 0 {
		maxSize = 3
	}
	if minSize < 0 {
		return Plan{}, fmt.Errorf("min_size cannot be negative")
	}
	if desiredCapacity < minSize {
		return Plan{}, fmt.Errorf("desired_capacity cannot be less than min_size")
	}
	if maxSize < desiredCapacity {
		return Plan{}, fmt.Errorf("max_size cannot be less than desired_capacity")
	}
	plan := Plan{
		Name:                input.Name,
		Region:              input.Region,
		VPCID:               input.VPCID,
		PrivateCIDRs:        append([]string{}, input.PrivateCIDRs...),
		AMIID:               input.AMIID,
		AMIChannel:          amiChannel,
		InstanceType:        instanceType,
		UseSpot:             input.UseSpot,
		MinSize:             minSize,
		DesiredCapacity:     desiredCapacity,
		MaxSize:             maxSize,
		IAMRoleName:         "betternat-" + input.Name + "-agent",
		InstanceProfileName: "betternat-" + input.Name + "-agent",
		SecurityGroupName:   "betternat-" + input.Name + "-appliance",
		LeaseTableName:      leaseTable,
		EIPAllocationNames:  map[string]string{},
		RequiredIAMActions: []string{
			"autoscaling:DescribeAutoScalingGroups",
			"ec2:AssociateAddress",
			"ec2:DescribeAddresses",
			"ec2:DescribeInstances",
			"ec2:DescribeInstanceAttribute",
			"ec2:DescribeRouteTables",
			"ec2:ModifyInstanceAttribute",
			"ec2:ReplaceRoute",
			"dynamodb:DeleteItem",
			"dynamodb:GetItem",
			"dynamodb:UpdateItem",
			"iam:SimulatePrincipalPolicy",
			"sts:GetCallerIdentity",
		},
		Tags: map[string]string{
			"BetterNATGateway": input.Name,
			"ManagedBy":        "betternat",
		},
	}
	for key, value := range input.Tags {
		if key == "" {
			return Plan{}, fmt.Errorf("tag key cannot be empty")
		}
		plan.Tags[key] = value
	}
	plan.Tags["BetterNATGateway"] = input.Name
	plan.Tags["ManagedBy"] = "betternat"
	if input.AgentConfigHash != "" {
		plan.Tags["BetterNATAgentConfigHash"] = input.AgentConfigHash
	}

	for _, az := range sortedKeys(input.PublicSubnetIDs) {
		subnetID := input.PublicSubnetIDs[az]
		if subnetID == "" {
			return Plan{}, fmt.Errorf("public subnet for %s is empty", az)
		}
		if _, ok := input.PrivateRouteTableIDs[az]; !ok {
			return Plan{}, fmt.Errorf("missing private route tables for %s", az)
		}
		poolName := input.Name + "-" + az
		plan.Pools = append(plan.Pools, Pool{
			Name:               poolName,
			AvailabilityZone:   az,
			SubnetID:           subnetID,
			MinSize:            minSize,
			DesiredCapacity:    desiredCapacity,
			MaxSize:            maxSize,
			LaunchTemplateName: "betternat-" + poolName,
			ASGName:            "betternat-" + poolName,
		})
		if input.StableEgressIP {
			plan.EIPAllocationNames[az] = "betternat-" + input.Name + "-" + az
		}
		for _, routeTableID := range input.PrivateRouteTableIDs[az] {
			if routeTableID == "" {
				return Plan{}, fmt.Errorf("private route table for %s is empty", az)
			}
			plan.ManagedRoutes = append(plan.ManagedRoutes, ManagedRoute{
				RouteTableID:      routeTableID,
				AvailabilityZone:  az,
				DestinationCIDR:   routeDestinationCIDR,
				InitialTargetRole: "active",
			})
		}
	}
	return plan, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
