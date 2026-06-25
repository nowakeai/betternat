package firestorecoord

import (
	"strings"
	"testing"
	"time"

	"github.com/nowakeai/betternat/internal/lease"
)

func TestAcquireCreatesLeaseWhenMissing(t *testing.T) {
	now := time.Unix(100, 0)

	doc, err := acquireLease(leaseDocument{}, false, "gw-a", "ha-a", "i-a", now, 10*time.Second)
	if err != nil {
		t.Fatalf("acquire missing lease: %v", err)
	}
	if doc.GatewayID != "gw-a" || doc.HAGroupID != "ha-a" || doc.OwnerInstanceID != "i-a" {
		t.Fatalf("unexpected document identity: %#v", doc)
	}
	if doc.Generation != 1 || doc.ExpiresAt.Unix() != 110 || doc.UpdatedAt.Unix() != 100 {
		t.Fatalf("unexpected document timing: %#v", doc)
	}
}

func TestAcquireRejectsDifferentUnexpiredOwner(t *testing.T) {
	now := time.Unix(100, 0)
	current := leaseDocument{
		OwnerInstanceID: "i-a",
		Generation:      7,
		ExpiresAt:       time.Unix(120, 0),
	}

	_, err := acquireLease(current, true, "gw-a", "ha-a", "i-b", now, 10*time.Second)
	if err == nil || !strings.Contains(err.Error(), "lease is held") {
		t.Fatalf("expected held lease error, got %v", err)
	}
}

func TestAcquireTakesExpiredLeaseAndBumpsGeneration(t *testing.T) {
	now := time.Unix(130, 0)
	current := leaseDocument{
		GatewayID:       "gw-a",
		HAGroupID:       "ha-a",
		OwnerInstanceID: "i-a",
		Generation:      7,
		ExpiresAt:       time.Unix(120, 0),
	}

	doc, err := acquireLease(current, true, "gw-a", "ha-a", "i-b", now, 10*time.Second)
	if err != nil {
		t.Fatalf("acquire expired lease: %v", err)
	}
	if doc.OwnerInstanceID != "i-b" || doc.Generation != 8 || doc.ExpiresAt.Unix() != 140 {
		t.Fatalf("unexpected acquired lease: %#v", doc)
	}
}

func TestAcquireRejectsDifferentOwnerWithinClockSkewAllowance(t *testing.T) {
	now := time.Unix(121, 0)
	current := leaseDocument{
		GatewayID:       "gw-a",
		HAGroupID:       "ha-a",
		OwnerInstanceID: "i-a",
		Generation:      7,
		ExpiresAt:       time.Unix(120, 0),
	}

	_, err := acquireLease(current, true, "gw-a", "ha-a", "i-b", now, 10*time.Second)
	if err == nil || !strings.Contains(err.Error(), "lease is held") {
		t.Fatalf("expected held lease within skew allowance, got %v", err)
	}
}

func TestRenewRequiresOwnerGenerationFence(t *testing.T) {
	now := time.Unix(100, 0)
	current := leaseDocument{
		OwnerInstanceID: "i-a",
		Generation:      7,
		ExpiresAt:       time.Unix(120, 0),
	}

	_, err := renewLease(current, leaseRecord("ha-a", "i-a", 6, 120), now, 10*time.Second)
	if err == nil || !strings.Contains(err.Error(), "fencing") {
		t.Fatalf("expected fencing error, got %v", err)
	}

	doc, err := renewLease(current, leaseRecord("ha-a", "i-a", 7, 120), now, 10*time.Second)
	if err != nil {
		t.Fatalf("renew fenced lease: %v", err)
	}
	if doc.Generation != 7 || doc.ExpiresAt.Unix() != 110 || doc.UpdatedAt.Unix() != 100 {
		t.Fatalf("unexpected renewed lease: %#v", doc)
	}
}

func TestRenewAllowsCurrentOwnerWithinClockSkewAllowance(t *testing.T) {
	now := time.Unix(120, 0)
	current := leaseDocument{
		OwnerInstanceID: "i-a",
		Generation:      7,
		ExpiresAt:       time.Unix(120, 0),
	}

	doc, err := renewLease(current, leaseRecord("ha-a", "i-a", 7, 120), now, 10*time.Second)
	if err != nil {
		t.Fatalf("renew within skew allowance: %v", err)
	}
	if doc.ExpiresAt.Unix() != 130 || doc.UpdatedAt.Unix() != 120 {
		t.Fatalf("unexpected renewed lease: %#v", doc)
	}
}

func TestRenewRejectsExpiredLeaseBeyondClockSkewAllowance(t *testing.T) {
	now := time.Unix(123, 0)
	current := leaseDocument{
		OwnerInstanceID: "i-a",
		Generation:      7,
		ExpiresAt:       time.Unix(120, 0),
	}

	_, err := renewLease(current, leaseRecord("ha-a", "i-a", 7, 120), now, 10*time.Second)
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expected expired error, got %v", err)
	}
}

func TestTransferRequiresFenceAndMovesOwnership(t *testing.T) {
	now := time.Unix(100, 0)
	current := leaseDocument{
		OwnerInstanceID: "i-a",
		Generation:      7,
		ExpiresAt:       time.Unix(120, 0),
	}

	_, err := transferLease(current, leaseRecord("ha-a", "i-a", 6, 120), "i-b", now, 10*time.Second)
	if err == nil || !strings.Contains(err.Error(), "fencing") {
		t.Fatalf("expected fencing error, got %v", err)
	}

	doc, err := transferLease(current, leaseRecord("ha-a", "i-a", 7, 120), "i-b", now, 10*time.Second)
	if err != nil {
		t.Fatalf("transfer fenced lease: %v", err)
	}
	if doc.OwnerInstanceID != "i-b" || doc.Generation != 8 || doc.ExpiresAt.Unix() != 110 {
		t.Fatalf("unexpected transferred lease: %#v", doc)
	}
}

func TestTransferAllowsCurrentOwnerWithinClockSkewAllowance(t *testing.T) {
	now := time.Unix(121, 0)
	current := leaseDocument{
		OwnerInstanceID: "i-a",
		Generation:      7,
		ExpiresAt:       time.Unix(120, 0),
	}

	doc, err := transferLease(current, leaseRecord("ha-a", "i-a", 7, 120), "i-b", now, 10*time.Second)
	if err != nil {
		t.Fatalf("transfer within skew allowance: %v", err)
	}
	if doc.OwnerInstanceID != "i-b" || doc.Generation != 8 || doc.ExpiresAt.Unix() != 131 {
		t.Fatalf("unexpected transferred lease: %#v", doc)
	}
}

func TestReleaseRequiresFence(t *testing.T) {
	current := leaseDocument{
		OwnerInstanceID: "i-a",
		Generation:      7,
	}

	err := releaseLease(current, leaseRecord("ha-a", "i-b", 7, 120))
	if err == nil || !strings.Contains(err.Error(), "fencing") {
		t.Fatalf("expected fencing error, got %v", err)
	}
	if err := releaseLease(current, leaseRecord("ha-a", "i-a", 7, 120)); err != nil {
		t.Fatalf("release fenced lease: %v", err)
	}
}

func TestLeaseRecordFromDocument(t *testing.T) {
	doc := leaseDocument{
		HAGroupID:       "ha-a",
		OwnerInstanceID: "i-a",
		Generation:      7,
		ExpiresAt:       time.Unix(120, 0),
		UpdatedAt:       time.Unix(100, 0),
	}

	record := leaseRecordFromDocument(doc)
	if record.HAGroupID != "ha-a" || record.OwnerInstanceID != "i-a" || record.Generation != 7 {
		t.Fatalf("unexpected record: %#v", record)
	}
}

func leaseRecord(group string, owner string, generation uint64, expiresAt int64) lease.Record {
	return lease.Record{
		HAGroupID:       group,
		OwnerInstanceID: owner,
		Generation:      generation,
		ExpiresAt:       time.Unix(expiresAt, 0),
	}
}
