package firestorecoord

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/nowakeai/betternat/internal/lease"
)

type Backend struct {
	client    *firestore.Client
	gatewayID string
	haGroupID string
	ttl       time.Duration
	now       lease.Clock
}

var (
	_ lease.Manager    = (*Backend)(nil)
	_ lease.Transferer = (*Backend)(nil)
)

func New(ctx context.Context, projectID string, databaseID string, gatewayID string, haGroupID string, ttl time.Duration) (*Backend, error) {
	if projectID == "" {
		return nil, fmt.Errorf("gcp project id is required")
	}
	var (
		client *firestore.Client
		err    error
	)
	if databaseID == "" || databaseID == firestore.DefaultDatabaseID {
		client, err = firestore.NewClient(ctx, projectID)
	} else {
		client, err = firestore.NewClientWithDatabase(ctx, projectID, databaseID)
	}
	if err != nil {
		return nil, fmt.Errorf("create firestore coordination client: %w", err)
	}
	return NewFromClient(client, gatewayID, haGroupID, ttl, time.Now), nil
}

func NewFromClient(client *firestore.Client, gatewayID string, haGroupID string, ttl time.Duration, now lease.Clock) *Backend {
	if now == nil {
		now = time.Now
	}
	return &Backend{
		client:    client,
		gatewayID: gatewayID,
		haGroupID: haGroupID,
		ttl:       ttl,
		now:       now,
	}
}

func (b *Backend) Close() error {
	if b.client == nil {
		return nil
	}
	return b.client.Close()
}

func (b *Backend) Acquire(ctx context.Context, owner string) (lease.Record, error) {
	if err := b.validate(owner); err != nil {
		return lease.Record{}, err
	}
	var out lease.Record
	ref := b.leaseDoc()
	err := b.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		current, exists, err := getLeaseDocument(ctx, tx, ref)
		if err != nil {
			return err
		}
		next, err := acquireLease(current, exists, b.gatewayID, b.haGroupID, owner, b.now(), b.ttl)
		if err != nil {
			return err
		}
		if exists {
			if err := tx.Set(ref, next); err != nil {
				return err
			}
		} else {
			if err := tx.Create(ref, next); err != nil {
				return err
			}
		}
		out = leaseRecordFromDocument(next)
		return nil
	})
	if err != nil {
		return lease.Record{}, fmt.Errorf("firestore acquire coordination lease: %w", err)
	}
	return out, nil
}

func (b *Backend) Renew(ctx context.Context, record lease.Record) (lease.Record, error) {
	if err := b.validate(record.OwnerInstanceID); err != nil {
		return lease.Record{}, err
	}
	var out lease.Record
	ref := b.leaseDoc()
	err := b.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		current, exists, err := getLeaseDocument(ctx, tx, ref)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("lease is not held")
		}
		next, err := renewLease(current, record, b.now(), b.ttl)
		if err != nil {
			return err
		}
		if err := tx.Set(ref, next); err != nil {
			return err
		}
		out = leaseRecordFromDocument(next)
		return nil
	})
	if err != nil {
		return lease.Record{}, fmt.Errorf("firestore renew coordination lease: %w", err)
	}
	return out, nil
}

func (b *Backend) Release(ctx context.Context, record lease.Record) error {
	if err := b.validate(record.OwnerInstanceID); err != nil {
		return err
	}
	ref := b.leaseDoc()
	err := b.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		current, exists, err := getLeaseDocument(ctx, tx, ref)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("lease is not held")
		}
		if err := releaseLease(current, record); err != nil {
			return err
		}
		return tx.Delete(ref)
	})
	if err != nil {
		return fmt.Errorf("firestore release coordination lease: %w", err)
	}
	return nil
}

func (b *Backend) Transfer(ctx context.Context, record lease.Record, newOwner string) (lease.Record, error) {
	if err := b.validate(record.OwnerInstanceID); err != nil {
		return lease.Record{}, err
	}
	var out lease.Record
	ref := b.leaseDoc()
	err := b.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		current, exists, err := getLeaseDocument(ctx, tx, ref)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("lease is not held")
		}
		next, err := transferLease(current, record, newOwner, b.now(), b.ttl)
		if err != nil {
			return err
		}
		if err := tx.Set(ref, next); err != nil {
			return err
		}
		out = leaseRecordFromDocument(next)
		return nil
	})
	if err != nil {
		return lease.Record{}, fmt.Errorf("firestore transfer coordination lease: %w", err)
	}
	return out, nil
}

func (b *Backend) Current(ctx context.Context) (lease.Record, error) {
	if err := b.validateBase(); err != nil {
		return lease.Record{}, err
	}
	snap, err := b.leaseDoc().Get(ctx)
	if status.Code(err) == codes.NotFound {
		return lease.Record{}, fmt.Errorf("lease is not held")
	}
	if err != nil {
		return lease.Record{}, fmt.Errorf("firestore current coordination lease: %w", err)
	}
	var doc leaseDocument
	if err := snap.DataTo(&doc); err != nil {
		return lease.Record{}, fmt.Errorf("decode firestore coordination lease: %w", err)
	}
	return leaseRecordFromDocument(doc), nil
}

func (b *Backend) validate(owner string) error {
	if err := b.validateBase(); err != nil {
		return err
	}
	if owner == "" {
		return fmt.Errorf("lease owner is required")
	}
	return nil
}

func (b *Backend) validateBase() error {
	if b.client == nil {
		return fmt.Errorf("firestore client is required")
	}
	if b.gatewayID == "" {
		return fmt.Errorf("gateway id is required")
	}
	if b.haGroupID == "" {
		return fmt.Errorf("ha group id is required")
	}
	if b.ttl <= 0 {
		return fmt.Errorf("lease ttl must be positive")
	}
	return nil
}

func (b *Backend) leaseDoc() *firestore.DocumentRef {
	return b.client.Collection("gateways").
		Doc(b.gatewayID).
		Collection("ha_groups").
		Doc(b.haGroupID).
		Collection("records").
		Doc(leaseRecordID)
}

func getLeaseDocument(ctx context.Context, tx *firestore.Transaction, ref *firestore.DocumentRef) (leaseDocument, bool, error) {
	snap, err := tx.Get(ref)
	if status.Code(err) == codes.NotFound {
		return leaseDocument{}, false, nil
	}
	if err != nil {
		return leaseDocument{}, false, fmt.Errorf("read firestore coordination lease: %w", err)
	}
	var doc leaseDocument
	if err := snap.DataTo(&doc); err != nil {
		return leaseDocument{}, false, fmt.Errorf("decode firestore coordination lease: %w", err)
	}
	return doc, true, nil
}
