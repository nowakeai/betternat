package firestorecoord

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestIntegrationFirestoreLeaseContention(t *testing.T) {
	projectID := os.Getenv("BETTERNAT_GCP_FIRESTORE_PROJECT")
	databaseID := os.Getenv("BETTERNAT_GCP_FIRESTORE_DATABASE")
	if projectID == "" || databaseID == "" {
		t.Skip("set BETTERNAT_GCP_FIRESTORE_PROJECT and BETTERNAT_GCP_FIRESTORE_DATABASE to run")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	runID := "itest-" + time.Now().UTC().Format("20060102150405")
	active, err := New(ctx, projectID, databaseID, "betternat-itest", runID, 30*time.Second)
	if err != nil {
		t.Fatalf("create active backend: %v", err)
	}
	defer active.Close()
	standby, err := New(ctx, projectID, databaseID, "betternat-itest", runID, 30*time.Second)
	if err != nil {
		t.Fatalf("create standby backend: %v", err)
	}
	defer standby.Close()

	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		_, _ = active.leaseDoc().Delete(cleanupCtx)
	}()

	record, err := active.Acquire(ctx, "gce-a")
	if err != nil {
		t.Fatalf("active acquire: %v", err)
	}
	if record.OwnerInstanceID != "gce-a" || record.Generation != 1 {
		t.Fatalf("unexpected active record: %#v", record)
	}

	_, err = standby.Acquire(ctx, "gce-b")
	if err == nil || !strings.Contains(err.Error(), "lease is held") {
		t.Fatalf("expected standby acquire to fail on held lease, got %v", err)
	}

	transferred, err := active.Transfer(ctx, record, "gce-b")
	if err != nil {
		t.Fatalf("transfer to standby: %v", err)
	}
	if transferred.OwnerInstanceID != "gce-b" || transferred.Generation != 2 {
		t.Fatalf("unexpected transferred record: %#v", transferred)
	}

	_, err = active.Renew(ctx, record)
	if err == nil || !strings.Contains(err.Error(), "fencing") {
		t.Fatalf("expected stale active renew to fail fencing, got %v", err)
	}

	renewed, err := standby.Renew(ctx, transferred)
	if err != nil {
		t.Fatalf("standby renew transferred lease: %v", err)
	}
	if renewed.OwnerInstanceID != "gce-b" || renewed.Generation != 2 {
		t.Fatalf("unexpected renewed record: %#v", renewed)
	}

	if err := standby.Release(ctx, renewed); err != nil {
		t.Fatalf("standby release: %v", err)
	}
	_, err = active.Current(ctx)
	if err == nil || !strings.Contains(err.Error(), "lease is not held") {
		t.Fatalf("expected current to report no lease, got %v", err)
	}
}
