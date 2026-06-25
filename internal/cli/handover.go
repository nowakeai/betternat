package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"

	"github.com/nowakeai/betternat/internal/agentapi"
	"github.com/nowakeai/betternat/internal/config"
	"github.com/nowakeai/betternat/internal/coordination"
	dynamodbcoord "github.com/nowakeai/betternat/internal/coordination/dynamodb"
)

type handoverOptions struct {
	host         string
	configPath   string
	output       outputFormat
	timeout      time.Duration
	target       string
	reason       string
	limit        int
	status       string
	includeStale bool
}

type handoverRecordOutput struct {
	RequestID       string    `json:"request_id"`
	Status          string    `json:"status"`
	SourceNodeID    string    `json:"source_node_id,omitempty"`
	TargetNodeID    string    `json:"target_node_id,omitempty"`
	Reason          string    `json:"reason,omitempty"`
	LeaseGeneration uint64    `json:"lease_generation,omitempty"`
	Message         string    `json:"message,omitempty"`
	Error           string    `json:"error,omitempty"`
	CreatedAt       time.Time `json:"created_at,omitempty"`
	UpdatedAt       time.Time `json:"updated_at,omitempty"`
	ExpiresAt       time.Time `json:"expires_at,omitempty"`
}

var newHandoverStoreReader = func(ctx context.Context, cfg config.Config) (coordination.HandoverReader, error) {
	return dynamodbcoord.New(ctx, cfg.Region, cfg.Coordination.Table, doctorLeaseKey(cfg), doctorLeaseTTL(cfg))
}

func newHandoverCommand(ctx context.Context, out io.Writer) *cobra.Command {
	opts := handoverOptions{host: defaultAgentHost(), configPath: defaultConfigPath, output: outputTable, timeout: 30 * time.Second, target: "auto", limit: 20}
	cmd := &cobra.Command{
		Use:   "handover",
		Short: "Inspect or start proactive active handover",
	}
	current := &cobra.Command{
		Use:     "current",
		Aliases: []string{"status"},
		Short:   "Show current handover state",
		RunE: func(*cobra.Command, []string) error {
			return runHandoverStatus(ctx, opts, out)
		},
	}
	current.Flags().StringVar(&opts.host, "host", opts.host, "agent daemon endpoint")
	current.Flags().VarP((*outputFlag)(&opts.output), "output", "o", "output format: table or json")
	current.Flags().DurationVar(&opts.timeout, "timeout", opts.timeout, "daemon request timeout")

	start := &cobra.Command{
		Use:   "start",
		Short: "Start proactive handover from the active daemon",
		RunE: func(*cobra.Command, []string) error {
			return runHandoverStart(ctx, opts, out)
		},
	}
	start.Flags().StringVar(&opts.host, "host", opts.host, "agent daemon endpoint")
	start.Flags().VarP((*outputFlag)(&opts.output), "output", "o", "output format: table or json")
	start.Flags().DurationVar(&opts.timeout, "timeout", opts.timeout, "daemon request timeout")
	start.Flags().StringVar(&opts.target, "to", opts.target, "target node id or auto")
	start.Flags().StringVar(&opts.reason, "reason", opts.reason, "handover reason")

	history := &cobra.Command{
		Use:   "history",
		Short: "List durable handover operation records",
		RunE: func(*cobra.Command, []string) error {
			return runHandoverHistory(ctx, opts, out)
		},
	}
	history.Flags().StringVar(&opts.configPath, "config", opts.configPath, "agent config path")
	history.Flags().VarP((*outputFlag)(&opts.output), "output", "o", "output format: table or json")
	history.Flags().IntVar(&opts.limit, "limit", opts.limit, "maximum records to show")
	history.Flags().StringVar(&opts.status, "status", opts.status, "filter by handover status")
	history.Flags().BoolVar(&opts.includeStale, "include-stale", opts.includeStale, "include stale non-terminal records from older lease generations")

	inspect := &cobra.Command{
		Use:   "inspect <request-id>",
		Short: "Show one durable handover operation record",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runHandoverInspect(ctx, opts, args[0], out)
		},
	}
	inspect.Flags().StringVar(&opts.configPath, "config", opts.configPath, "agent config path")
	inspect.Flags().VarP((*outputFlag)(&opts.output), "output", "o", "output format: table or json")

	cmd.AddCommand(current)
	cmd.AddCommand(start)
	cmd.AddCommand(history)
	cmd.AddCommand(inspect)
	return cmd
}

func runHandoverStatus(ctx context.Context, opts handoverOptions, out io.Writer) error {
	resp, err := requestHandover(ctx, opts.host, opts.timeout, http.MethodGet, nil)
	if err != nil {
		return handoverDaemonError(opts.host, err)
	}
	return renderHandoverResponse(out, opts.output, resp)
}

func runHandoverStart(ctx context.Context, opts handoverOptions, out io.Writer) error {
	req := agentapi.HandoverRequest{
		RequestID:    fmt.Sprintf("%d", time.Now().UnixNano()),
		TargetNodeID: opts.target,
		Reason:       opts.reason,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	resp, err := requestHandover(ctx, opts.host, opts.timeout, http.MethodPost, body)
	if err != nil {
		return handoverDaemonError(opts.host, err)
	}
	if renderErr := renderHandoverResponse(out, opts.output, resp); renderErr != nil {
		return renderErr
	}
	if resp.Error != "" {
		return fmt.Errorf("%s", resp.Error)
	}
	return nil
}

func runHandoverHistory(ctx context.Context, opts handoverOptions, out io.Writer) error {
	store, err := openHandoverStore(ctx, opts.configPath)
	if err != nil {
		return err
	}
	records, err := store.ListHandovers(ctx)
	if err != nil {
		return err
	}
	records = filterHandoverRecords(records, opts.status)
	if !opts.includeStale {
		records = filterCurrentHandoverRecords(ctx, store, records)
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].UpdatedAt.After(records[j].UpdatedAt)
	})
	if opts.limit > 0 && len(records) > opts.limit {
		records = records[:opts.limit]
	}
	return renderHandoverRecords(out, opts.output, records)
}

func runHandoverInspect(ctx context.Context, opts handoverOptions, requestID string, out io.Writer) error {
	store, err := openHandoverStore(ctx, opts.configPath)
	if err != nil {
		return err
	}
	record, err := store.GetHandover(ctx, strings.TrimPrefix(requestID, "handover#"))
	if err != nil {
		return err
	}
	return renderHandoverRecord(out, opts.output, record)
}

func openHandoverStore(ctx context.Context, configPath string) (coordination.HandoverReader, error) {
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return nil, err
	}
	if cfg.Coordination.Table == "" {
		return nil, fmt.Errorf("coordination table is not configured")
	}
	return newHandoverStoreReader(ctx, cfg)
}

func filterHandoverRecords(records []coordination.HandoverRecord, status string) []coordination.HandoverRecord {
	if status == "" {
		return records
	}
	filtered := make([]coordination.HandoverRecord, 0, len(records))
	for _, record := range records {
		if record.Status == status {
			filtered = append(filtered, record)
		}
	}
	return filtered
}

func filterCurrentHandoverRecords(ctx context.Context, store coordination.HandoverReader, records []coordination.HandoverRecord) []coordination.HandoverRecord {
	current, err := store.Current(ctx)
	if err != nil || current.Generation == 0 {
		return records
	}
	filtered := make([]coordination.HandoverRecord, 0, len(records))
	for _, record := range records {
		if record.LeaseGeneration < current.Generation && !handoverTerminalStatus(record.Status) {
			continue
		}
		filtered = append(filtered, record)
	}
	return filtered
}

func handoverTerminalStatus(status string) bool {
	switch status {
	case "completed", "failed", "rejected", "aborted", "failed_manual_intervention":
		return true
	default:
		return false
	}
}

func renderHandoverRecords(out io.Writer, format outputFormat, records []coordination.HandoverRecord) error {
	rows := make([]handoverRecordOutput, 0, len(records))
	for _, record := range records {
		rows = append(rows, handoverRecordView(record))
	}
	if format == outputJSON {
		return json.NewEncoder(out).Encode(map[string]any{
			"schema_version": "v1",
			"records":        rows,
		})
	}
	t := table.NewWriter()
	t.SetOutputMirror(out)
	t.SetStyle(statusTableStyle())
	t.AppendHeader(table.Row{"Request", "Status", "Source", "Target", "Generation", "Updated", "Message", "Error"})
	for _, row := range rows {
		t.AppendRow(table.Row{
			valueOrUnknown(row.RequestID),
			valueOrUnknown(row.Status),
			valueOrUnknown(row.SourceNodeID),
			valueOrUnknown(row.TargetNodeID),
			leaseGenerationValue(row.LeaseGeneration),
			timeValue(row.UpdatedAt),
			valueOrEmpty(row.Message),
			valueOrEmpty(row.Error),
		})
	}
	t.Render()
	return nil
}

func renderHandoverRecord(out io.Writer, format outputFormat, record coordination.HandoverRecord) error {
	if format == outputJSON {
		return json.NewEncoder(out).Encode(handoverRecordView(record))
	}
	return renderHandoverRecords(out, format, []coordination.HandoverRecord{record})
}

func handoverRecordView(record coordination.HandoverRecord) handoverRecordOutput {
	return handoverRecordOutput{
		RequestID:       record.RequestID,
		Status:          record.Status,
		SourceNodeID:    handoverRecordSourceNode(record),
		TargetNodeID:    handoverRecordTargetNode(record),
		Reason:          record.Reason,
		LeaseGeneration: record.LeaseGeneration,
		Message:         record.Message,
		Error:           record.Error,
		CreatedAt:       record.CreatedAt,
		UpdatedAt:       record.UpdatedAt,
		ExpiresAt:       record.ExpiresAt,
	}
}

func handoverRecordSourceNode(record coordination.HandoverRecord) string {
	if record.SourceNodeID != "" {
		return record.SourceNodeID
	}
	return record.SourceInstanceID
}

func handoverRecordTargetNode(record coordination.HandoverRecord) string {
	if record.TargetNodeID != "" {
		return record.TargetNodeID
	}
	return record.TargetInstanceID
}

func timeValue(value time.Time) string {
	if value.IsZero() {
		return "unknown"
	}
	return value.UTC().Format(time.RFC3339)
}

func valueOrEmpty(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func requestHandover(ctx context.Context, host string, timeout time.Duration, method string, body []byte) (agentapi.HandoverResponse, error) {
	client, baseURL, err := daemonHTTPClient(host, timeout)
	if err != nil {
		return agentapi.HandoverResponse{}, err
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	httpReq, err := http.NewRequestWithContext(reqCtx, method, baseURL+agentapi.HandoverPath, reader)
	if err != nil {
		return agentapi.HandoverResponse{}, err
	}
	if body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	httpResp, err := client.Do(httpReq)
	if err != nil {
		return agentapi.HandoverResponse{}, err
	}
	defer httpResp.Body.Close()
	var resp agentapi.HandoverResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return agentapi.HandoverResponse{}, err
	}
	return resp, nil
}

func renderHandoverResponse(out io.Writer, format outputFormat, resp agentapi.HandoverResponse) error {
	if format == outputJSON {
		return json.NewEncoder(out).Encode(resp)
	}
	source := handoverSourceNode(resp)
	target := handoverTargetNode(resp)
	if target != "" {
		_, _ = fmt.Fprintf(out, "handover %s: %s -> %s", resp.Status, source, target)
	} else {
		_, _ = fmt.Fprintf(out, "handover %s", resp.Status)
	}
	if resp.LeaseGeneration > 0 {
		_, _ = fmt.Fprintf(out, " generation=%d", resp.LeaseGeneration)
	}
	if resp.Message != "" {
		_, _ = fmt.Fprintf(out, " %s", resp.Message)
	}
	if resp.Error != "" {
		_, _ = fmt.Fprintf(out, " error=%s", resp.Error)
	}
	_, _ = fmt.Fprintln(out)
	return nil
}

func handoverSourceNode(resp agentapi.HandoverResponse) string {
	if resp.SourceNodeID != "" {
		return resp.SourceNodeID
	}
	return resp.SourceInstanceID
}

func handoverTargetNode(resp agentapi.HandoverResponse) string {
	if resp.TargetNodeID != "" {
		return resp.TargetNodeID
	}
	return resp.TargetInstanceID
}

func handoverDaemonError(host string, err error) error {
	if host == "" {
		host = defaultAgentHost()
	}
	configPath := os.Getenv("BETTERNAT_CONFIG")
	if configPath == "" {
		configPath = defaultConfigPath
	}
	return fmt.Errorf("betternat-agent daemon is not reachable at %s: %w\nTry:\n  sudo systemctl status betternat-agent\n  sudo betternat status --direct --config %s", host, err, configPath)
}
