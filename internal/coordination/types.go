package coordination

import (
	"context"
	"time"

	"github.com/nowakeai/betternat/internal/lease"
)

type AgentRecord struct {
	GatewayID           string
	HAGroupID           string
	NodeID              string
	InstanceID          string
	Cloud               string
	Region              string
	AvailabilityZone    string
	PrivateIP           string
	PublicIP            string
	MetricsURL          string
	ControlURL          string
	Version             string
	Commit              string
	DatapathEngine      string
	DatapathReady       bool
	HAState             string
	LeaseGeneration     uint64
	RouteTargetMatch    bool
	PublicIdentityMatch bool
	UpdatedAt           time.Time
	ExpiresAt           time.Time
}

type HandoverRecord struct {
	RequestID        string
	HAGroupID        string
	Status           string
	SourceNodeID     string
	TargetNodeID     string
	SourceInstanceID string
	TargetInstanceID string
	Reason           string
	LeaseGeneration  uint64
	Message          string
	Error            string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	ExpiresAt        time.Time
}

type AgentRegistry interface {
	PutAgent(context.Context, AgentRecord, time.Duration) error
	DeleteAgent(context.Context, string) error
	ListAgents(context.Context) ([]AgentRecord, error)
}

type AgentReader interface {
	Current(context.Context) (lease.Record, error)
	ListAgents(context.Context) ([]AgentRecord, error)
}

type HandoverStore interface {
	CreateHandover(context.Context, HandoverRecord, time.Duration) (HandoverRecord, error)
	UpdateHandover(context.Context, HandoverRecord, time.Duration) (HandoverRecord, error)
	GetHandover(context.Context, string) (HandoverRecord, error)
	Current(context.Context) (lease.Record, error)
}

type HandoverReader interface {
	GetHandover(context.Context, string) (HandoverRecord, error)
	ListHandovers(context.Context) ([]HandoverRecord, error)
	Current(context.Context) (lease.Record, error)
}

type Store interface {
	lease.Manager
	lease.Transferer
	AgentRegistry
	AgentReader
	HandoverStore
	HandoverReader
}
