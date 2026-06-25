package gcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	firestoreapi "google.golang.org/api/firestore/v1"
)

type FirestoreDatabaseInputs struct {
	ProjectID         string
	DatabaseID        string
	LocationID        string
	OperationPollTime time.Duration
}

type FirestoreDatabaseAPI interface {
	GetDatabase(ctx context.Context, name string) (*firestoreapi.GoogleFirestoreAdminV1Database, error)
	CreateDatabase(ctx context.Context, parent string, databaseID string, database *firestoreapi.GoogleFirestoreAdminV1Database) (*firestoreapi.GoogleLongrunningOperation, error)
	DeleteDatabase(ctx context.Context, name string) (*firestoreapi.GoogleLongrunningOperation, error)
	GetOperation(ctx context.Context, name string) (*firestoreapi.GoogleLongrunningOperation, error)
}

type FirestoreDatabaseManager struct {
	API FirestoreDatabaseAPI
}

func NewFirestoreDatabaseAPI(ctx context.Context) (FirestoreDatabaseAPI, error) {
	service, err := firestoreapi.NewService(ctx)
	if err != nil {
		return nil, fmt.Errorf("create GCP Firestore service: %w", err)
	}
	return googleFirestoreDatabaseAPI{service: service}, nil
}

func (m FirestoreDatabaseManager) Apply(ctx context.Context, inputs FirestoreDatabaseInputs) error {
	if err := inputs.validate(); err != nil {
		return err
	}
	if m.API == nil {
		return fmt.Errorf("gcp firestore database api is required")
	}
	name := firestoreDatabaseName(inputs.ProjectID, inputs.DatabaseID)
	if _, err := m.API.GetDatabase(ctx, name); err == nil {
		return nil
	} else if !isGoogleNotFound(err) {
		return fmt.Errorf("get firestore database %q: %w", name, err)
	}
	op, err := m.API.CreateDatabase(ctx, "projects/"+inputs.ProjectID, inputs.DatabaseID, &firestoreapi.GoogleFirestoreAdminV1Database{
		LocationId:               inputs.LocationID,
		Type:                     "FIRESTORE_NATIVE",
		DatabaseEdition:          "STANDARD",
		DeleteProtectionState:    "DELETE_PROTECTION_DISABLED",
		AppEngineIntegrationMode: "DISABLED",
	})
	if err != nil {
		return fmt.Errorf("create firestore database %q: %w", name, err)
	}
	if err := m.waitOperation(ctx, inputs, op); err != nil {
		return fmt.Errorf("wait for firestore database %q create: %w", name, err)
	}
	return nil
}

func (m FirestoreDatabaseManager) Cleanup(ctx context.Context, inputs FirestoreDatabaseInputs) error {
	if err := inputs.validate(); err != nil {
		return err
	}
	if m.API == nil {
		return fmt.Errorf("gcp firestore database api is required")
	}
	name := firestoreDatabaseName(inputs.ProjectID, inputs.DatabaseID)
	op, err := m.API.DeleteDatabase(ctx, name)
	if err != nil {
		if isGoogleNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete firestore database %q: %w", name, err)
	}
	if err := m.waitOperation(ctx, inputs, op); err != nil {
		return fmt.Errorf("wait for firestore database %q delete: %w", name, err)
	}
	return nil
}

func (m FirestoreDatabaseManager) waitOperation(ctx context.Context, inputs FirestoreDatabaseInputs, op *firestoreapi.GoogleLongrunningOperation) error {
	if op == nil || op.Name == "" {
		return nil
	}
	poll := inputs.OperationPollTime
	if poll <= 0 {
		poll = 2 * time.Second
	}
	for {
		current, err := m.API.GetOperation(ctx, op.Name)
		if err != nil {
			return err
		}
		if current == nil {
			return fmt.Errorf("operation %s not found", op.Name)
		}
		if current.Done {
			if current.Error != nil {
				return fmt.Errorf("operation %s failed: %s", op.Name, current.Error.Message)
			}
			return nil
		}
		if err := sleepContext(ctx, poll); err != nil {
			return err
		}
	}
}

func (i FirestoreDatabaseInputs) Validate() error {
	return i.validate()
}

func (i FirestoreDatabaseInputs) validate() error {
	missing := []string{}
	if strings.TrimSpace(i.ProjectID) == "" {
		missing = append(missing, "project_id")
	}
	if strings.TrimSpace(i.DatabaseID) == "" {
		missing = append(missing, "database_id")
	}
	if strings.TrimSpace(i.LocationID) == "" {
		missing = append(missing, "location_id")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required Firestore database inputs: %s", strings.Join(missing, ", "))
	}
	return nil
}

func firestoreDatabaseName(projectID string, databaseID string) string {
	return "projects/" + projectID + "/databases/" + databaseID
}

type googleFirestoreDatabaseAPI struct {
	service *firestoreapi.Service
}

func (a googleFirestoreDatabaseAPI) GetDatabase(ctx context.Context, name string) (*firestoreapi.GoogleFirestoreAdminV1Database, error) {
	return a.service.Projects.Databases.Get(name).Context(ctx).Do()
}

func (a googleFirestoreDatabaseAPI) CreateDatabase(ctx context.Context, parent string, databaseID string, database *firestoreapi.GoogleFirestoreAdminV1Database) (*firestoreapi.GoogleLongrunningOperation, error) {
	return a.service.Projects.Databases.Create(parent, database).DatabaseId(databaseID).Context(ctx).Do()
}

func (a googleFirestoreDatabaseAPI) DeleteDatabase(ctx context.Context, name string) (*firestoreapi.GoogleLongrunningOperation, error) {
	return a.service.Projects.Databases.Delete(name).Context(ctx).Do()
}

func (a googleFirestoreDatabaseAPI) GetOperation(ctx context.Context, name string) (*firestoreapi.GoogleLongrunningOperation, error) {
	return a.service.Projects.Databases.Operations.Get(name).Context(ctx).Do()
}
