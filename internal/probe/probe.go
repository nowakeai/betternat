package probe

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
)

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type Result struct {
	ObservedIP string `json:"observed_ip"`
	ExpectedIP string `json:"expected_ip"`
	Matched    bool   `json:"matched"`
}

type SourceIPProbe struct {
	URL        string
	ExpectedIP string
	Client     HTTPClient
}

func (p SourceIPProbe) Run(ctx context.Context) (Result, error) {
	if p.URL == "" {
		return Result{}, fmt.Errorf("probe url is required")
	}
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.URL, nil)
	if err != nil {
		return Result{}, fmt.Errorf("build probe request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("run source ip probe: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("source ip probe returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if err != nil {
		return Result{}, fmt.Errorf("read source ip probe response: %w", err)
	}
	observed := strings.TrimSpace(string(body))
	if net.ParseIP(observed) == nil {
		return Result{}, fmt.Errorf("source ip probe response %q is not an IP address", observed)
	}
	result := Result{
		ObservedIP: observed,
		ExpectedIP: p.ExpectedIP,
		Matched:    p.ExpectedIP == "" || observed == p.ExpectedIP,
	}
	return result, nil
}
