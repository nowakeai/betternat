package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/nowakeai/betternat/internal/agentapi"
	"github.com/nowakeai/betternat/internal/config"
	"github.com/nowakeai/betternat/internal/coordination"
)

func prepareHandoverTarget(ctx context.Context, cfg config.Config, status agentapi.StatusResponse, req agentapi.HandoverRequest, source string, target string, generation uint64) error {
	if cfg.Control.PeerAPI.AuthToken == "" {
		return nil
	}
	targetRow := findStatusInstance(status, target)
	if targetRow.ControlURL == "" {
		return nil
	}
	prepare := agentapi.HandoverPrepareRequest{
		RequestID:       req.RequestID,
		SourceNodeID:    source,
		TargetNodeID:    target,
		LeaseGeneration: generation,
		Reason:          req.Reason,
	}
	body, err := json.Marshal(prepare)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(targetRow.ControlURL, "/")+agentapi.HandoverPreparePath, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+cfg.Control.PeerAPI.AuthToken)
	httpResp, err := (&http.Client{Timeout: controlHTTPTimeout}).Do(httpReq)
	if err != nil {
		return fmt.Errorf("prepare handover target %q: %w", target, err)
	}
	defer httpResp.Body.Close()
	var resp agentapi.HandoverResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return err
	}
	if resp.Error != "" || resp.Status != "prepared" {
		if resp.Error == "" {
			resp.Error = "target did not accept prepare"
		}
		return fmt.Errorf("prepare handover target %q: %s", target, resp.Error)
	}
	return nil
}

func requestedTargetNode(req agentapi.HandoverRequest) string {
	if req.TargetNodeID != "" {
		return req.TargetNodeID
	}
	return req.TargetInstanceID
}

func requestSourceNode(req agentapi.HandoverPrepareRequest) string {
	if req.SourceNodeID != "" {
		return req.SourceNodeID
	}
	return req.SourceInstanceID
}

func requestTargetNode(req agentapi.HandoverPrepareRequest) string {
	if req.TargetNodeID != "" {
		return req.TargetNodeID
	}
	return req.TargetInstanceID
}

func statusNodeID(row agentapi.StatusInstance) string {
	if row.NodeID != "" {
		return row.NodeID
	}
	return row.InstanceID
}

func agentRecordNodeID(record coordination.AgentRecord) string {
	if record.NodeID != "" {
		return record.NodeID
	}
	return record.InstanceID
}

func handoverRecordSourceNodeID(record coordination.HandoverRecord) string {
	if record.SourceNodeID != "" {
		return record.SourceNodeID
	}
	return record.SourceInstanceID
}

func handoverRecordTargetNodeID(record coordination.HandoverRecord) string {
	if record.TargetNodeID != "" {
		return record.TargetNodeID
	}
	return record.TargetInstanceID
}

func findStatusInstance(status agentapi.StatusResponse, instanceID string) agentapi.StatusInstance {
	for _, row := range status.Instances {
		if statusNodeID(row) == instanceID {
			return row
		}
	}
	return agentapi.StatusInstance{}
}

func selectHandoverTarget(status agentapi.StatusResponse, source string, requested string) (string, error) {
	if requested == "" {
		requested = "auto"
	}
	for _, row := range status.Instances {
		rowNodeID := statusNodeID(row)
		if rowNodeID == "" || rowNodeID == source {
			continue
		}
		if requested != "auto" && rowNodeID != requested {
			continue
		}
		if row.Role != "standby" {
			if requested != "auto" {
				return "", fmt.Errorf("handover target %q is not standby", rowNodeID)
			}
			continue
		}
		if row.Health != "" && row.Health != "Healthy" {
			if requested != "auto" {
				return "", fmt.Errorf("handover target %q is not healthy", rowNodeID)
			}
			continue
		}
		if !row.Fresh {
			if requested != "auto" {
				return "", fmt.Errorf("handover target %q is stale", rowNodeID)
			}
			continue
		}
		if err := validateHandoverTargetGeneration(status, row); err != nil {
			if requested != "auto" {
				return "", fmt.Errorf("handover target %q %w", rowNodeID, err)
			}
			continue
		}
		return rowNodeID, nil
	}
	if requested == "auto" {
		return "", fmt.Errorf("no healthy standby target is available")
	}
	return "", fmt.Errorf("handover target %q was not found", requested)
}

func validateHandoverTargetGeneration(status agentapi.StatusResponse, row agentapi.StatusInstance) error {
	if status.LeaseGeneration == 0 {
		return nil
	}
	if row.LeaseGeneration == 0 {
		if status.Cloud == "gcp" {
			return fmt.Errorf("is missing lease generation")
		}
		return nil
	}
	if row.LeaseGeneration != status.LeaseGeneration {
		return fmt.Errorf("has stale lease generation")
	}
	return nil
}
