package datapath

import (
	"context"

	"github.com/betternat/betternat/internal/config"
)

// Engine reconciles and reports a local NAT datapath implementation.
type Engine interface {
	Name() string
	EnsureReady(ctx context.Context, cfg config.DatapathConfig) error
	Reconcile(ctx context.Context, cfg config.DatapathConfig) error
	Status(ctx context.Context) (Status, error)
	Counters(ctx context.Context) (Counters, error)
	ConntrackSummary(ctx context.Context) (ConntrackSummary, error)
	Cleanup(ctx context.Context) error
}

type Status struct {
	Ready   bool   `json:"ready"`
	Engine  string `json:"engine"`
	Message string `json:"message"`
}

type Counters struct {
	Rules []RuleCounter `json:"rules"`
}

type RuleCounter struct {
	CIDR    string `json:"cidr"`
	Packets uint64 `json:"packets"`
	Bytes   uint64 `json:"bytes"`
}

type ConntrackSummary struct {
	Entries     uint64            `json:"entries"`
	Established map[string]uint64 `json:"established"`
	UDPEntries  uint64            `json:"udp_entries"`
}
