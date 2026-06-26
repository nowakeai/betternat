package firestorecoord

import (
	"testing"
	"time"

	"github.com/nowakeai/betternat/internal/coordination"
)

func TestAgentDocumentNormalizesIdentityAndTTL(t *testing.T) {
	now := time.Unix(100, 0)
	backend := NewFromClient(nil, "gw-a", "ha-a", 30*time.Second, func() time.Time { return now })

	doc, err := backend.agentDocument(coordination.AgentRecord{
		InstanceID:      "legacy-node",
		PrivateIP:       "10.0.1.10",
		DatapathReady:   true,
		LeaseGeneration: 7,
	}, 20*time.Second)
	if err != nil {
		t.Fatalf("agent document: %v", err)
	}
	if doc.RecordType != agentRecordType || doc.GatewayID != "gw-a" || doc.HAGroupID != "ha-a" {
		t.Fatalf("unexpected document identity: %#v", doc)
	}
	if doc.NodeID != "legacy-node" || doc.ExpiresAt.Unix() != 120 || doc.UpdatedAt.Unix() != 100 {
		t.Fatalf("unexpected normalized document: %#v", doc)
	}

	record := agentRecordFromDocument(doc)
	if record.NodeID != "legacy-node" || !record.DatapathReady || record.LeaseGeneration != 7 {
		t.Fatalf("unexpected agent record: %#v", record)
	}
}

func TestHandoverDocumentDefaultsAndRoundTrips(t *testing.T) {
	now := time.Unix(100, 0)
	backend := NewFromClient(nil, "gw-a", "ha-a", 30*time.Second, func() time.Time { return now })

	doc, err := backend.handoverDocument(coordination.HandoverRecord{
		RequestID:       "req-1",
		SourceNodeID:    "node-a",
		TargetNodeID:    "node-b",
		LeaseGeneration: 7,
	}, 60*time.Second, true)
	if err != nil {
		t.Fatalf("handover document: %v", err)
	}
	if doc.RecordType != handoverRecordType || doc.Status != "requested" || doc.ExpiresAt.Unix() != 160 {
		t.Fatalf("unexpected handover defaults: %#v", doc)
	}

	record := handoverRecordFromDocument(doc)
	if record.RequestID != "req-1" || record.SourceNodeID != "node-a" || record.TargetNodeID != "node-b" || record.LeaseGeneration != 7 {
		t.Fatalf("unexpected handover record: %#v", record)
	}
}

func TestHandoverUpdateDoesNotRequireSource(t *testing.T) {
	now := time.Unix(100, 0)
	backend := NewFromClient(nil, "gw-a", "ha-a", 30*time.Second, func() time.Time { return now })

	doc, err := backend.handoverDocument(coordination.HandoverRecord{
		RequestID:       "req-1",
		Status:          "completed",
		TargetNodeID:    "node-b",
		LeaseGeneration: 8,
	}, 60*time.Second, false)
	if err != nil {
		t.Fatalf("handover update document: %v", err)
	}
	if doc.SourceNodeID != "" || doc.Status != "completed" || doc.UpdatedAt.Unix() != 100 {
		t.Fatalf("unexpected handover update: %#v", doc)
	}
}
