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
	StableEgressIP       bool
	LeaseTableName       string
	AgentConfigHash      string
	AMIID                string
	AMIChannel           string
	InstanceType         string
	UseSpot              bool
	RouteDestinationCIDR string
	RouteTargetType      string
	Tags                 map[string]string
}

type Plan struct {
	Name                string            `json:"name"`
	Region              string            `json:"region"`
	VPCID               string            `json:"vpc_id"`
	AMIID               string            `json:"ami_id,omitempty"`
	AMIChannel          string            `json:"ami_channel,omitempty"`
	InstanceType        string            `json:"instance_type"`
	UseSpot             bool              `json:"use_spot,omitempty"`
	IAMRoleName         string            `json:"iam_role_name"`
	InstanceProfileName string            `json:"instance_profile_name"`
	SecurityGroupName   string            `json:"security_group_name"`
	LeaseTableName      string            `json:"lease_table_name"`
	EIPAllocationNames  map[string]string `json:"eip_allocation_names"`
	Appliances          []Appliance       `json:"appliances"`
	ManagedRoutes       []ManagedRoute    `json:"managed_routes"`
	RequiredIAMActions  []string          `json:"required_iam_actions"`
	Tags                map[string]string `json:"tags"`
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
	plan := Plan{
		Name:                input.Name,
		Region:              input.Region,
		VPCID:               input.VPCID,
		AMIID:               input.AMIID,
		AMIChannel:          amiChannel,
		InstanceType:        instanceType,
		UseSpot:             input.UseSpot,
		IAMRoleName:         "betternat-" + input.Name + "-agent",
		InstanceProfileName: "betternat-" + input.Name + "-agent",
		SecurityGroupName:   "betternat-" + input.Name + "-appliance",
		LeaseTableName:      leaseTable,
		EIPAllocationNames:  map[string]string{},
		RequiredIAMActions: []string{
			"ec2:AssociateAddress",
			"ec2:DescribeAddresses",
			"ec2:DescribeInstanceAttribute",
			"ec2:DescribeRouteTables",
			"ec2:ReplaceRoute",
			"dynamodb:DeleteItem",
			"dynamodb:GetItem",
			"dynamodb:UpdateItem",
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
		plan.Appliances = append(plan.Appliances,
			Appliance{Name: input.Name + "-" + az + "-active", AvailabilityZone: az, SubnetID: subnetID, Role: "active", SourceDestCheck: false},
			Appliance{Name: input.Name + "-" + az + "-standby", AvailabilityZone: az, SubnetID: subnetID, Role: "standby", SourceDestCheck: false},
		)
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
