package firestorecoord

import (
	"fmt"
	"math"
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

const leaseClockSkewAllowance = 2 * time.Second

type leaseDocument struct {
	RecordType      string    `firestore:"record_type"`
	GatewayID       string    `firestore:"gateway_id"`
	HAGroupID       string    `firestore:"ha_group_id"`
	OwnerInstanceID string    `firestore:"owner_instance_id"`
	Generation      int64     `firestore:"generation"`
	ExpiresAt       time.Time `firestore:"expires_at"`
	UpdatedAt       time.Time `firestore:"updated_at"`
}

func acquireLease(current leaseDocument, exists bool, gatewayID string, haGroupID string, owner string, now time.Time, ttl time.Duration) (leaseDocument, error) {
	if owner == "" {
		return leaseDocument{}, fmt.Errorf("lease owner is required")
	}
	if exists && current.OwnerInstanceID != "" && current.OwnerInstanceID != owner && leaseStillLive(current.ExpiresAt, now) {
		return leaseDocument{}, fmt.Errorf("lease is held by %q until %s", current.OwnerInstanceID, current.ExpiresAt.UTC().Format(time.RFC3339))
	}
	if current.Generation == math.MaxInt64 {
		return leaseDocument{}, fmt.Errorf("lease generation exceeded firestore integer range")
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
	if !leaseStillLive(current.ExpiresAt, now) {
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
	if !leaseStillLive(current.ExpiresAt, now) {
		return leaseDocument{}, fmt.Errorf("lease expired at %s", current.ExpiresAt.UTC().Format(time.RFC3339))
	}
	if current.Generation == math.MaxInt64 {
		return leaseDocument{}, fmt.Errorf("lease generation exceeded firestore integer range")
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
	generation, ok := generationToFirestore(record.Generation)
	return ok && current.OwnerInstanceID == record.OwnerInstanceID && current.Generation == generation
}

func leaseStillLive(expiresAt time.Time, now time.Time) bool {
	return now.Before(expiresAt.Add(leaseClockSkewAllowance))
}

func leaseRecordFromDocument(doc leaseDocument) lease.Record {
	return lease.Record{
		HAGroupID:       doc.HAGroupID,
		OwnerInstanceID: doc.OwnerInstanceID,
		Generation:      generationFromFirestore(doc.Generation),
		ExpiresAt:       doc.ExpiresAt,
		UpdatedAt:       doc.UpdatedAt,
	}
}

func generationToFirestore(generation uint64) (int64, bool) {
	if generation > math.MaxInt64 {
		return 0, false
	}
	return int64(generation), true
}

func generationFromFirestore(generation int64) uint64 {
	if generation < 0 {
		return 0
	}
	return uint64(generation)
}
