package awscloud

import (
	"context"
	"fmt"
	"strings"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/nowakeai/betternat/internal/cloud"
)

type EC2API interface {
	ReplaceRoute(ctx context.Context, params *ec2.ReplaceRouteInput, optFns ...func(*ec2.Options)) (*ec2.ReplaceRouteOutput, error)
	AssociateAddress(ctx context.Context, params *ec2.AssociateAddressInput, optFns ...func(*ec2.Options)) (*ec2.AssociateAddressOutput, error)
	DescribeRouteTables(ctx context.Context, params *ec2.DescribeRouteTablesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeRouteTablesOutput, error)
	DescribeAddresses(ctx context.Context, params *ec2.DescribeAddressesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeAddressesOutput, error)
	DescribeInstanceAttribute(ctx context.Context, params *ec2.DescribeInstanceAttributeInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstanceAttributeOutput, error)
	ModifyInstanceAttribute(ctx context.Context, params *ec2.ModifyInstanceAttributeInput, optFns ...func(*ec2.Options)) (*ec2.ModifyInstanceAttributeOutput, error)
}

type Provider struct {
	ec2 EC2API
}

func New(ctx context.Context, region string) (*Provider, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	return NewFromClient(ec2.NewFromConfig(cfg)), nil
}

func ResolveLocalInstanceID(ctx context.Context, region string) (string, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return "", fmt.Errorf("load aws config: %w", err)
	}
	output, err := imds.NewFromConfig(cfg).GetInstanceIdentityDocument(ctx, &imds.GetInstanceIdentityDocumentInput{})
	if err != nil {
		return "", fmt.Errorf("aws imds instance identity document: %w", err)
	}
	if output.InstanceID == "" {
		return "", fmt.Errorf("aws imds instance identity document returned empty instance id")
	}
	return output.InstanceID, nil
}

func ResolveSharedEIPAllocationID(ctx context.Context, region string, gatewayID string, availabilityZone string) (string, error) {
	if gatewayID == "" {
		return "", fmt.Errorf("gateway id is required")
	}
	if availabilityZone == "" {
		return "", fmt.Errorf("availability zone is required")
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return "", fmt.Errorf("load aws config: %w", err)
	}
	provider := NewFromClient(ec2.NewFromConfig(cfg))
	return provider.ResolveSharedEIPAllocationID(ctx, gatewayID, availabilityZone)
}

func NewFromClient(client EC2API) *Provider {
	return &Provider{ec2: client}
}

func (p *Provider) ResolveSharedEIPAllocationID(ctx context.Context, gatewayID string, availabilityZone string) (string, error) {
	if p.ec2 == nil {
		return "", fmt.Errorf("ec2 client is required")
	}
	if gatewayID == "" {
		return "", fmt.Errorf("gateway id is required")
	}
	if availabilityZone == "" {
		return "", fmt.Errorf("availability zone is required")
	}
	name := "betternat-" + gatewayID + "-" + availabilityZone
	output, err := p.ec2.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{
		Filters: []types.Filter{
			{Name: awssdk.String("tag:BetterNATGateway"), Values: []string{gatewayID}},
			{Name: awssdk.String("tag:ManagedBy"), Values: []string{"betternat"}},
			{Name: awssdk.String("tag:Name"), Values: []string{name}},
		},
	})
	if err != nil {
		return "", fmt.Errorf("aws ec2 DescribeAddresses shared EIP: %w", err)
	}
	if len(output.Addresses) == 0 {
		return "", fmt.Errorf("shared EIP %s not found", name)
	}
	if len(output.Addresses) > 1 {
		return "", fmt.Errorf("shared EIP %s is ambiguous: %d matches", name, len(output.Addresses))
	}
	allocationID := awssdk.ToString(output.Addresses[0].AllocationId)
	if allocationID == "" {
		return "", fmt.Errorf("shared EIP %s returned empty allocation id", name)
	}
	return allocationID, nil
}

func (p *Provider) ReplaceRoute(ctx context.Context, target cloud.RouteTarget) error {
	if p.ec2 == nil {
		return fmt.Errorf("ec2 client is required")
	}
	if target.RouteTableID == "" {
		return fmt.Errorf("route table id is required")
	}
	if target.DestinationCIDR == "" {
		return fmt.Errorf("destination cidr is required")
	}
	if target.Target == "" {
		return fmt.Errorf("route target is required")
	}
	input := &ec2.ReplaceRouteInput{
		RouteTableId:         awssdk.String(target.RouteTableID),
		DestinationCidrBlock: awssdk.String(target.DestinationCIDR),
	}
	if strings.HasPrefix(target.Target, "eni-") {
		input.NetworkInterfaceId = awssdk.String(target.Target)
	} else {
		input.InstanceId = awssdk.String(target.Target)
	}
	if _, err := p.ec2.ReplaceRoute(ctx, input); err != nil {
		return fmt.Errorf("aws ec2 ReplaceRoute: %w", err)
	}
	return nil
}

func (p *Provider) AssociateEIP(ctx context.Context, allocationID string, instanceID string) (cloud.PublicIdentity, error) {
	if p.ec2 == nil {
		return cloud.PublicIdentity{}, fmt.Errorf("ec2 client is required")
	}
	if allocationID == "" {
		return cloud.PublicIdentity{}, fmt.Errorf("allocation id is required")
	}
	if instanceID == "" {
		return cloud.PublicIdentity{}, fmt.Errorf("instance id is required")
	}
	if _, err := p.ec2.AssociateAddress(ctx, &ec2.AssociateAddressInput{
		AllocationId:       awssdk.String(allocationID),
		InstanceId:         awssdk.String(instanceID),
		AllowReassociation: awssdk.Bool(true),
	}); err != nil {
		return cloud.PublicIdentity{}, fmt.Errorf("aws ec2 AssociateAddress: %w", err)
	}
	identity, err := p.DescribePublicIdentity(ctx, allocationID)
	if err != nil {
		return cloud.PublicIdentity{}, err
	}
	return identity, nil
}

func (p *Provider) DescribeRoute(ctx context.Context, routeTableID string, destinationCIDR string) (cloud.RouteTarget, error) {
	if p.ec2 == nil {
		return cloud.RouteTarget{}, fmt.Errorf("ec2 client is required")
	}
	output, err := p.ec2.DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{
		RouteTableIds: []string{routeTableID},
	})
	if err != nil {
		return cloud.RouteTarget{}, fmt.Errorf("aws ec2 DescribeRouteTables: %w", err)
	}
	for _, table := range output.RouteTables {
		for _, route := range table.Routes {
			if awssdk.ToString(route.DestinationCidrBlock) != destinationCIDR {
				continue
			}
			return cloud.RouteTarget{
				RouteTableID:    routeTableID,
				DestinationCIDR: destinationCIDR,
				Target:          routeTarget(route),
			}, nil
		}
	}
	return cloud.RouteTarget{}, fmt.Errorf("route %s not found in %s", destinationCIDR, routeTableID)
}

func (p *Provider) DescribePublicIdentity(ctx context.Context, allocationID string) (cloud.PublicIdentity, error) {
	if p.ec2 == nil {
		return cloud.PublicIdentity{}, fmt.Errorf("ec2 client is required")
	}
	output, err := p.ec2.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{
		AllocationIds: []string{allocationID},
	})
	if err != nil {
		return cloud.PublicIdentity{}, fmt.Errorf("aws ec2 DescribeAddresses: %w", err)
	}
	if len(output.Addresses) == 0 {
		return cloud.PublicIdentity{}, fmt.Errorf("address %s not found", allocationID)
	}
	address := output.Addresses[0]
	return cloud.PublicIdentity{
		AllocationID: allocationID,
		PublicIP:     awssdk.ToString(address.PublicIp),
		InstanceID:   awssdk.ToString(address.InstanceId),
		PrivateIP:    awssdk.ToString(address.PrivateIpAddress),
	}, nil
}

func (p *Provider) DescribeInstance(ctx context.Context, instanceID string) (cloud.InstanceInfo, error) {
	if p.ec2 == nil {
		return cloud.InstanceInfo{}, fmt.Errorf("ec2 client is required")
	}
	if instanceID == "" {
		return cloud.InstanceInfo{}, fmt.Errorf("instance id is required")
	}
	output, err := p.ec2.DescribeInstanceAttribute(ctx, &ec2.DescribeInstanceAttributeInput{
		InstanceId: awssdk.String(instanceID),
		Attribute:  types.InstanceAttributeNameSourceDestCheck,
	})
	if err != nil {
		return cloud.InstanceInfo{}, fmt.Errorf("aws ec2 DescribeInstanceAttribute sourceDestCheck: %w", err)
	}
	sourceDestCheck := false
	if output.SourceDestCheck != nil {
		sourceDestCheck = awssdk.ToBool(output.SourceDestCheck.Value)
	}
	return cloud.InstanceInfo{
		InstanceID:             instanceID,
		SourceDestCheckEnabled: sourceDestCheck,
	}, nil
}

func (p *Provider) DisableSourceDestCheck(ctx context.Context, instanceID string) error {
	if p.ec2 == nil {
		return fmt.Errorf("ec2 client is required")
	}
	if instanceID == "" {
		return fmt.Errorf("instance id is required")
	}
	if _, err := p.ec2.ModifyInstanceAttribute(ctx, &ec2.ModifyInstanceAttributeInput{
		InstanceId:      awssdk.String(instanceID),
		SourceDestCheck: &types.AttributeBooleanValue{Value: awssdk.Bool(false)},
	}); err != nil {
		return fmt.Errorf("aws ec2 ModifyInstanceAttribute sourceDestCheck: %w", err)
	}
	return nil
}

func routeTarget(route types.Route) string {
	switch {
	case route.InstanceId != nil:
		return awssdk.ToString(route.InstanceId)
	case route.NetworkInterfaceId != nil:
		return awssdk.ToString(route.NetworkInterfaceId)
	case route.NatGatewayId != nil:
		return awssdk.ToString(route.NatGatewayId)
	case route.GatewayId != nil:
		return awssdk.ToString(route.GatewayId)
	case route.TransitGatewayId != nil:
		return awssdk.ToString(route.TransitGatewayId)
	case route.VpcPeeringConnectionId != nil:
		return awssdk.ToString(route.VpcPeeringConnectionId)
	case route.EgressOnlyInternetGatewayId != nil:
		return awssdk.ToString(route.EgressOnlyInternetGatewayId)
	default:
		return ""
	}
}
