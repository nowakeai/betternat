package agent

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nowakeai/betternat/internal/datapath"
)

type controlMetrics struct {
	Version    string
	RXBytes    *uint64
	TXBytes    *uint64
	ObservedAt time.Time
}

func (m controlMetrics) withObservedAt(t time.Time) controlMetrics {
	m.ObservedAt = t
	return m
}

func scrapeControlMetrics(ctx context.Context, url string) (controlMetrics, error) {
	if url == "" {
		return controlMetrics{}, fmt.Errorf("metrics url is not configured")
	}
	client := &http.Client{Timeout: controlHTTPTimeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return controlMetrics{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return controlMetrics{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return controlMetrics{}, fmt.Errorf("metrics returned HTTP %d", resp.StatusCode)
	}
	var metrics controlMetrics
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, labels, value := splitControlMetricLine(line)
		switch name {
		case "betternat_agent_build_info":
			metrics.Version = labels["version"]
		case "betternat_interface_rx_bytes_total":
			if parsed, ok := parseControlUint(value); ok {
				metrics.RXBytes = &parsed
			}
		case "betternat_interface_tx_bytes_total":
			if parsed, ok := parseControlUint(value); ok {
				metrics.TXBytes = &parsed
			}
		}
	}
	return metrics, scanner.Err()
}

func splitControlMetricLine(line string) (string, map[string]string, string) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return line, nil, ""
	}
	head := fields[0]
	value := fields[1]
	labels := map[string]string{}
	open := strings.IndexByte(head, '{')
	if open == -1 {
		return head, labels, value
	}
	name := head[:open]
	close := strings.LastIndexByte(head, '}')
	if close <= open {
		return name, labels, value
	}
	for _, part := range strings.Split(head[open+1:close], ",") {
		key, val, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		unquoted, err := strconv.Unquote(val)
		if err != nil {
			unquoted = strings.Trim(val, `"`)
		}
		labels[key] = unquoted
	}
	return name, labels, value
}

func parseControlUint(value string) (uint64, bool) {
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err == nil {
		return parsed, true
	}
	floatValue, err := strconv.ParseFloat(value, 64)
	if err != nil || floatValue < 0 {
		return 0, false
	}
	return uint64(floatValue), true
}

func controlRateMbps(first *uint64, second *uint64, elapsed time.Duration) float64 {
	if first == nil || second == nil || elapsed <= 0 || *second < *first {
		return 0
	}
	return float64(*second-*first) * 8 / elapsed.Seconds() / 1_000_000
}

func sumCounterBytes(counters datapath.Counters) uint64 {
	var total uint64
	for _, rule := range counters.Rules {
		total += rule.Bytes
	}
	return total
}
