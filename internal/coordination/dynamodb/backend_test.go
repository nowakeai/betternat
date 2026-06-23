package dynamodbcoord

import (
	"context"
	"testing"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/nowakeai/betternat/internal/lease"
)

func TestAcquireUsesCoordinationLeaseRecord(t *testing.T) {
	now := time.Unix(100, 0)
	db := &fakeDynamoDB{item: map[string]types.AttributeValue{
		"ha_group_id":       s("ha-a"),
		"record_id":         s("lease"),
		"owner_instance_id": s("i-a"),
		"generation":        n(2),
		"expires_at":        n(110),
		"updated_at":        n(100),
	}}
	backend := NewFromClient(db, "coordination", "ha-a", 10*time.Second, func() time.Time { return now })

	record, err := backend.Acquire(context.Background(), "i-a")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if record.OwnerInstanceID != "i-a" || record.Generation != 2 {
		t.Fatalf("unexpected record: %#v", record)
	}
	if got := db.updateInput.Key["record_id"].(*types.AttributeValueMemberS).Value; got != "lease" {
		t.Fatalf("unexpected record key: %s", got)
	}
}

func TestPutAgentUsesAgentRecord(t *testing.T) {
	now := time.Unix(100, 0)
	db := &fakeDynamoDB{}
	backend := NewFromClient(db, "coordination", "ha-a", 20*time.Second, func() time.Time { return now })

	err := backend.PutAgent(context.Background(), AgentRecord{
		GatewayID:     "prod",
		NodeID:        "node-a",
		PrivateIP:     "10.0.1.10",
		MetricsURL:    "http://10.0.1.10:9108/metrics",
		ControlURL:    "http://10.0.1.10:9109",
		Version:       "v0",
		DatapathReady: true,
		HAState:       "active",
	}, 20*time.Second)
	if err != nil {
		t.Fatalf("put agent: %v", err)
	}
	if got := db.updateInput.Key["record_id"].(*types.AttributeValueMemberS).Value; got != "agent#node-a" {
		t.Fatalf("unexpected record key: %s", got)
	}
	if got := db.updateInput.ExpressionAttributeValues[":expires"].(*types.AttributeValueMemberN).Value; got != "120" {
		t.Fatalf("unexpected expires_at: %s", got)
	}
	if got := db.updateInput.ExpressionAttributeValues[":control_url"].(*types.AttributeValueMemberS).Value; got != "http://10.0.1.10:9109" {
		t.Fatalf("unexpected control url: %s", got)
	}
}

func TestListAgentsFiltersExpiredRecords(t *testing.T) {
	now := time.Unix(100, 0)
	db := &fakeDynamoDB{items: []map[string]types.AttributeValue{
		{
			"ha_group_id":        s("ha-a"),
			"record_id":          s("agent#i-fresh"),
			"node_id":            s("i-fresh"),
			"datapath_ready":     boolAttr(true),
			"updated_at":         n(95),
			"expires_at":         n(120),
			"lease_generation":   n(1),
			"route_target_match": boolAttr(true),
		},
		{
			"ha_group_id":      s("ha-a"),
			"record_id":        s("agent#i-stale"),
			"instance_id":      s("i-stale"),
			"updated_at":       n(80),
			"expires_at":       n(90),
			"lease_generation": n(1),
		},
	}}
	backend := NewFromClient(db, "coordination", "ha-a", 20*time.Second, func() time.Time { return now })

	records, err := backend.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	if len(records) != 1 || records[0].NodeID != "i-fresh" {
		t.Fatalf("unexpected records: %#v", records)
	}
	if awssdk.ToString(db.queryInput.TableName) != "coordination" {
		t.Fatalf("unexpected query input: %#v", db.queryInput)
	}
}

func TestTransferUsesOwnerGenerationFence(t *testing.T) {
	now := time.Unix(100, 0)
	db := &fakeDynamoDB{item: map[string]types.AttributeValue{
		"ha_group_id":       s("ha-a"),
		"record_id":         s("lease"),
		"owner_instance_id": s("i-b"),
		"generation":        n(3),
		"expires_at":        n(110),
		"updated_at":        n(100),
	}}
	backend := NewFromClient(db, "coordination", "ha-a", 10*time.Second, func() time.Time { return now })

	record, err := backend.Transfer(context.Background(), leaseRecord("ha-a", "i-a", 2, 110), "i-b")
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}
	if record.OwnerInstanceID != "i-b" || record.Generation != 3 {
		t.Fatalf("unexpected transferred record: %#v", record)
	}
	if got := db.updateInput.ConditionExpression; got == nil || *got != "owner_instance_id = :owner AND generation = :generation AND expires_at > :now" {
		t.Fatalf("unexpected condition: %#v", db.updateInput.ConditionExpression)
	}
	if got := db.updateInput.ExpressionAttributeValues[":new_owner"].(*types.AttributeValueMemberS).Value; got != "i-b" {
		t.Fatalf("unexpected new owner: %s", got)
	}
}

func TestCreateHandoverUsesConditionalRecord(t *testing.T) {
	now := time.Unix(100, 0)
	db := &fakeDynamoDB{}
	backend := NewFromClient(db, "coordination", "ha-a", 20*time.Second, func() time.Time { return now })

	record, err := backend.CreateHandover(context.Background(), HandoverRecord{
		RequestID:       "req-1",
		SourceNodeID:    "i-active",
		TargetNodeID:    "i-standby",
		Reason:          "test",
		LeaseGeneration: 7,
	}, 60*time.Second)
	if err != nil {
		t.Fatalf("create handover: %v", err)
	}
	if record.Status != "requested" || record.ExpiresAt.Unix() != 160 {
		t.Fatalf("unexpected record: %#v", record)
	}
	if got := db.updateInput.Key["record_id"].(*types.AttributeValueMemberS).Value; got != "handover#req-1" {
		t.Fatalf("unexpected record key: %s", got)
	}
	if got := db.updateInput.ConditionExpression; got == nil || *got != "attribute_not_exists(record_id)" {
		t.Fatalf("unexpected condition: %#v", db.updateInput.ConditionExpression)
	}
}

func TestUpdateHandoverUsesOnlyUpdateExpressionNames(t *testing.T) {
	now := time.Unix(100, 0)
	db := &fakeDynamoDB{}
	backend := NewFromClient(db, "coordination", "ha-a", 20*time.Second, func() time.Time { return now })

	record, err := backend.UpdateHandover(context.Background(), HandoverRecord{
		RequestID:       "req-1",
		Status:          "completed",
		TargetNodeID:    "i-standby",
		LeaseGeneration: 8,
		Message:         "done",
	}, 60*time.Second)
	if err != nil {
		t.Fatalf("update handover: %v", err)
	}
	if record.Status != "completed" || record.ExpiresAt.Unix() != 160 {
		t.Fatalf("unexpected record: %#v", record)
	}
	if got := db.updateInput.Key["record_id"].(*types.AttributeValueMemberS).Value; got != "handover#req-1" {
		t.Fatalf("unexpected record key: %s", got)
	}
	for _, unused := range []string{"#request_id", "#created_at", "#source_node_id", "#source_instance_id", "#reason"} {
		if _, ok := db.updateInput.ExpressionAttributeNames[unused]; ok {
			t.Fatalf("update expression contains unused name %s: %#v", unused, db.updateInput.ExpressionAttributeNames)
		}
	}
}

func TestGetHandoverParsesRecord(t *testing.T) {
	db := &fakeDynamoDB{getItem: map[string]types.AttributeValue{
		"ha_group_id":      s("ha-a"),
		"record_id":        s("handover#req-1"),
		"request_id":       s("req-1"),
		"status":           s("completed"),
		"source_node_id":   s("i-active"),
		"target_node_id":   s("i-standby"),
		"reason":           s("manual"),
		"lease_generation": n(8),
		"message":          s("done"),
		"error":            s(""),
		"created_at":       n(100),
		"updated_at":       n(101),
		"expires_at":       n(200),
	}}
	backend := NewFromClient(db, "coordination", "ha-a", 20*time.Second, nil)

	record, err := backend.GetHandover(context.Background(), "req-1")
	if err != nil {
		t.Fatalf("get handover: %v", err)
	}
	if record.Status != "completed" || record.TargetNodeID != "i-standby" || record.LeaseGeneration != 8 {
		t.Fatalf("unexpected handover record: %#v", record)
	}
}

func TestListHandoversParsesRecords(t *testing.T) {
	db := &fakeDynamoDB{items: []map[string]types.AttributeValue{
		{
			"ha_group_id":      s("ha-a"),
			"record_id":        s("handover#req-1"),
			"request_id":       s("req-1"),
			"status":           s("completed"),
			"source_node_id":   s("i-active"),
			"target_node_id":   s("i-standby"),
			"lease_generation": n(8),
			"created_at":       n(100),
			"updated_at":       n(101),
			"expires_at":       n(200),
		},
	}}
	backend := NewFromClient(db, "coordination", "ha-a", 20*time.Second, nil)

	records, err := backend.ListHandovers(context.Background())
	if err != nil {
		t.Fatalf("list handovers: %v", err)
	}
	if len(records) != 1 || records[0].RequestID != "req-1" || records[0].TargetNodeID != "i-standby" {
		t.Fatalf("unexpected handover records: %#v", records)
	}
	if got := db.queryInput.ExpressionAttributeValues[":prefix"].(*types.AttributeValueMemberS).Value; got != "handover#" {
		t.Fatalf("unexpected query prefix: %s", got)
	}
}

func TestListAgentsReadsLegacyInstanceIDAsNodeID(t *testing.T) {
	now := time.Unix(100, 0)
	db := &fakeDynamoDB{items: []map[string]types.AttributeValue{
		{
			"ha_group_id":      s("ha-a"),
			"record_id":        s("agent#i-legacy"),
			"instance_id":      s("i-legacy"),
			"updated_at":       n(95),
			"expires_at":       n(120),
			"lease_generation": n(1),
		},
	}}
	backend := NewFromClient(db, "coordination", "ha-a", 20*time.Second, func() time.Time { return now })

	records, err := backend.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	if len(records) != 1 || records[0].NodeID != "i-legacy" {
		t.Fatalf("unexpected legacy record normalization: %#v", records)
	}
}

func leaseRecord(group string, owner string, generation uint64, expiresAt int64) lease.Record {
	return lease.Record{
		HAGroupID:       group,
		OwnerInstanceID: owner,
		Generation:      generation,
		ExpiresAt:       time.Unix(expiresAt, 0),
		UpdatedAt:       time.Unix(expiresAt-10, 0),
	}
}

type fakeDynamoDB struct {
	updateInput *dynamodb.UpdateItemInput
	deleteInput *dynamodb.DeleteItemInput
	getItem     map[string]types.AttributeValue
	item        map[string]types.AttributeValue
	items       []map[string]types.AttributeValue
	queryInput  *dynamodb.QueryInput
}

func (f *fakeDynamoDB) UpdateItem(_ context.Context, params *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	f.updateInput = params
	item := f.item
	if item == nil {
		owner, ok := params.ExpressionAttributeValues[":owner"]
		if !ok {
			return &dynamodb.UpdateItemOutput{}, nil
		}
		item = map[string]types.AttributeValue{
			"ha_group_id":       params.Key["ha_group_id"],
			"record_id":         params.Key["record_id"],
			"owner_instance_id": owner,
			"generation":        n(1),
			"expires_at":        params.ExpressionAttributeValues[":expires"],
			"updated_at":        params.ExpressionAttributeValues[":now"],
		}
	}
	return &dynamodb.UpdateItemOutput{Attributes: item}, nil
}

func (f *fakeDynamoDB) DeleteItem(_ context.Context, params *dynamodb.DeleteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	f.deleteInput = params
	return &dynamodb.DeleteItemOutput{}, nil
}

func (f *fakeDynamoDB) GetItem(context.Context, *dynamodb.GetItemInput, ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	return &dynamodb.GetItemOutput{Item: f.getItem}, nil
}

func (f *fakeDynamoDB) Query(_ context.Context, params *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	f.queryInput = params
	return &dynamodb.QueryOutput{Items: f.items}, nil
}
