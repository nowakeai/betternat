package awscloud

import (
	"context"
	"testing"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling/types"
)

func TestDescribeASG(t *testing.T) {
	client := &fakeAutoScaling{
		groups: []types.AutoScalingGroup{{
			AutoScalingGroupName: awssdk.String("betternat-prod-us-west-2a"),
			DesiredCapacity:      awssdk.Int32(2),
			MinSize:              awssdk.Int32(1),
			MaxSize:              awssdk.Int32(3),
			Instances: []types.Instance{
				{
					InstanceId:       awssdk.String("i-a"),
					LifecycleState:   types.LifecycleStateInService,
					HealthStatus:     awssdk.String("Healthy"),
					AvailabilityZone: awssdk.String("us-west-2a"),
				},
			},
		}},
	}
	provider := NewASGProviderFromClient(client)

	info, err := provider.DescribeASG(context.Background(), "betternat-prod-us-west-2a")
	if err != nil {
		t.Fatalf("describe asg: %v", err)
	}
	if client.input == nil || len(client.input.AutoScalingGroupNames) != 1 {
		t.Fatalf("unexpected input: %#v", client.input)
	}
	if info.Name != "betternat-prod-us-west-2a" || info.DesiredCapacity != 2 {
		t.Fatalf("unexpected info: %#v", info)
	}
	if len(info.Instances) != 1 || info.Instances[0].LifecycleState != "InService" {
		t.Fatalf("unexpected instances: %#v", info.Instances)
	}
}

type fakeAutoScaling struct {
	input  *autoscaling.DescribeAutoScalingGroupsInput
	groups []types.AutoScalingGroup
}

func (f *fakeAutoScaling) DescribeAutoScalingGroups(_ context.Context, params *autoscaling.DescribeAutoScalingGroupsInput, _ ...func(*autoscaling.Options)) (*autoscaling.DescribeAutoScalingGroupsOutput, error) {
	f.input = params
	return &autoscaling.DescribeAutoScalingGroupsOutput{AutoScalingGroups: f.groups}, nil
}
