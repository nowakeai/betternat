package awscloud

import (
	"context"
	"fmt"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"

	"github.com/nowakeai/betternat/internal/cloud"
)

type AutoScalingAPI interface {
	CompleteLifecycleAction(ctx context.Context, params *autoscaling.CompleteLifecycleActionInput, optFns ...func(*autoscaling.Options)) (*autoscaling.CompleteLifecycleActionOutput, error)
	DescribeAutoScalingGroups(ctx context.Context, params *autoscaling.DescribeAutoScalingGroupsInput, optFns ...func(*autoscaling.Options)) (*autoscaling.DescribeAutoScalingGroupsOutput, error)
}

type ASGProvider struct {
	asg AutoScalingAPI
}

func NewASGProvider(ctx context.Context, region string) (*ASGProvider, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	return NewASGProviderFromClient(autoscaling.NewFromConfig(cfg)), nil
}

func NewASGProviderFromClient(client AutoScalingAPI) *ASGProvider {
	return &ASGProvider{asg: client}
}

func (p *ASGProvider) DescribeASG(ctx context.Context, name string) (cloud.ASGInfo, error) {
	if p.asg == nil {
		return cloud.ASGInfo{}, fmt.Errorf("autoscaling client is required")
	}
	if name == "" {
		return cloud.ASGInfo{}, fmt.Errorf("asg name is required")
	}
	output, err := p.asg.DescribeAutoScalingGroups(ctx, &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []string{name},
	})
	if err != nil {
		return cloud.ASGInfo{}, fmt.Errorf("aws autoscaling DescribeAutoScalingGroups: %w", err)
	}
	if len(output.AutoScalingGroups) == 0 {
		return cloud.ASGInfo{}, fmt.Errorf("asg %s not found", name)
	}
	group := output.AutoScalingGroups[0]
	info := cloud.ASGInfo{
		Name:            awssdk.ToString(group.AutoScalingGroupName),
		MinSize:         awssdk.ToInt32(group.MinSize),
		DesiredCapacity: awssdk.ToInt32(group.DesiredCapacity),
		MaxSize:         awssdk.ToInt32(group.MaxSize),
	}
	for _, instance := range group.Instances {
		info.Instances = append(info.Instances, cloud.ASGInstance{
			InstanceID:       awssdk.ToString(instance.InstanceId),
			LifecycleState:   string(instance.LifecycleState),
			HealthStatus:     awssdk.ToString(instance.HealthStatus),
			AvailabilityZone: awssdk.ToString(instance.AvailabilityZone),
		})
	}
	return info, nil
}

func (p *ASGProvider) CompleteLifecycleAction(ctx context.Context, action cloud.LifecycleAction) error {
	if p.asg == nil {
		return fmt.Errorf("autoscaling client is required")
	}
	if action.AutoScalingGroupName == "" {
		return fmt.Errorf("autoscaling group name is required")
	}
	if action.LifecycleHookName == "" {
		return fmt.Errorf("lifecycle hook name is required")
	}
	if action.InstanceID == "" {
		return fmt.Errorf("instance id is required")
	}
	result := action.Result
	if result == "" {
		result = "CONTINUE"
	}
	_, err := p.asg.CompleteLifecycleAction(ctx, &autoscaling.CompleteLifecycleActionInput{
		AutoScalingGroupName:  awssdk.String(action.AutoScalingGroupName),
		LifecycleActionResult: awssdk.String(result),
		LifecycleHookName:     awssdk.String(action.LifecycleHookName),
		InstanceId:            awssdk.String(action.InstanceID),
	})
	if err != nil {
		return fmt.Errorf("aws autoscaling CompleteLifecycleAction: %w", err)
	}
	return nil
}
