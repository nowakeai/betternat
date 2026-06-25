package firestorecoord

import (
	"fmt"
	"time"

	"github.com/nowakeai/betternat/internal/coordination"
)

type agentDocument struct {
	RecordType          string    `firestore:"record_type"`
	GatewayID           string    `firestore:"gateway_id"`
	HAGroupID           string    `firestore:"ha_group_id"`
	NodeID              string    `firestore:"node_id"`
	InstanceID          string    `firestore:"instance_id"`
	Cloud               string    `firestore:"cloud"`
	Region              string    `firestore:"region"`
	AvailabilityZone    string    `firestore:"availability_zone"`
	PrivateIP           string    `firestore:"private_ip"`
	PublicIP            string    `firestore:"public_ip"`
	MetricsURL          string    `firestore:"metrics_url"`
	ControlURL          string    `firestore:"control_url"`
	Version             string    `firestore:"version"`
	Commit              string    `firestore:"commit"`
	DatapathEngine      string    `firestore:"datapath_engine"`
	DatapathReady       bool      `firestore:"datapath_ready"`
	HAState             string    `firestore:"ha_state"`
	LeaseGeneration     int64     `firestore:"lease_generation"`
	RouteTargetMatch    bool      `firestore:"route_target_match"`
	PublicIdentityMatch bool      `firestore:"public_identity_match"`
	UpdatedAt           time.Time `firestore:"updated_at"`
	ExpiresAt           time.Time `firestore:"expires_at"`
}

type handoverDocument struct {
	RecordType       string    `firestore:"record_type"`
	GatewayID        string    `firestore:"gateway_id"`
	HAGroupID        string    `firestore:"ha_group_id"`
	RequestID        string    `firestore:"request_id"`
	Status           string    `firestore:"status"`
	SourceNodeID     string    `firestore:"source_node_id"`
	TargetNodeID     string    `firestore:"target_node_id"`
	SourceInstanceID string    `firestore:"source_instance_id"`
	TargetInstanceID string    `firestore:"target_instance_id"`
	Reason           string    `firestore:"reason"`
	LeaseGeneration  int64     `firestore:"lease_generation"`
	Message          string    `firestore:"message"`
	Error            string    `firestore:"error"`
	CreatedAt        time.Time `firestore:"created_at"`
	UpdatedAt        time.Time `firestore:"updated_at"`
	ExpiresAt        time.Time `firestore:"expires_at"`
}

func (b *Backend) agentDocument(record coordination.AgentRecord, ttl time.Duration) (agentDocument, error) {
	if record.NodeID == "" && record.InstanceID != "" {
		record.NodeID = record.InstanceID
	}
	if record.NodeID == "" {
		return agentDocument{}, fmt.Errorf("agent node id is required")
	}
	if record.HAGroupID == "" {
		record.HAGroupID = b.haGroupID
	}
	if record.GatewayID == "" {
		record.GatewayID = b.gatewayID
	}
	now := b.now()
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = now
	}
	if ttl <= 0 {
		ttl = 20 * time.Second
	}
	if record.ExpiresAt.IsZero() {
		record.ExpiresAt = record.UpdatedAt.Add(ttl)
	}
	leaseGeneration, ok := generationToFirestore(record.LeaseGeneration)
	if !ok {
		return agentDocument{}, fmt.Errorf("agent lease generation exceeded firestore integer range")
	}
	return agentDocument{
		RecordType:          agentRecordType,
		GatewayID:           record.GatewayID,
		HAGroupID:           record.HAGroupID,
		NodeID:              record.NodeID,
		InstanceID:          record.InstanceID,
		Cloud:               record.Cloud,
		Region:              record.Region,
		AvailabilityZone:    record.AvailabilityZone,
		PrivateIP:           record.PrivateIP,
		PublicIP:            record.PublicIP,
		MetricsURL:          record.MetricsURL,
		ControlURL:          record.ControlURL,
		Version:             record.Version,
		Commit:              record.Commit,
		DatapathEngine:      record.DatapathEngine,
		DatapathReady:       record.DatapathReady,
		HAState:             record.HAState,
		LeaseGeneration:     leaseGeneration,
		RouteTargetMatch:    record.RouteTargetMatch,
		PublicIdentityMatch: record.PublicIdentityMatch,
		UpdatedAt:           record.UpdatedAt,
		ExpiresAt:           record.ExpiresAt,
	}, nil
}

func agentRecordFromDocument(doc agentDocument) coordination.AgentRecord {
	return coordination.AgentRecord{
		GatewayID:           doc.GatewayID,
		HAGroupID:           doc.HAGroupID,
		NodeID:              firstString(doc.NodeID, doc.InstanceID),
		InstanceID:          doc.InstanceID,
		Cloud:               doc.Cloud,
		Region:              doc.Region,
		AvailabilityZone:    doc.AvailabilityZone,
		PrivateIP:           doc.PrivateIP,
		PublicIP:            doc.PublicIP,
		MetricsURL:          doc.MetricsURL,
		ControlURL:          doc.ControlURL,
		Version:             doc.Version,
		Commit:              doc.Commit,
		DatapathEngine:      doc.DatapathEngine,
		DatapathReady:       doc.DatapathReady,
		HAState:             doc.HAState,
		LeaseGeneration:     generationFromFirestore(doc.LeaseGeneration),
		RouteTargetMatch:    doc.RouteTargetMatch,
		PublicIdentityMatch: doc.PublicIdentityMatch,
		UpdatedAt:           doc.UpdatedAt,
		ExpiresAt:           doc.ExpiresAt,
	}
}

func (b *Backend) handoverDocument(record coordination.HandoverRecord, ttl time.Duration, create bool) (handoverDocument, error) {
	if record.RequestID == "" {
		return handoverDocument{}, fmt.Errorf("handover request id is required")
	}
	if record.SourceNodeID == "" && record.SourceInstanceID != "" {
		record.SourceNodeID = record.SourceInstanceID
	}
	if record.TargetNodeID == "" && record.TargetInstanceID != "" {
		record.TargetNodeID = record.TargetInstanceID
	}
	if create && record.SourceNodeID == "" {
		return handoverDocument{}, fmt.Errorf("handover source node id is required")
	}
	if record.HAGroupID == "" {
		record.HAGroupID = b.haGroupID
	}
	now := b.now()
	if create && record.Status == "" {
		record.Status = "requested"
	}
	if create && record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = now
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	if record.ExpiresAt.IsZero() {
		record.ExpiresAt = record.UpdatedAt.Add(ttl)
	}
	leaseGeneration, ok := generationToFirestore(record.LeaseGeneration)
	if !ok {
		return handoverDocument{}, fmt.Errorf("handover lease generation exceeded firestore integer range")
	}
	return handoverDocument{
		RecordType:       handoverRecordType,
		GatewayID:        b.gatewayID,
		HAGroupID:        record.HAGroupID,
		RequestID:        record.RequestID,
		Status:           record.Status,
		SourceNodeID:     record.SourceNodeID,
		TargetNodeID:     record.TargetNodeID,
		SourceInstanceID: record.SourceInstanceID,
		TargetInstanceID: record.TargetInstanceID,
		Reason:           record.Reason,
		LeaseGeneration:  leaseGeneration,
		Message:          record.Message,
		Error:            record.Error,
		CreatedAt:        record.CreatedAt,
		UpdatedAt:        record.UpdatedAt,
		ExpiresAt:        record.ExpiresAt,
	}, nil
}

func handoverRecordFromDocument(doc handoverDocument) coordination.HandoverRecord {
	return coordination.HandoverRecord{
		RequestID:        doc.RequestID,
		HAGroupID:        doc.HAGroupID,
		Status:           doc.Status,
		SourceNodeID:     firstString(doc.SourceNodeID, doc.SourceInstanceID),
		TargetNodeID:     firstString(doc.TargetNodeID, doc.TargetInstanceID),
		SourceInstanceID: doc.SourceInstanceID,
		TargetInstanceID: doc.TargetInstanceID,
		Reason:           doc.Reason,
		LeaseGeneration:  generationFromFirestore(doc.LeaseGeneration),
		Message:          doc.Message,
		Error:            doc.Error,
		CreatedAt:        doc.CreatedAt,
		UpdatedAt:        doc.UpdatedAt,
		ExpiresAt:        doc.ExpiresAt,
	}
}

func agentRecordID(nodeID string) string {
	return agentPrefix + nodeID
}

func handoverRecordID(requestID string) string {
	return handoverPrefix + requestID
}

func firstString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
