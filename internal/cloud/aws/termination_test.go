package awscloud

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
)

func TestTerminationWatcherDetectsSpotInterruption(t *testing.T) {
	watcher := &TerminationWatcher{
		Metadata:             fakeMetadata{values: map[string]string{"spot/instance-action": `{"action":"terminate"}`}},
		AutoScalingGroupName: "betternat-prod-us-west-2a",
		LifecycleHookName:    "betternat-prod-us-west-2a-terminating",
		InstanceID:           "i-local",
	}

	action, ok, err := watcher.Check(context.Background())
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !ok {
		t.Fatal("expected interruption action")
	}
	if action.Reason != "spot-instance-action" {
		t.Fatalf("unexpected reason: %#v", action)
	}
	if action.AutoScalingGroupName != "betternat-prod-us-west-2a" || action.LifecycleHookName != "betternat-prod-us-west-2a-terminating" || action.InstanceID != "i-local" {
		t.Fatalf("unexpected action: %#v", action)
	}
}

func TestTerminationWatcherDetectsASGTargetTermination(t *testing.T) {
	watcher := &TerminationWatcher{
		Metadata:             fakeMetadata{values: map[string]string{"autoscaling/target-lifecycle-state": "Terminated"}},
		AutoScalingGroupName: "betternat-prod-us-west-2a",
		LifecycleHookName:    "betternat-prod-us-west-2a-terminating",
		InstanceID:           "i-local",
	}

	action, ok, err := watcher.Check(context.Background())
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !ok {
		t.Fatal("expected lifecycle action")
	}
	if action.Reason != "autoscaling-target-lifecycle-state:Terminated" {
		t.Fatalf("unexpected reason: %#v", action)
	}
}

func TestTerminationWatcherRunReturnsOnDetection(t *testing.T) {
	watcher := &TerminationWatcher{
		Metadata:             fakeMetadata{values: map[string]string{"autoscaling/target-lifecycle-state": "Terminated"}},
		PollInterval:         time.Hour,
		AutoScalingGroupName: "betternat-prod-us-west-2a",
		LifecycleHookName:    "betternat-prod-us-west-2a-terminating",
		InstanceID:           "i-local",
	}

	action, err := watcher.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if action.InstanceID != "i-local" {
		t.Fatalf("unexpected action: %#v", action)
	}
}

type fakeMetadata struct {
	values map[string]string
}

func (f fakeMetadata) GetMetadata(_ context.Context, params *imds.GetMetadataInput, _ ...func(*imds.Options)) (*imds.GetMetadataOutput, error) {
	if value, ok := f.values[params.Path]; ok {
		return &imds.GetMetadataOutput{Content: io.NopCloser(strings.NewReader(value))}, nil
	}
	return &imds.GetMetadataOutput{Content: io.NopCloser(strings.NewReader(""))}, nil
}
