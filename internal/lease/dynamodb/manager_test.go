package dynamodblease

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/nowakeai/betternat/internal/lease"
)

func TestAcquireUsesFencedCondition(t *testing.T) {
	now := time.Unix(100, 0)
	db := &fakeDynamoDB{item: item("ha-a", "i-a", 2, 110, 100)}
	manager := NewFromClient(db, "leases", "ha-a", 10*time.Second, func() time.Time { return now })

	record, err := manager.Acquire(context.Background(), "i-a")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if record.Generation != 2 || record.OwnerInstanceID != "i-a" {
		t.Fatalf("unexpected record: %#v", record)
	}
	if got := *db.updateInput.ConditionExpression; got != "attribute_not_exists(ha_group_id) OR expires_at < :now OR owner_instance_id = :owner" {
		t.Fatalf("unexpected condition: %s", got)
	}
	if got := *db.updateInput.UpdateExpression; got != "SET owner_instance_id = :owner, expires_at = :expires, updated_at = :now ADD generation :one" {
		t.Fatalf("unexpected update: %s", got)
	}
}

func TestRenewUsesGenerationFence(t *testing.T) {
	now := time.Unix(100, 0)
	db := &fakeDynamoDB{item: item("ha-a", "i-a", 2, 110, 100)}
	manager := NewFromClient(db, "leases", "ha-a", 10*time.Second, func() time.Time { return now })

	_, err := manager.Renew(context.Background(), mustRecord(item("ha-a", "i-a", 2, 105, 95)))
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	if got := *db.updateInput.ConditionExpression; got != "owner_instance_id = :owner AND generation = :generation AND expires_at > :now" {
		t.Fatalf("unexpected condition: %s", got)
	}
}

func TestReleaseUsesGenerationFence(t *testing.T) {
	db := &fakeDynamoDB{}
	manager := NewFromClient(db, "leases", "ha-a", 10*time.Second, time.Now)

	err := manager.Release(context.Background(), mustRecord(item("ha-a", "i-a", 2, 105, 95)))
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if got := *db.deleteInput.ConditionExpression; got != "owner_instance_id = :owner AND generation = :generation" {
		t.Fatalf("unexpected condition: %s", got)
	}
}

func TestTransferUsesGenerationFence(t *testing.T) {
	now := time.Unix(100, 0)
	db := &fakeDynamoDB{item: item("ha-a", "i-b", 3, 110, 100)}
	manager := NewFromClient(db, "leases", "ha-a", 10*time.Second, func() time.Time { return now })

	record, err := manager.Transfer(context.Background(), mustRecord(item("ha-a", "i-a", 2, 105, 95)), "i-b")
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}
	if record.OwnerInstanceID != "i-b" || record.Generation != 3 {
		t.Fatalf("unexpected transferred record: %#v", record)
	}
	if got := *db.updateInput.ConditionExpression; got != "owner_instance_id = :owner AND generation = :generation AND expires_at > :now" {
		t.Fatalf("unexpected condition: %s", got)
	}
	if got := db.updateInput.ExpressionAttributeValues[":new_owner"].(*types.AttributeValueMemberS).Value; got != "i-b" {
		t.Fatalf("unexpected new owner: %s", got)
	}
}

func TestCurrentReturnsRecord(t *testing.T) {
	db := &fakeDynamoDB{item: item("ha-a", "i-a", 2, 105, 95)}
	manager := NewFromClient(db, "leases", "ha-a", 10*time.Second, time.Now)

	record, err := manager.Current(context.Background())
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if record.HAGroupID != "ha-a" || record.Generation != 2 {
		t.Fatalf("unexpected current record: %#v", record)
	}
}

type fakeDynamoDB struct {
	updateInput *dynamodb.UpdateItemInput
	deleteInput *dynamodb.DeleteItemInput
	item        map[string]types.AttributeValue
}

func (f *fakeDynamoDB) UpdateItem(_ context.Context, params *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	f.updateInput = params
	return &dynamodb.UpdateItemOutput{Attributes: f.item}, nil
}

func (f *fakeDynamoDB) DeleteItem(_ context.Context, params *dynamodb.DeleteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	f.deleteInput = params
	return &dynamodb.DeleteItemOutput{}, nil
}

func (f *fakeDynamoDB) GetItem(_ context.Context, _ *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	return &dynamodb.GetItemOutput{Item: f.item}, nil
}

func item(haGroupID string, owner string, generation int64, expiresAt int64, updatedAt int64) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		"ha_group_id":       s(haGroupID),
		"owner_instance_id": s(owner),
		"generation":        n(generation),
		"expires_at":        n(expiresAt),
		"updated_at":        n(updatedAt),
	}
}

func mustRecord(item map[string]types.AttributeValue) lease.Record {
	record, err := recordFromItem(item)
	if err != nil {
		panic(err)
	}
	return record
}
