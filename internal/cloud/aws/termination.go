package awscloud

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/nowakeai/betternat/internal/cloud"
)

type MetadataAPI interface {
	GetMetadata(ctx context.Context, params *imds.GetMetadataInput, optFns ...func(*imds.Options)) (*imds.GetMetadataOutput, error)
}

type TerminationWatcher struct {
	Metadata              MetadataAPI
	PollInterval          time.Duration
	AutoScalingGroupName  string
	LifecycleHookName     string
	InstanceID            string
	SuppressMetadataError bool
}

func NewTerminationWatcher(ctx context.Context, region string, gatewayID string, availabilityZone string, instanceID string) (*TerminationWatcher, error) {
	if gatewayID == "" {
		return nil, fmt.Errorf("gateway id is required")
	}
	if availabilityZone == "" {
		return nil, fmt.Errorf("availability zone is required")
	}
	if instanceID == "" || instanceID == "auto" {
		return nil, fmt.Errorf("resolved instance id is required")
	}
	cfg, err := LoadConfig(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	asgName := "betternat-" + gatewayID + "-" + availabilityZone
	return &TerminationWatcher{
		Metadata:             imds.NewFromConfig(cfg),
		PollInterval:         5 * time.Second,
		AutoScalingGroupName: asgName,
		LifecycleHookName:    asgName + "-terminating",
		InstanceID:           instanceID,
	}, nil
}

func (w *TerminationWatcher) Run(ctx context.Context) (cloud.LifecycleAction, error) {
	interval := w.PollInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	for {
		action, ok, err := w.Check(ctx)
		if err != nil && !w.SuppressMetadataError {
			log.Printf("betternat_termination_watch error=%q", err.Error())
		}
		if ok {
			return action, nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return cloud.LifecycleAction{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func (w *TerminationWatcher) Check(ctx context.Context) (cloud.LifecycleAction, bool, error) {
	if w.Metadata == nil {
		return cloud.LifecycleAction{}, false, fmt.Errorf("metadata client is required")
	}
	if text, ok, err := w.metadata(ctx, "spot/instance-action"); err != nil {
		return cloud.LifecycleAction{}, false, err
	} else if ok && strings.TrimSpace(text) != "" {
		return w.action("spot-instance-action"), true, nil
	}
	if text, ok, err := w.metadata(ctx, "autoscaling/target-lifecycle-state"); err != nil {
		return cloud.LifecycleAction{}, false, err
	} else if ok && isTerminatingLifecycleState(text) {
		return w.action("autoscaling-target-lifecycle-state:" + strings.TrimSpace(text)), true, nil
	}
	return cloud.LifecycleAction{}, false, nil
}

func (w *TerminationWatcher) action(reason string) cloud.LifecycleAction {
	return cloud.LifecycleAction{
		AutoScalingGroupName: w.AutoScalingGroupName,
		LifecycleHookName:    w.LifecycleHookName,
		InstanceID:           w.InstanceID,
		Result:               "CONTINUE",
		Reason:               reason,
	}
}

func (w *TerminationWatcher) metadata(ctx context.Context, path string) (string, bool, error) {
	output, err := w.Metadata.GetMetadata(ctx, &imds.GetMetadataInput{Path: path})
	if err != nil {
		if isMetadataNotFound(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("imds %s: %w", path, err)
	}
	defer output.Content.Close()
	body, err := io.ReadAll(output.Content)
	if err != nil {
		return "", false, fmt.Errorf("read imds %s: %w", path, err)
	}
	return string(body), true, nil
}

func isMetadataNotFound(err error) bool {
	var responseErr *smithyhttp.ResponseError
	if errors.As(err, &responseErr) && responseErr.HTTPStatusCode() == 404 {
		return true
	}
	return false
}

func isTerminatingLifecycleState(value string) bool {
	state := strings.ToLower(strings.TrimSpace(value))
	return strings.Contains(state, "terminat") || strings.Contains(state, "detach")
}
