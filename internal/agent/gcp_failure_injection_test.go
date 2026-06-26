package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nowakeai/betternat/internal/agentapi"
	"github.com/nowakeai/betternat/internal/config"
	"github.com/nowakeai/betternat/internal/ha"
	"github.com/nowakeai/betternat/internal/lease"
)

func TestGCPHandoverUsesFreshLeaseWhenLocalCacheStillLooksActive(t *testing.T) {
	forwarded := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		forwarded = true
		_ = json.NewEncoder(w).Encode(agentapi.HandoverResponse{Status: "forwarded", SourceNodeID: "gce-new-active"})
	}))
	defer server.Close()

	cfg, err := configFromJSON(validGCPHAConfigJSON())
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Local.NodeID = "gce-old-active"
	cfg.Control.PeerAPI.AuthToken = "secret"
	store := &fakeHandoverStore{
		current: lease.Record{
			HAGroupID:       cfg.HAGroupID,
			OwnerInstanceID: "gce-new-active",
			Generation:      11,
			ExpiresAt:       time.Now().Add(time.Minute),
		},
	}
	cache := newControlStatusCache(cfg)
	cache.status.RouteTarget = "gce-old-active"
	cache.status.Cache.Mode = "cached"
	cache.status.Instances = []agentapi.StatusInstance{
		{NodeID: "gce-old-active", Role: "active", Health: "Healthy", Fresh: true},
		{NodeID: "gce-new-active", Role: "active", Health: "Healthy", Fresh: true, ControlURL: server.URL},
	}
	reporter := fakeStatusReporter{snapshot: ha.StatusSnapshot{Lease: lease.Record{
		HAGroupID:       cfg.HAGroupID,
		OwnerInstanceID: "gce-old-active",
		Generation:      10,
		ExpiresAt:       time.Now().Add(time.Minute),
	}}}
	handler := newHandoverHandler(cfg, cache, reporter, store)

	resp := handler(context.Background(), agentapi.HandoverRequest{
		RequestID:    "req-owner-moved",
		TargetNodeID: "auto",
		Reason:       "test",
	})
	if !forwarded {
		t.Fatal("handover should be forwarded to fresh lease owner")
	}
	if resp.Status != "forwarded" {
		t.Fatalf("unexpected response: %#v", resp)
	}
	if len(store.created) != 0 || len(store.updated) != 0 {
		t.Fatalf("stale local active must not create handover records: created=%#v updated=%#v", store.created, store.updated)
	}
}

func configFromJSON(raw string) (config.Config, error) {
	return config.Load(strings.NewReader(raw))
}
