package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nowakeai/betternat/internal/agentapi"
	"github.com/nowakeai/betternat/internal/cloud"
	awscloud "github.com/nowakeai/betternat/internal/cloud/aws"
	"github.com/nowakeai/betternat/internal/config"
)

func defaultTerminationHandling(ctx context.Context, cfg config.Config, watcher TerminationWatcher, completer LifecycleCompleter) (TerminationWatcher, LifecycleCompleter) {
	if cfg.Cloud != "aws" || !cfg.HA.Enabled {
		return watcher, completer
	}
	if watcher == nil {
		created, err := awscloud.NewTerminationWatcher(ctx, cfg.Region, cfg.GatewayID, cfg.Local.AvailabilityZone, cfg.Local.NodeID)
		if err != nil {
			return nil, completer
		}
		watcher = created
	}
	if completer == nil {
		created, err := awscloud.NewASGProvider(ctx, cfg.Region)
		if err != nil {
			return watcher, nil
		}
		completer = created
	}
	return watcher, completer
}

func watchTermination(ctx context.Context, watcher TerminationWatcher, handle func(cloud.LifecycleAction)) <-chan cloud.LifecycleAction {
	actions := make(chan cloud.LifecycleAction, 1)
	go func() {
		action, err := watcher.Run(ctx)
		if err != nil {
			return
		}
		logTermination(action)
		actions <- action
		if handle != nil {
			handle(action)
		}
	}()
	return actions
}

func watchGracefulStop(ctx context.Context, cancel context.CancelFunc, handover func(context.Context, agentapi.HandoverRequest) agentapi.HandoverResponse) {
	go func() {
		<-ctx.Done()
		if handover != nil {
			handoverCtx, handoverCancel := context.WithTimeout(context.Background(), handoverTimeout+5*time.Second)
			defer handoverCancel()
			resp := handover(handoverCtx, agentapi.HandoverRequest{
				RequestID:    fmt.Sprintf("systemd-stop-%d", time.Now().UnixNano()),
				TargetNodeID: "auto",
				Reason:       "systemd-stop",
			})
			if resp.Error != "" {
				_, _ = fmt.Fprintf(os.Stderr, "betternat-agent: graceful stop handover failed: %s\n", resp.Error)
			}
		}
		cancel()
	}()
}

func logTermination(action cloud.LifecycleAction) {
	_, _ = fmt.Fprintf(
		os.Stderr,
		"betternat-agent: termination event reason=%s asg=%s hook=%s instance=%s\n",
		action.Reason,
		action.AutoScalingGroupName,
		action.LifecycleHookName,
		action.InstanceID,
	)
}

func terminationHandoverRequestID(action cloud.LifecycleAction) string {
	parts := []string{"termination", action.InstanceID, action.Reason}
	if action.LifecycleHookName != "" {
		parts = append(parts, action.LifecycleHookName)
	}
	for i, part := range parts {
		parts[i] = strings.NewReplacer(" ", "-", "/", "-", ":", "-").Replace(part)
	}
	return strings.Join(parts, "-")
}
