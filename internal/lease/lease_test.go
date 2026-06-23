package lease

import (
	"context"
	"testing"
	"time"
)

func TestMemoryManagerAcquireRenewRelease(t *testing.T) {
	now := time.Unix(100, 0)
	manager := NewMemoryManager("ha-a", 10*time.Second, func() time.Time { return now })

	record, err := manager.Acquire(context.Background(), "i-a")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if record.OwnerInstanceID != "i-a" || record.Generation != 1 {
		t.Fatalf("unexpected record: %#v", record)
	}

	now = time.Unix(105, 0)
	renewed, err := manager.Renew(context.Background(), record)
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	if !renewed.ExpiresAt.Equal(time.Unix(115, 0)) {
		t.Fatalf("unexpected renewed expiry: %s", renewed.ExpiresAt)
	}

	if err := manager.Release(context.Background(), renewed); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := manager.Current(context.Background()); err == nil {
		t.Fatal("expected no current lease after release")
	}
}

func TestMemoryManagerRejectsTakeoverBeforeExpiry(t *testing.T) {
	now := time.Unix(100, 0)
	manager := NewMemoryManager("ha-a", 10*time.Second, func() time.Time { return now })

	if _, err := manager.Acquire(context.Background(), "i-a"); err != nil {
		t.Fatalf("acquire i-a: %v", err)
	}
	if _, err := manager.Acquire(context.Background(), "i-b"); err == nil {
		t.Fatal("expected takeover before expiry to fail")
	}

	now = time.Unix(111, 0)
	record, err := manager.Acquire(context.Background(), "i-b")
	if err != nil {
		t.Fatalf("acquire i-b after expiry: %v", err)
	}
	if record.OwnerInstanceID != "i-b" || record.Generation != 2 {
		t.Fatalf("unexpected takeover record: %#v", record)
	}
}

func TestMemoryManagerFencesStaleRenew(t *testing.T) {
	now := time.Unix(100, 0)
	manager := NewMemoryManager("ha-a", 10*time.Second, func() time.Time { return now })

	stale, err := manager.Acquire(context.Background(), "i-a")
	if err != nil {
		t.Fatalf("acquire i-a: %v", err)
	}
	now = time.Unix(111, 0)
	if _, err := manager.Acquire(context.Background(), "i-b"); err != nil {
		t.Fatalf("acquire i-b: %v", err)
	}
	if _, err := manager.Renew(context.Background(), stale); err == nil {
		t.Fatal("expected stale renew to fail")
	}
}

func TestMemoryManagerTransferMovesOwnerAndFencesOldGeneration(t *testing.T) {
	now := time.Unix(100, 0)
	manager := NewMemoryManager("ha-a", 10*time.Second, func() time.Time { return now })

	record, err := manager.Acquire(context.Background(), "i-a")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	transferred, err := manager.Transfer(context.Background(), record, "i-b")
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}
	if transferred.OwnerInstanceID != "i-b" || transferred.Generation != record.Generation+1 {
		t.Fatalf("unexpected transferred lease: %#v", transferred)
	}
	if _, err := manager.Renew(context.Background(), record); err == nil {
		t.Fatal("expected old generation renew to be fenced")
	}
	current, err := manager.Current(context.Background())
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if current.OwnerInstanceID != "i-b" {
		t.Fatalf("unexpected current owner: %#v", current)
	}
}
