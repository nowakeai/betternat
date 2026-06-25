package gcpcloud

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	compute "google.golang.org/api/compute/v1"

	"github.com/nowakeai/betternat/internal/cloud"
)

func TestReplaceRouteDeletesAndRecreatesTaggedNextHopInstanceRoute(t *testing.T) {
	routes := &fakeRoutes{}
	ops := &fakeOperations{}
	provider := NewFromAPI(testConfig(), routes, ops)

	err := provider.ReplaceRoute(context.Background(), cloud.RouteTarget{
		RouteTableID:    "prod-default-via-gw",
		DestinationCIDR: "0.0.0.0/0",
		Target:          "prod-gw-b",
	})
	if err != nil {
		t.Fatalf("replace route: %v", err)
	}
	if routes.deleted != "prod-default-via-gw" {
		t.Fatalf("route was not deleted first: %#v", routes)
	}
	insert := routes.inserted
	if insert == nil {
		t.Fatal("route was not inserted")
	}
	if insert.Name != "prod-default-via-gw" || insert.DestRange != "0.0.0.0/0" || insert.Priority != 900 {
		t.Fatalf("unexpected route insert: %#v", insert)
	}
	if insert.Network != "projects/test-project/global/networks/test-vpc" {
		t.Fatalf("unexpected network link: %s", insert.Network)
	}
	if insert.NextHopInstance != "projects/test-project/zones/us-west2-a/instances/prod-gw-b" {
		t.Fatalf("unexpected next hop: %s", insert.NextHopInstance)
	}
	if len(insert.Tags) != 1 || insert.Tags[0] != "private-client" {
		t.Fatalf("unexpected tags: %#v", insert.Tags)
	}
	if got := strings.Join(ops.waited, ","); got != "delete-op,insert-op" {
		t.Fatalf("unexpected operation waits: %s", got)
	}
}

func TestReplaceRouteIgnoresMissingExistingRoute(t *testing.T) {
	routes := &fakeRoutes{deleteErr: errors.New("googleapi: Error 404: not found")}
	provider := NewFromAPI(testConfig(), routes, &fakeOperations{})

	if err := provider.ReplaceRoute(context.Background(), cloud.RouteTarget{
		RouteTableID:    "prod-default-via-gw",
		DestinationCIDR: "0.0.0.0/0",
		Target:          "prod-gw-a",
	}); err != nil {
		t.Fatalf("replace missing route: %v", err)
	}
	if routes.inserted == nil {
		t.Fatal("replacement route was not inserted")
	}
}

func TestReplaceRouteRestoresPreviousRouteWhenInsertFails(t *testing.T) {
	previous := &compute.Route{
		Name:            "prod-default-via-gw",
		Network:         "projects/test-project/global/networks/test-vpc",
		DestRange:       "0.0.0.0/0",
		Priority:        900,
		Tags:            []string{"private-client"},
		NextHopInstance: "projects/test-project/zones/us-west2-a/instances/prod-gw-a",
	}
	routes := &fakeRoutes{route: previous, insertErrs: []error{errors.New("insert failed"), nil}}
	ops := &fakeOperations{}
	provider := NewFromAPI(testConfig(), routes, ops)

	err := provider.ReplaceRoute(context.Background(), cloud.RouteTarget{
		RouteTableID:    "prod-default-via-gw",
		DestinationCIDR: "0.0.0.0/0",
		Target:          "prod-gw-b",
	})
	if err == nil || !strings.Contains(err.Error(), "previous gcp route") || !strings.Contains(err.Error(), "restored") {
		t.Fatalf("expected restored previous route error, got %v", err)
	}
	if len(routes.inserts) != 2 {
		t.Fatalf("expected replacement and restore inserts, got %d", len(routes.inserts))
	}
	restored := routes.inserts[1]
	if restored.NextHopInstance != previous.NextHopInstance || restored.DestRange != previous.DestRange {
		t.Fatalf("previous route was not restored: %#v", restored)
	}
	if got := strings.Join(ops.waited, ","); got != "delete-op,insert-op" {
		t.Fatalf("unexpected operation waits: %s", got)
	}
}

func TestReplaceRouteRestoresPreviousRouteWhenInsertOperationFails(t *testing.T) {
	previous := &compute.Route{
		Name:            "prod-default-via-gw",
		Network:         "projects/test-project/global/networks/test-vpc",
		DestRange:       "0.0.0.0/0",
		Priority:        900,
		NextHopInstance: "projects/test-project/zones/us-west2-a/instances/prod-gw-a",
	}
	routes := &fakeRoutes{route: previous}
	ops := &fakeOperations{operationErrors: map[string]error{"insert-op": errors.New("operation failed")}}
	provider := NewFromAPI(testConfig(), routes, ops)

	err := provider.ReplaceRoute(context.Background(), cloud.RouteTarget{
		RouteTableID:    "prod-default-via-gw",
		DestinationCIDR: "0.0.0.0/0",
		Target:          "prod-gw-b",
	})
	if err == nil || !strings.Contains(err.Error(), "previous gcp route") || !strings.Contains(err.Error(), "restored") {
		t.Fatalf("expected restored previous route error, got %v", err)
	}
	if len(routes.inserts) != 2 {
		t.Fatalf("expected replacement and restore inserts, got %d", len(routes.inserts))
	}
}

func TestReplaceRouteRestoresPreviousRouteWhenDeleteOperationFails(t *testing.T) {
	previous := &compute.Route{
		Name:            "prod-default-via-gw",
		Network:         "projects/test-project/global/networks/test-vpc",
		DestRange:       "0.0.0.0/0",
		Priority:        900,
		NextHopInstance: "projects/test-project/zones/us-west2-a/instances/prod-gw-a",
	}
	routes := &fakeRoutes{route: previous}
	ops := &fakeOperations{operationErrors: map[string]error{"delete-op": errors.New("delete operation failed")}}
	provider := NewFromAPI(testConfig(), routes, ops)

	err := provider.ReplaceRoute(context.Background(), cloud.RouteTarget{
		RouteTableID:    "prod-default-via-gw",
		DestinationCIDR: "0.0.0.0/0",
		Target:          "prod-gw-b",
	})
	if err == nil || !strings.Contains(err.Error(), "previous gcp route") || !strings.Contains(err.Error(), "restored") {
		t.Fatalf("expected restored previous route error, got %v", err)
	}
	if len(routes.inserts) != 1 {
		t.Fatalf("expected restore insert after failed delete operation, got %d", len(routes.inserts))
	}
	restored := routes.inserts[0]
	if restored.NextHopInstance != previous.NextHopInstance || restored.DestRange != previous.DestRange {
		t.Fatalf("previous route was not restored: %#v", restored)
	}
	if got := strings.Join(ops.waited, ","); got != "delete-op,insert-op" {
		t.Fatalf("unexpected operation waits: %s", got)
	}
}

func TestDescribeRouteReturnsBaseInstanceName(t *testing.T) {
	routes := &fakeRoutes{route: &compute.Route{
		Name:            "prod-default-via-gw",
		DestRange:       "0.0.0.0/0",
		NextHopInstance: "https://www.googleapis.com/compute/v1/projects/test-project/zones/us-west2-a/instances/prod-gw-a",
	}}
	provider := NewFromAPI(testConfig(), routes, &fakeOperations{})

	route, err := provider.DescribeRoute(context.Background(), "prod-default-via-gw", "0.0.0.0/0")
	if err != nil {
		t.Fatalf("describe route: %v", err)
	}
	if route.Target != "prod-gw-a" || route.RouteTableID != "prod-default-via-gw" || route.DestinationCIDR != "0.0.0.0/0" {
		t.Fatalf("unexpected route: %#v", route)
	}
}

func TestDescribeRouteRejectsDestinationMismatch(t *testing.T) {
	routes := &fakeRoutes{route: &compute.Route{Name: "route-a", DestRange: "10.0.0.0/8"}}
	provider := NewFromAPI(testConfig(), routes, &fakeOperations{})

	_, err := provider.DescribeRoute(context.Background(), "route-a", "0.0.0.0/0")
	if err == nil || !strings.Contains(err.Error(), "destination") {
		t.Fatalf("expected destination mismatch, got %v", err)
	}
}

func TestSharedPublicIdentityIsUnsupported(t *testing.T) {
	provider := NewFromAPI(testConfig(), &fakeRoutes{}, &fakeOperations{})

	if _, err := provider.AssociateEIP(context.Background(), "ip", "node"); err == nil {
		t.Fatal("expected AssociateEIP to fail")
	}
	if _, err := provider.DescribePublicIdentity(context.Background(), "ip"); err == nil {
		t.Fatal("expected DescribePublicIdentity to fail")
	}
}

func testConfig() Config {
	return Config{
		ProjectID:         "test-project",
		Zone:              "us-west2-a",
		Network:           "test-vpc",
		ClientTag:         "private-client",
		RoutePriority:     900,
		OperationPollTime: time.Nanosecond,
	}
}

type fakeRoutes struct {
	route      *compute.Route
	inserted   *compute.Route
	inserts    []*compute.Route
	insertErrs []error
	deleted    string
	deleteErr  error
}

func (f *fakeRoutes) Get(context.Context, string, string) (*compute.Route, error) {
	if f.route == nil {
		return nil, errors.New("googleapi: Error 404: not found")
	}
	return f.route, nil
}

func (f *fakeRoutes) Insert(_ context.Context, _ string, route *compute.Route) (*compute.Operation, error) {
	f.inserted = route
	f.inserts = append(f.inserts, route)
	if len(f.insertErrs) > 0 {
		err := f.insertErrs[0]
		f.insertErrs = f.insertErrs[1:]
		if err != nil {
			return nil, err
		}
	}
	return &compute.Operation{Name: "insert-op", Status: "PENDING"}, nil
}

func (f *fakeRoutes) Delete(_ context.Context, _ string, routeName string) (*compute.Operation, error) {
	f.deleted = routeName
	if f.deleteErr != nil {
		return nil, f.deleteErr
	}
	return &compute.Operation{Name: "delete-op", Status: "PENDING"}, nil
}

type fakeOperations struct {
	waited          []string
	operationErrors map[string]error
}

func (f *fakeOperations) Get(_ context.Context, _ string, name string) (*compute.Operation, error) {
	f.waited = append(f.waited, name)
	if err := f.operationErrors[name]; err != nil {
		delete(f.operationErrors, name)
		return &compute.Operation{Name: name, Status: "DONE", Error: &compute.OperationError{
			Errors: []*compute.OperationErrorErrors{{Message: err.Error()}},
		}}, nil
	}
	return &compute.Operation{Name: name, Status: "DONE"}, nil
}
