package gcp

import (
	"context"
	"testing"

	firestoreapi "google.golang.org/api/firestore/v1"
	"google.golang.org/api/googleapi"
)

func TestFirestoreDatabaseApplyCreatesMissingDatabase(t *testing.T) {
	api := &fakeFirestoreDatabaseAPI{
		getErr: &googleapi.Error{Code: 404, Message: "not found"},
		operations: map[string]*firestoreapi.GoogleLongrunningOperation{
			"operations/create": {Name: "operations/create", Done: true},
		},
	}
	manager := FirestoreDatabaseManager{API: api}

	err := manager.Apply(context.Background(), FirestoreDatabaseInputs{
		ProjectID:  "shared-resources-alt",
		DatabaseID: "(default)",
		LocationID: "nam5",
	})
	if err != nil {
		t.Fatalf("apply firestore database: %v", err)
	}
	if api.createParent != "projects/shared-resources-alt" || api.createDatabaseID != "(default)" {
		t.Fatalf("unexpected create request: parent=%q database_id=%q", api.createParent, api.createDatabaseID)
	}
	if api.createDatabase.LocationId != "nam5" || api.createDatabase.Type != "FIRESTORE_NATIVE" {
		t.Fatalf("unexpected database create body: %#v", api.createDatabase)
	}
}

func TestFirestoreDatabaseApplyIsIdempotent(t *testing.T) {
	api := &fakeFirestoreDatabaseAPI{
		database: &firestoreapi.GoogleFirestoreAdminV1Database{Name: "projects/shared-resources-alt/databases/(default)"},
	}
	manager := FirestoreDatabaseManager{API: api}

	err := manager.Apply(context.Background(), FirestoreDatabaseInputs{
		ProjectID:  "shared-resources-alt",
		DatabaseID: "(default)",
		LocationID: "nam5",
	})
	if err != nil {
		t.Fatalf("apply existing firestore database: %v", err)
	}
	if api.createDatabase != nil {
		t.Fatalf("database should not be created when it exists: %#v", api.createDatabase)
	}
}

func TestFirestoreDatabaseCleanupDeletesDatabase(t *testing.T) {
	api := &fakeFirestoreDatabaseAPI{
		operations: map[string]*firestoreapi.GoogleLongrunningOperation{
			"operations/delete": {Name: "operations/delete", Done: true},
		},
	}
	manager := FirestoreDatabaseManager{API: api}

	err := manager.Cleanup(context.Background(), FirestoreDatabaseInputs{
		ProjectID:  "shared-resources-alt",
		DatabaseID: "(default)",
		LocationID: "nam5",
	})
	if err != nil {
		t.Fatalf("cleanup firestore database: %v", err)
	}
	if api.deletedName != "projects/shared-resources-alt/databases/(default)" {
		t.Fatalf("unexpected deleted database: %s", api.deletedName)
	}
}

func TestFirestoreDatabaseCleanupIgnoresMissingDatabase(t *testing.T) {
	api := &fakeFirestoreDatabaseAPI{deleteErr: &googleapi.Error{Code: 404, Message: "not found"}}
	manager := FirestoreDatabaseManager{API: api}

	err := manager.Cleanup(context.Background(), FirestoreDatabaseInputs{
		ProjectID:  "shared-resources-alt",
		DatabaseID: "(default)",
		LocationID: "nam5",
	})
	if err != nil {
		t.Fatalf("cleanup missing firestore database: %v", err)
	}
}

func TestFirestoreDatabaseOperationFailure(t *testing.T) {
	api := &fakeFirestoreDatabaseAPI{
		getErr: &googleapi.Error{Code: 404, Message: "not found"},
		operations: map[string]*firestoreapi.GoogleLongrunningOperation{
			"operations/create": {
				Name:  "operations/create",
				Done:  true,
				Error: &firestoreapi.Status{Message: "permission denied"},
			},
		},
	}
	manager := FirestoreDatabaseManager{API: api}

	err := manager.Apply(context.Background(), FirestoreDatabaseInputs{
		ProjectID:  "shared-resources-alt",
		DatabaseID: "(default)",
		LocationID: "nam5",
	})
	if err == nil {
		t.Fatal("expected operation failure")
	}
}

type fakeFirestoreDatabaseAPI struct {
	database         *firestoreapi.GoogleFirestoreAdminV1Database
	getErr           error
	createParent     string
	createDatabaseID string
	createDatabase   *firestoreapi.GoogleFirestoreAdminV1Database
	deletedName      string
	deleteErr        error
	operations       map[string]*firestoreapi.GoogleLongrunningOperation
}

func (f *fakeFirestoreDatabaseAPI) GetDatabase(_ context.Context, _ string) (*firestoreapi.GoogleFirestoreAdminV1Database, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.database, nil
}

func (f *fakeFirestoreDatabaseAPI) CreateDatabase(_ context.Context, parent string, databaseID string, database *firestoreapi.GoogleFirestoreAdminV1Database) (*firestoreapi.GoogleLongrunningOperation, error) {
	f.createParent = parent
	f.createDatabaseID = databaseID
	f.createDatabase = database
	return &firestoreapi.GoogleLongrunningOperation{Name: "operations/create"}, nil
}

func (f *fakeFirestoreDatabaseAPI) DeleteDatabase(_ context.Context, name string) (*firestoreapi.GoogleLongrunningOperation, error) {
	f.deletedName = name
	if f.deleteErr != nil {
		return nil, f.deleteErr
	}
	return &firestoreapi.GoogleLongrunningOperation{Name: "operations/delete"}, nil
}

func (f *fakeFirestoreDatabaseAPI) GetOperation(_ context.Context, name string) (*firestoreapi.GoogleLongrunningOperation, error) {
	return f.operations[name], nil
}
