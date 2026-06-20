package probe

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestSourceIPProbeMatchesExpectedIP(t *testing.T) {
	result, err := SourceIPProbe{
		URL:        "https://checkip.example",
		ExpectedIP: "203.0.113.10",
		Client:     fakeClient{status: 200, body: "203.0.113.10\n"},
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if !result.Matched || result.ObservedIP != "203.0.113.10" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestSourceIPProbeDetectsMismatch(t *testing.T) {
	result, err := SourceIPProbe{
		URL:        "https://checkip.example",
		ExpectedIP: "203.0.113.10",
		Client:     fakeClient{status: 200, body: "198.51.100.10"},
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if result.Matched {
		t.Fatalf("expected mismatch: %#v", result)
	}
}

func TestSourceIPProbeRejectsInvalidBody(t *testing.T) {
	_, err := SourceIPProbe{
		URL:    "https://checkip.example",
		Client: fakeClient{status: 200, body: "not-an-ip"},
	}.Run(context.Background())
	if err == nil {
		t.Fatal("expected invalid IP error")
	}
}

func TestSourceIPProbeRejectsBadStatus(t *testing.T) {
	_, err := SourceIPProbe{
		URL:    "https://checkip.example",
		Client: fakeClient{status: 503, body: "unavailable"},
	}.Run(context.Background())
	if err == nil {
		t.Fatal("expected bad status error")
	}
}

type fakeClient struct {
	status int
	body   string
}

func (c fakeClient) Do(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: c.status,
		Body:       io.NopCloser(strings.NewReader(c.body)),
	}, nil
}
