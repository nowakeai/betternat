package awscloud

import (
	"context"
	"testing"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/nowakeai/betternat/internal/cloud"
)

func TestReplaceRouteUsesInstanceTarget(t *testing.T) {
	client := &fakeEC2{}
	provider := NewFromClient(client)

	err := provider.ReplaceRoute(context.Background(), cloud.RouteTarget{
		RouteTableID:    "rtb-a",
		DestinationCIDR: "0.0.0.0/0",
		Target:          "i-123",
	})
	if err != nil {
		t.Fatalf("replace route: %v", err)
	}
	if awssdk.ToString(client.replaceRouteInput.InstanceId) != "i-123" {
		t.Fatalf("missing instance target: %#v", client.replaceRouteInput)
	}
	if client.replaceRouteInput.NetworkInterfaceId != nil {
		t.Fatalf("unexpected eni target: %#v", client.replaceRouteInput)
	}
}

func TestReplaceRouteUsesENITarget(t *testing.T) {
	client := &fakeEC2{}
	provider := NewFromClient(client)

	err := provider.ReplaceRoute(context.Background(), cloud.RouteTarget{
		RouteTableID:    "rtb-a",
		DestinationCIDR: "0.0.0.0/0",
		Target:          "eni-123",
	})
	if err != nil {
		t.Fatalf("replace route: %v", err)
	}
	if awssdk.ToString(client.replaceRouteInput.NetworkInterfaceId) != "eni-123" {
		t.Fatalf("missing eni target: %#v", client.replaceRouteInput)
	}
}

func TestAssociateEIPAllowsReassociationAndReturnsIdentity(t *testing.T) {
	client := &fakeEC2{
		addresses: []types.Address{{
			AllocationId:     awssdk.String("eipalloc-123"),
			PublicIp:         awssdk.String("203.0.113.10"),
			InstanceId:       awssdk.String("i-123"),
			PrivateIpAddress: awssdk.String("10.0.1.10"),
		}},
	}
	provider := NewFromClient(client)

	identity, err := provider.AssociateEIP(context.Background(), "eipalloc-123", "i-123")
	if err != nil {
		t.Fatalf("associate eip: %v", err)
	}
	if client.associateInput.AllowReassociation == nil || !*client.associateInput.AllowReassociation {
		t.Fatalf("expected allow reassociation: %#v", client.associateInput)
	}
	if identity.PublicIP != "203.0.113.10" || identity.InstanceID != "i-123" {
		t.Fatalf("unexpected identity: %#v", identity)
	}
}

func TestDescribeRouteReturnsTarget(t *testing.T) {
	client := &fakeEC2{
		routeTables: []types.RouteTable{{
			Routes: []types.Route{{
				DestinationCidrBlock: awssdk.String("0.0.0.0/0"),
				InstanceId:           awssdk.String("i-123"),
			}},
		}},
	}
	provider := NewFromClient(client)

	route, err := provider.DescribeRoute(context.Background(), "rtb-a", "0.0.0.0/0")
	if err != nil {
		t.Fatalf("describe route: %v", err)
	}
	if route.Target != "i-123" {
		t.Fatalf("unexpected route: %#v", route)
	}
}

func TestDescribeInstanceReadsSourceDestCheck(t *testing.T) {
	client := &fakeEC2{
		sourceDestCheck: true,
		instances: []types.Instance{{
			InstanceId:       awssdk.String("i-123"),
			PrivateIpAddress: awssdk.String("10.0.1.10"),
			PublicIpAddress:  awssdk.String("198.51.100.10"),
		}},
	}
	provider := NewFromClient(client)

	info, err := provider.DescribeInstance(context.Background(), "i-123")
	if err != nil {
		t.Fatalf("describe instance: %v", err)
	}
	if !info.SourceDestCheckEnabled {
		t.Fatalf("expected source/dest check enabled: %#v", info)
	}
	if info.PrivateIP != "10.0.1.10" || info.PublicIP != "198.51.100.10" {
		t.Fatalf("unexpected instance addresses: %#v", info)
	}
	if awssdk.ToString(client.describeInstanceAttributeInput.InstanceId) != "i-123" {
		t.Fatalf("unexpected describe input: %#v", client.describeInstanceAttributeInput)
	}
	if client.describeInstanceAttributeInput.Attribute != types.InstanceAttributeNameSourceDestCheck {
		t.Fatalf("unexpected attribute: %#v", client.describeInstanceAttributeInput)
	}
}

func TestDisableSourceDestCheck(t *testing.T) {
	client := &fakeEC2{}
	provider := NewFromClient(client)

	if err := provider.DisableSourceDestCheck(context.Background(), "i-123"); err != nil {
		t.Fatalf("disable source/dest check: %v", err)
	}
	if awssdk.ToString(client.modifyInstanceAttributeInput.InstanceId) != "i-123" {
		t.Fatalf("unexpected modify input: %#v", client.modifyInstanceAttributeInput)
	}
	if client.modifyInstanceAttributeInput.SourceDestCheck == nil || awssdk.ToBool(client.modifyInstanceAttributeInput.SourceDestCheck.Value) {
		t.Fatalf("source/dest check should be disabled: %#v", client.modifyInstanceAttributeInput)
	}
}

func TestResolveSharedEIPAllocationIDUsesBetterNATTags(t *testing.T) {
	client := &fakeEC2{
		addresses: []types.Address{{
			AllocationId: awssdk.String("eipalloc-123"),
		}},
	}
	provider := NewFromClient(client)

	allocationID, err := provider.ResolveSharedEIPAllocationID(context.Background(), "prod-egress", "us-west-2a")
	if err != nil {
		t.Fatalf("resolve shared eip: %v", err)
	}
	if allocationID != "eipalloc-123" {
		t.Fatalf("unexpected allocation id: %s", allocationID)
	}
	wantFilters := map[string]string{
		"tag:BetterNATGateway": "prod-egress",
		"tag:ManagedBy":        "betternat",
		"tag:Name":             "betternat-prod-egress-us-west-2a",
	}
	for _, filter := range client.describeAddressesInput.Filters {
		delete(wantFilters, awssdk.ToString(filter.Name))
	}
	if len(wantFilters) != 0 {
		t.Fatalf("missing filters: %#v input=%#v", wantFilters, client.describeAddressesInput)
	}
}

type fakeEC2 struct {
	replaceRouteInput              *ec2.ReplaceRouteInput
	associateInput                 *ec2.AssociateAddressInput
	describeAddressesInput         *ec2.DescribeAddressesInput
	describeInstanceAttributeInput *ec2.DescribeInstanceAttributeInput
	modifyInstanceAttributeInput   *ec2.ModifyInstanceAttributeInput
	routeTables                    []types.RouteTable
	addresses                      []types.Address
	instances                      []types.Instance
	sourceDestCheck                bool
}

func (f *fakeEC2) ReplaceRoute(_ context.Context, params *ec2.ReplaceRouteInput, _ ...func(*ec2.Options)) (*ec2.ReplaceRouteOutput, error) {
	f.replaceRouteInput = params
	return &ec2.ReplaceRouteOutput{}, nil
}

func (f *fakeEC2) AssociateAddress(_ context.Context, params *ec2.AssociateAddressInput, _ ...func(*ec2.Options)) (*ec2.AssociateAddressOutput, error) {
	f.associateInput = params
	return &ec2.AssociateAddressOutput{}, nil
}

func (f *fakeEC2) DescribeRouteTables(_ context.Context, _ *ec2.DescribeRouteTablesInput, _ ...func(*ec2.Options)) (*ec2.DescribeRouteTablesOutput, error) {
	return &ec2.DescribeRouteTablesOutput{RouteTables: f.routeTables}, nil
}

func (f *fakeEC2) DescribeAddresses(_ context.Context, params *ec2.DescribeAddressesInput, _ ...func(*ec2.Options)) (*ec2.DescribeAddressesOutput, error) {
	f.describeAddressesInput = params
	return &ec2.DescribeAddressesOutput{Addresses: f.addresses}, nil
}

func (f *fakeEC2) DescribeInstances(_ context.Context, _ *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	return &ec2.DescribeInstancesOutput{
		Reservations: []types.Reservation{{Instances: f.instances}},
	}, nil
}

func (f *fakeEC2) DescribeInstanceAttribute(_ context.Context, params *ec2.DescribeInstanceAttributeInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstanceAttributeOutput, error) {
	f.describeInstanceAttributeInput = params
	return &ec2.DescribeInstanceAttributeOutput{
		SourceDestCheck: &types.AttributeBooleanValue{Value: awssdk.Bool(f.sourceDestCheck)},
	}, nil
}

func (f *fakeEC2) ModifyInstanceAttribute(_ context.Context, params *ec2.ModifyInstanceAttributeInput, _ ...func(*ec2.Options)) (*ec2.ModifyInstanceAttributeOutput, error) {
	f.modifyInstanceAttributeInput = params
	return &ec2.ModifyInstanceAttributeOutput{}, nil
}
