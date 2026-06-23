package dynamodblease

import (
	"context"
	"fmt"
	"strconv"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/nowakeai/betternat/internal/lease"
)

type DynamoDBAPI interface {
	UpdateItem(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
	DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
	GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
}

type Manager struct {
	table     string
	haGroupID string
	ttl       time.Duration
	now       lease.Clock
	db        DynamoDBAPI
}

func New(ctx context.Context, region string, table string, haGroupID string, ttl time.Duration) (*Manager, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	return NewFromClient(dynamodb.NewFromConfig(cfg), table, haGroupID, ttl, time.Now), nil
}

func NewFromClient(db DynamoDBAPI, table string, haGroupID string, ttl time.Duration, now lease.Clock) *Manager {
	if now == nil {
		now = time.Now
	}
	return &Manager{
		table:     table,
		haGroupID: haGroupID,
		ttl:       ttl,
		now:       now,
		db:        db,
	}
}

func (m *Manager) Acquire(ctx context.Context, owner string) (lease.Record, error) {
	if err := m.validate(owner); err != nil {
		return lease.Record{}, err
	}
	now := m.now()
	output, err := m.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:           &m.table,
		Key:                 m.key(),
		UpdateExpression:    ptr("SET owner_instance_id = :owner, expires_at = :expires, updated_at = :now ADD generation :one"),
		ConditionExpression: ptr("attribute_not_exists(ha_group_id) OR expires_at < :now OR owner_instance_id = :owner"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":owner":   s(owner),
			":now":     n(now.Unix()),
			":expires": n(now.Add(m.ttl).Unix()),
			":one":     n(1),
		},
		ReturnValues: types.ReturnValueAllNew,
	})
	if err != nil {
		return lease.Record{}, fmt.Errorf("dynamodb acquire lease: %w", err)
	}
	return recordFromItem(output.Attributes)
}

func (m *Manager) Renew(ctx context.Context, record lease.Record) (lease.Record, error) {
	if err := m.validate(record.OwnerInstanceID); err != nil {
		return lease.Record{}, err
	}
	now := m.now()
	output, err := m.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:           &m.table,
		Key:                 m.key(),
		UpdateExpression:    ptr("SET expires_at = :expires, updated_at = :now"),
		ConditionExpression: ptr("owner_instance_id = :owner AND generation = :generation AND expires_at > :now"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":owner":      s(record.OwnerInstanceID),
			":generation": n(int64(record.Generation)),
			":now":        n(now.Unix()),
			":expires":    n(now.Add(m.ttl).Unix()),
		},
		ReturnValues: types.ReturnValueAllNew,
	})
	if err != nil {
		return lease.Record{}, fmt.Errorf("dynamodb renew lease: %w", err)
	}
	return recordFromItem(output.Attributes)
}

func (m *Manager) Release(ctx context.Context, record lease.Record) error {
	if err := m.validate(record.OwnerInstanceID); err != nil {
		return err
	}
	_, err := m.db.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName:           &m.table,
		Key:                 m.key(),
		ConditionExpression: ptr("owner_instance_id = :owner AND generation = :generation"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":owner":      s(record.OwnerInstanceID),
			":generation": n(int64(record.Generation)),
		},
	})
	if err != nil {
		return fmt.Errorf("dynamodb release lease: %w", err)
	}
	return nil
}

func (m *Manager) Transfer(ctx context.Context, record lease.Record, newOwner string) (lease.Record, error) {
	if err := m.validate(record.OwnerInstanceID); err != nil {
		return lease.Record{}, err
	}
	if newOwner == "" {
		return lease.Record{}, fmt.Errorf("new lease owner is required")
	}
	now := m.now()
	output, err := m.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:           &m.table,
		Key:                 m.key(),
		UpdateExpression:    ptr("SET owner_instance_id = :new_owner, expires_at = :expires, updated_at = :now ADD generation :one"),
		ConditionExpression: ptr("owner_instance_id = :owner AND generation = :generation AND expires_at > :now"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":owner":      s(record.OwnerInstanceID),
			":new_owner":  s(newOwner),
			":generation": n(int64(record.Generation)),
			":now":        n(now.Unix()),
			":expires":    n(now.Add(m.ttl).Unix()),
			":one":        n(1),
		},
		ReturnValues: types.ReturnValueAllNew,
	})
	if err != nil {
		return lease.Record{}, fmt.Errorf("dynamodb transfer lease: %w", err)
	}
	return recordFromItem(output.Attributes)
}

func (m *Manager) Current(ctx context.Context) (lease.Record, error) {
	if m.db == nil {
		return lease.Record{}, fmt.Errorf("dynamodb client is required")
	}
	output, err := m.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &m.table,
		Key:       m.key(),
	})
	if err != nil {
		return lease.Record{}, fmt.Errorf("dynamodb current lease: %w", err)
	}
	if len(output.Item) == 0 {
		return lease.Record{}, fmt.Errorf("lease is not held")
	}
	return recordFromItem(output.Item)
}

func (m *Manager) validate(owner string) error {
	if m.db == nil {
		return fmt.Errorf("dynamodb client is required")
	}
	if m.table == "" {
		return fmt.Errorf("dynamodb lease table is required")
	}
	if m.haGroupID == "" {
		return fmt.Errorf("ha group id is required")
	}
	if owner == "" {
		return fmt.Errorf("lease owner is required")
	}
	return nil
}

func (m *Manager) key() map[string]types.AttributeValue {
	return map[string]types.AttributeValue{"ha_group_id": s(m.haGroupID)}
}

func recordFromItem(item map[string]types.AttributeValue) (lease.Record, error) {
	generation, err := number(item, "generation")
	if err != nil {
		return lease.Record{}, err
	}
	expiresAt, err := number(item, "expires_at")
	if err != nil {
		return lease.Record{}, err
	}
	updatedAt, err := number(item, "updated_at")
	if err != nil {
		return lease.Record{}, err
	}
	return lease.Record{
		HAGroupID:       stringValue(item, "ha_group_id"),
		OwnerInstanceID: stringValue(item, "owner_instance_id"),
		Generation:      uint64(generation),
		ExpiresAt:       time.Unix(expiresAt, 0),
		UpdatedAt:       time.Unix(updatedAt, 0),
	}, nil
}

func stringValue(item map[string]types.AttributeValue, key string) string {
	value, ok := item[key].(*types.AttributeValueMemberS)
	if !ok {
		return ""
	}
	return value.Value
}

func number(item map[string]types.AttributeValue, key string) (int64, error) {
	value, ok := item[key].(*types.AttributeValueMemberN)
	if !ok {
		return 0, fmt.Errorf("dynamodb lease field %q is missing or not numeric", key)
	}
	parsed, err := strconv.ParseInt(value.Value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse dynamodb lease field %q: %w", key, err)
	}
	return parsed, nil
}

func s(value string) types.AttributeValue {
	return &types.AttributeValueMemberS{Value: value}
}

func n(value int64) types.AttributeValue {
	return &types.AttributeValueMemberN{Value: strconv.FormatInt(value, 10)}
}

func ptr(value string) *string {
	return &value
}
