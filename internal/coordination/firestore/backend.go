package firestorecoord

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/nowakeai/betternat/internal/coordination"
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
	_ lease.Manager               = (*Backend)(nil)
	_ lease.Transferer            = (*Backend)(nil)
	_ coordination.AgentRegistry  = (*Backend)(nil)
	_ coordination.AgentReader    = (*Backend)(nil)
	_ coordination.HandoverStore  = (*Backend)(nil)
	_ coordination.HandoverReader = (*Backend)(nil)
	_ coordination.Store          = (*Backend)(nil)
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

func (b *Backend) PutAgent(ctx context.Context, record coordination.AgentRecord, ttl time.Duration) error {
	if err := b.validateBase(); err != nil {
		return err
	}
	doc, err := b.agentDocument(record, ttl)
	if err != nil {
		return err
	}
	if _, err := b.recordsCollection().Doc(agentRecordID(doc.NodeID)).Set(ctx, doc); err != nil {
		return fmt.Errorf("firestore put agent record: %w", err)
	}
	return nil
}

func (b *Backend) DeleteAgent(ctx context.Context, nodeID string) error {
	if err := b.validateBase(); err != nil {
		return err
	}
	if nodeID == "" {
		return fmt.Errorf("agent node id is required")
	}
	if _, err := b.recordsCollection().Doc(agentRecordID(nodeID)).Delete(ctx); err != nil {
		return fmt.Errorf("firestore delete agent record: %w", err)
	}
	return nil
}

func (b *Backend) ListAgents(ctx context.Context) ([]coordination.AgentRecord, error) {
	if err := b.validateBase(); err != nil {
		return nil, err
	}
	iter := b.recordsCollection().Documents(ctx)
	defer iter.Stop()
	now := b.now()
	records := []coordination.AgentRecord{}
	for {
		snap, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("firestore list agent records: %w", err)
		}
		var doc agentDocument
		if err := snap.DataTo(&doc); err != nil {
			return nil, fmt.Errorf("decode firestore agent record %q: %w", snap.Ref.ID, err)
		}
		if doc.RecordType != agentRecordType {
			continue
		}
		record := agentRecordFromDocument(doc)
		if !record.ExpiresAt.IsZero() && record.ExpiresAt.Before(now) {
			continue
		}
		records = append(records, record)
	}
	return records, nil
}

func (b *Backend) CreateHandover(ctx context.Context, record coordination.HandoverRecord, ttl time.Duration) (coordination.HandoverRecord, error) {
	if err := b.validateBase(); err != nil {
		return coordination.HandoverRecord{}, err
	}
	doc, err := b.handoverDocument(record, ttl, true)
	if err != nil {
		return coordination.HandoverRecord{}, err
	}
	if _, err := b.recordsCollection().Doc(handoverRecordID(doc.RequestID)).Create(ctx, doc); err != nil {
		return coordination.HandoverRecord{}, fmt.Errorf("firestore create handover record: %w", err)
	}
	return handoverRecordFromDocument(doc), nil
}

func (b *Backend) UpdateHandover(ctx context.Context, record coordination.HandoverRecord, ttl time.Duration) (coordination.HandoverRecord, error) {
	if err := b.validateBase(); err != nil {
		return coordination.HandoverRecord{}, err
	}
	doc, err := b.handoverDocument(record, ttl, false)
	if err != nil {
		return coordination.HandoverRecord{}, err
	}
	update := map[string]any{
		"record_type":      doc.RecordType,
		"gateway_id":       doc.GatewayID,
		"ha_group_id":      doc.HAGroupID,
		"request_id":       doc.RequestID,
		"status":           doc.Status,
		"target_node_id":   doc.TargetNodeID,
		"lease_generation": doc.LeaseGeneration,
		"message":          doc.Message,
		"error":            doc.Error,
		"updated_at":       doc.UpdatedAt,
		"expires_at":       doc.ExpiresAt,
	}
	if _, err := b.recordsCollection().Doc(handoverRecordID(doc.RequestID)).Set(ctx, update, firestore.MergeAll); err != nil {
		return coordination.HandoverRecord{}, fmt.Errorf("firestore update handover record: %w", err)
	}
	return handoverRecordFromDocument(doc), nil
}

func (b *Backend) GetHandover(ctx context.Context, requestID string) (coordination.HandoverRecord, error) {
	if err := b.validateBase(); err != nil {
		return coordination.HandoverRecord{}, err
	}
	if requestID == "" {
		return coordination.HandoverRecord{}, fmt.Errorf("handover request id is required")
	}
	snap, err := b.recordsCollection().Doc(handoverRecordID(requestID)).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return coordination.HandoverRecord{}, fmt.Errorf("handover request %q not found", requestID)
	}
	if err != nil {
		return coordination.HandoverRecord{}, fmt.Errorf("firestore get handover record: %w", err)
	}
	var doc handoverDocument
	if err := snap.DataTo(&doc); err != nil {
		return coordination.HandoverRecord{}, fmt.Errorf("decode firestore handover record: %w", err)
	}
	record := handoverRecordFromDocument(doc)
	if b.handoverRecordExpired(record) {
		_ = b.deleteHandoverRecord(ctx, requestID)
		return coordination.HandoverRecord{}, fmt.Errorf("handover request %q expired", requestID)
	}
	return record, nil
}

func (b *Backend) ListHandovers(ctx context.Context) ([]coordination.HandoverRecord, error) {
	if err := b.validateBase(); err != nil {
		return nil, err
	}
	iter := b.recordsCollection().Documents(ctx)
	defer iter.Stop()
	records := []coordination.HandoverRecord{}
	for {
		snap, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("firestore list handover records: %w", err)
		}
		var doc handoverDocument
		if err := snap.DataTo(&doc); err != nil {
			return nil, fmt.Errorf("decode firestore handover record %q: %w", snap.Ref.ID, err)
		}
		if doc.RecordType != handoverRecordType {
			continue
		}
		record := handoverRecordFromDocument(doc)
		if b.handoverRecordExpired(record) {
			_ = b.deleteHandoverRecord(ctx, record.RequestID)
			continue
		}
		records = append(records, record)
	}
	return records, nil
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
	return b.recordsCollection().Doc(leaseRecordID)
}

func (b *Backend) recordsCollection() *firestore.CollectionRef {
	return b.client.Collection("gateways").
		Doc(b.gatewayID).
		Collection("ha_groups").
		Doc(b.haGroupID).
		Collection("records")
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

func (b *Backend) handoverRecordExpired(record coordination.HandoverRecord) bool {
	if record.ExpiresAt.IsZero() || record.ExpiresAt.Unix() <= 0 {
		return false
	}
	return record.ExpiresAt.Before(b.now())
}

func (b *Backend) deleteHandoverRecord(ctx context.Context, requestID string) error {
	if requestID == "" {
		return nil
	}
	if _, err := b.recordsCollection().Doc(handoverRecordID(requestID)).Delete(ctx); err != nil {
		return fmt.Errorf("firestore delete handover record: %w", err)
	}
	return nil
}
