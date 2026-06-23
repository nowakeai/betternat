package ha

import (
	"sync"
	"time"

	"github.com/nowakeai/betternat/internal/cloud"
	"github.com/nowakeai/betternat/internal/lease"
)

type StatusSnapshot struct {
	State                   State
	Lease                   lease.Record
	LastError               string
	UpdatedAt               time.Time
	TakeoverAttempts        uint64
	TakeoverSuccesses       uint64
	LeaseRenewErrors        uint64
	RouteTargetMatches      bool
	PublicIdentityMatches   bool
	HasRouteTargetCheck     bool
	HasPublicIdentityCheck  bool
	SecondsUntilLeaseExpiry float64
	PublicIdentity          cloud.PublicIdentity
}

type StatusReporter interface {
	Report(StepResult)
	Snapshot() StatusSnapshot
}

type MemoryStatus struct {
	mu       sync.Mutex
	snapshot StatusSnapshot
	now      func() time.Time
}

func NewMemoryStatus() *MemoryStatus {
	return &MemoryStatus{now: time.Now}
}

func (s *MemoryStatus) Report(result StepResult) {
	if s == nil {
		return
	}
	now := s.currentTime()
	s.mu.Lock()
	defer s.mu.Unlock()

	if result.State == StateTakingOver {
		s.snapshot.TakeoverAttempts++
	}
	if result.State == StateActive && result.Activation.Lease.OwnerInstanceID != "" {
		s.snapshot.TakeoverSuccesses++
	}
	if result.State == StateDegraded && result.Err != nil {
		s.snapshot.LeaseRenewErrors++
	}
	s.snapshot.State = result.State
	s.snapshot.Lease = result.Lease
	s.snapshot.UpdatedAt = now
	s.snapshot.LastError = ""
	if result.Err != nil {
		s.snapshot.LastError = result.Err.Error()
	}
	s.snapshot.HasRouteTargetCheck = len(result.Activation.Routes) > 0
	s.snapshot.RouteTargetMatches = s.snapshot.HasRouteTargetCheck && result.Err == nil
	s.snapshot.HasPublicIdentityCheck = result.Activation.PublicIdentity.AllocationID != ""
	s.snapshot.PublicIdentityMatches = s.snapshot.HasPublicIdentityCheck && result.Err == nil
	s.snapshot.PublicIdentity = result.Activation.PublicIdentity
	s.snapshot.SecondsUntilLeaseExpiry = 0
	if !result.Lease.ExpiresAt.IsZero() {
		s.snapshot.SecondsUntilLeaseExpiry = result.Lease.ExpiresAt.Sub(now).Seconds()
	}
}

func (s *MemoryStatus) Snapshot() StatusSnapshot {
	if s == nil {
		return StatusSnapshot{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshot
}

func (s *MemoryStatus) currentTime() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}
