package lease

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type Record struct {
	HAGroupID       string
	OwnerInstanceID string
	Generation      uint64
	ExpiresAt       time.Time
	UpdatedAt       time.Time
}

// Manager owns fenced active/standby ownership.
type Manager interface {
	Acquire(ctx context.Context, owner string) (Record, error)
	Renew(ctx context.Context, record Record) (Record, error)
	Release(ctx context.Context, record Record) error
	Current(ctx context.Context) (Record, error)
}

// Transferer moves an unexpired fenced lease from the current owner to a new owner.
type Transferer interface {
	Transfer(ctx context.Context, record Record, newOwner string) (Record, error)
}

type Clock func() time.Time

// MemoryManager is a deterministic in-process lease manager for tests and local dry runs.
type MemoryManager struct {
	mu        sync.Mutex
	haGroupID string
	ttl       time.Duration
	now       Clock
	record    Record
}

func NewMemoryManager(haGroupID string, ttl time.Duration, now Clock) *MemoryManager {
	if now == nil {
		now = time.Now
	}
	return &MemoryManager{
		haGroupID: haGroupID,
		ttl:       ttl,
		now:       now,
	}
}

func (m *MemoryManager) Acquire(_ context.Context, owner string) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if owner == "" {
		return Record{}, fmt.Errorf("lease owner is required")
	}
	now := m.now()
	if m.record.OwnerInstanceID != "" && m.record.OwnerInstanceID != owner && now.Before(m.record.ExpiresAt) {
		return Record{}, fmt.Errorf("lease is held by %q until %s", m.record.OwnerInstanceID, m.record.ExpiresAt.UTC().Format(time.RFC3339))
	}
	m.record = Record{
		HAGroupID:       m.haGroupID,
		OwnerInstanceID: owner,
		Generation:      m.record.Generation + 1,
		ExpiresAt:       now.Add(m.ttl),
		UpdatedAt:       now,
	}
	return m.record, nil
}

func (m *MemoryManager) Renew(_ context.Context, record Record) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !sameOwnerAndGeneration(m.record, record) {
		return Record{}, fmt.Errorf("lease fencing check failed")
	}
	now := m.now()
	if !now.Before(m.record.ExpiresAt) {
		return Record{}, fmt.Errorf("lease expired at %s", m.record.ExpiresAt.UTC().Format(time.RFC3339))
	}
	m.record.ExpiresAt = now.Add(m.ttl)
	m.record.UpdatedAt = now
	return m.record, nil
}

func (m *MemoryManager) Release(_ context.Context, record Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !sameOwnerAndGeneration(m.record, record) {
		return fmt.Errorf("lease fencing check failed")
	}
	m.record = Record{}
	return nil
}

func (m *MemoryManager) Transfer(_ context.Context, record Record, newOwner string) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if newOwner == "" {
		return Record{}, fmt.Errorf("new lease owner is required")
	}
	if !sameOwnerAndGeneration(m.record, record) {
		return Record{}, fmt.Errorf("lease fencing check failed")
	}
	now := m.now()
	if !now.Before(m.record.ExpiresAt) {
		return Record{}, fmt.Errorf("lease expired at %s", m.record.ExpiresAt.UTC().Format(time.RFC3339))
	}
	m.record.OwnerInstanceID = newOwner
	m.record.Generation++
	m.record.ExpiresAt = now.Add(m.ttl)
	m.record.UpdatedAt = now
	return m.record, nil
}

func (m *MemoryManager) Current(_ context.Context) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.record.OwnerInstanceID == "" {
		return Record{}, fmt.Errorf("lease is not held")
	}
	return m.record, nil
}

func sameOwnerAndGeneration(a Record, b Record) bool {
	return a.OwnerInstanceID == b.OwnerInstanceID && a.Generation == b.Generation
}
