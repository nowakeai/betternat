package awscloud

import (
	"context"
	"testing"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling/types"

	"github.com/nowakeai/betternat/internal/cloud"
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

func TestCompleteLifecycleAction(t *testing.T) {
	client := &fakeAutoScaling{}
	provider := NewASGProviderFromClient(client)

	err := provider.CompleteLifecycleAction(context.Background(), cloud.LifecycleAction{
		AutoScalingGroupName: "betternat-prod-us-west-2a",
		LifecycleHookName:    "betternat-prod-us-west-2a-terminating",
		InstanceID:           "i-a",
	})
	if err != nil {
		t.Fatalf("complete lifecycle action: %v", err)
	}
	if client.completeInput == nil {
		t.Fatal("expected complete lifecycle action input")
	}
	if awssdk.ToString(client.completeInput.AutoScalingGroupName) != "betternat-prod-us-west-2a" {
		t.Fatalf("unexpected complete input: %#v", client.completeInput)
	}
	if awssdk.ToString(client.completeInput.LifecycleHookName) != "betternat-prod-us-west-2a-terminating" {
		t.Fatalf("unexpected hook: %#v", client.completeInput)
	}
	if awssdk.ToString(client.completeInput.LifecycleActionResult) != "CONTINUE" {
		t.Fatalf("unexpected result: %#v", client.completeInput)
	}
}

type fakeAutoScaling struct {
	completeInput *autoscaling.CompleteLifecycleActionInput
	input         *autoscaling.DescribeAutoScalingGroupsInput
	groups        []types.AutoScalingGroup
}

func (f *fakeAutoScaling) CompleteLifecycleAction(_ context.Context, params *autoscaling.CompleteLifecycleActionInput, _ ...func(*autoscaling.Options)) (*autoscaling.CompleteLifecycleActionOutput, error) {
	f.completeInput = params
	return &autoscaling.CompleteLifecycleActionOutput{}, nil
}

func (f *fakeAutoScaling) DescribeAutoScalingGroups(_ context.Context, params *autoscaling.DescribeAutoScalingGroupsInput, _ ...func(*autoscaling.Options)) (*autoscaling.DescribeAutoScalingGroupsOutput, error) {
	f.input = params
	return &autoscaling.DescribeAutoScalingGroupsOutput{AutoScalingGroups: f.groups}, nil
}
