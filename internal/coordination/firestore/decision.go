package firestorecoord

import (
	"fmt"
	"time"

	"github.com/nowakeai/betternat/internal/lease"
)

const leaseRecordID = "lease"

const (
	agentRecordType    = "agent"
	handoverRecordType = "handover"
	agentPrefix        = "agent#"
	handoverPrefix     = "handover#"
)

type leaseDocument struct {
	RecordType      string    `firestore:"record_type"`
	GatewayID       string    `firestore:"gateway_id"`
	HAGroupID       string    `firestore:"ha_group_id"`
	OwnerInstanceID string    `firestore:"owner_instance_id"`
	Generation      uint64    `firestore:"generation"`
	ExpiresAt       time.Time `firestore:"expires_at"`
	UpdatedAt       time.Time `firestore:"updated_at"`
}

func acquireLease(current leaseDocument, exists bool, gatewayID string, haGroupID string, owner string, now time.Time, ttl time.Duration) (leaseDocument, error) {
	if owner == "" {
		return leaseDocument{}, fmt.Errorf("lease owner is required")
	}
	if exists && current.OwnerInstanceID != "" && current.OwnerInstanceID != owner && now.Before(current.ExpiresAt) {
		return leaseDocument{}, fmt.Errorf("lease is held by %q until %s", current.OwnerInstanceID, current.ExpiresAt.UTC().Format(time.RFC3339))
	}
	return leaseDocument{
		RecordType:      leaseRecordID,
		GatewayID:       gatewayID,
		HAGroupID:       haGroupID,
		OwnerInstanceID: owner,
		Generation:      current.Generation + 1,
		ExpiresAt:       now.Add(ttl),
		UpdatedAt:       now,
	}, nil
}

func renewLease(current leaseDocument, record lease.Record, now time.Time, ttl time.Duration) (leaseDocument, error) {
	if record.OwnerInstanceID == "" {
		return leaseDocument{}, fmt.Errorf("lease owner is required")
	}
	if !sameOwnerAndGeneration(current, record) {
		return leaseDocument{}, fmt.Errorf("lease fencing check failed")
	}
	if !now.Before(current.ExpiresAt) {
		return leaseDocument{}, fmt.Errorf("lease expired at %s", current.ExpiresAt.UTC().Format(time.RFC3339))
	}
	current.ExpiresAt = now.Add(ttl)
	current.UpdatedAt = now
	return current, nil
}

func transferLease(current leaseDocument, record lease.Record, newOwner string, now time.Time, ttl time.Duration) (leaseDocument, error) {
	if record.OwnerInstanceID == "" {
		return leaseDocument{}, fmt.Errorf("lease owner is required")
	}
	if newOwner == "" {
		return leaseDocument{}, fmt.Errorf("new lease owner is required")
	}
	if !sameOwnerAndGeneration(current, record) {
		return leaseDocument{}, fmt.Errorf("lease fencing check failed")
	}
	if !now.Before(current.ExpiresAt) {
		return leaseDocument{}, fmt.Errorf("lease expired at %s", current.ExpiresAt.UTC().Format(time.RFC3339))
	}
	current.OwnerInstanceID = newOwner
	current.Generation++
	current.ExpiresAt = now.Add(ttl)
	current.UpdatedAt = now
	return current, nil
}

func releaseLease(current leaseDocument, record lease.Record) error {
	if record.OwnerInstanceID == "" {
		return fmt.Errorf("lease owner is required")
	}
	if !sameOwnerAndGeneration(current, record) {
		return fmt.Errorf("lease fencing check failed")
	}
	return nil
}

func sameOwnerAndGeneration(current leaseDocument, record lease.Record) bool {
	return current.OwnerInstanceID == record.OwnerInstanceID && current.Generation == record.Generation
}

func leaseRecordFromDocument(doc leaseDocument) lease.Record {
	return lease.Record{
		HAGroupID:       doc.HAGroupID,
		OwnerInstanceID: doc.OwnerInstanceID,
		Generation:      doc.Generation,
		ExpiresAt:       doc.ExpiresAt,
		UpdatedAt:       doc.UpdatedAt,
	}
}
