package dynamodbcoord

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/nowakeai/betternat/internal/lease"
)

const (
	leaseRecordID   = "lease"
	agentPrefix     = "agent#"
	handoverPrefix  = "handover#"
	defaultNowSlack = 0
)

type API interface {
	UpdateItem(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
	DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
	GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
}

type Backend struct {
	table     string
	haGroupID string
	ttl       time.Duration
	now       lease.Clock
	db        API
}

type AgentRecord struct {
	GatewayID           string
	HAGroupID           string
	NodeID              string
	InstanceID          string
	Cloud               string
	Region              string
	AvailabilityZone    string
	PrivateIP           string
	PublicIP            string
	MetricsURL          string
	ControlURL          string
	Version             string
	Commit              string
	DatapathEngine      string
	DatapathReady       bool
	HAState             string
	LeaseGeneration     uint64
	RouteTargetMatch    bool
	PublicIdentityMatch bool
	UpdatedAt           time.Time
	ExpiresAt           time.Time
}

type HandoverRecord struct {
	RequestID        string
	HAGroupID        string
	Status           string
	SourceNodeID     string
	TargetNodeID     string
	SourceInstanceID string
	TargetInstanceID string
	Reason           string
	LeaseGeneration  uint64
	Message          string
	Error            string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	ExpiresAt        time.Time
}

func New(ctx context.Context, region string, table string, haGroupID string, ttl time.Duration) (*Backend, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	return NewFromClient(dynamodb.NewFromConfig(cfg), table, haGroupID, ttl, time.Now), nil
}

func NewFromClient(db API, table string, haGroupID string, ttl time.Duration, now lease.Clock) *Backend {
	if now == nil {
		now = time.Now
	}
	return &Backend{db: db, table: table, haGroupID: haGroupID, ttl: ttl, now: now}
}

func (b *Backend) Acquire(ctx context.Context, owner string) (lease.Record, error) {
	if err := b.validate(owner); err != nil {
		return lease.Record{}, err
	}
	now := b.now()
	output, err := b.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:           &b.table,
		Key:                 b.key(leaseRecordID),
		UpdateExpression:    ptr("SET owner_instance_id = :owner, expires_at = :expires, updated_at = :now ADD generation :one"),
		ConditionExpression: ptr("attribute_not_exists(record_id) OR expires_at < :now OR owner_instance_id = :owner"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":owner":   s(owner),
			":now":     n(now.Unix()),
			":expires": n(now.Add(b.ttl).Unix()),
			":one":     n(1),
		},
		ReturnValues: types.ReturnValueAllNew,
	})
	if err != nil {
		return lease.Record{}, fmt.Errorf("dynamodb acquire coordination lease: %w", err)
	}
	return leaseRecordFromItem(output.Attributes)
}

func (b *Backend) Renew(ctx context.Context, record lease.Record) (lease.Record, error) {
	if err := b.validate(record.OwnerInstanceID); err != nil {
		return lease.Record{}, err
	}
	now := b.now()
	output, err := b.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:           &b.table,
		Key:                 b.key(leaseRecordID),
		UpdateExpression:    ptr("SET expires_at = :expires, updated_at = :now"),
		ConditionExpression: ptr("owner_instance_id = :owner AND generation = :generation AND expires_at > :now"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":owner":      s(record.OwnerInstanceID),
			":generation": n(int64(record.Generation)),
			":now":        n(now.Unix()),
			":expires":    n(now.Add(b.ttl).Unix()),
		},
		ReturnValues: types.ReturnValueAllNew,
	})
	if err != nil {
		return lease.Record{}, fmt.Errorf("dynamodb renew coordination lease: %w", err)
	}
	return leaseRecordFromItem(output.Attributes)
}

func (b *Backend) Release(ctx context.Context, record lease.Record) error {
	if err := b.validate(record.OwnerInstanceID); err != nil {
		return err
	}
	_, err := b.db.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName:           &b.table,
		Key:                 b.key(leaseRecordID),
		ConditionExpression: ptr("owner_instance_id = :owner AND generation = :generation"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":owner":      s(record.OwnerInstanceID),
			":generation": n(int64(record.Generation)),
		},
	})
	if err != nil {
		return fmt.Errorf("dynamodb release coordination lease: %w", err)
	}
	return nil
}

func (b *Backend) Transfer(ctx context.Context, record lease.Record, newOwner string) (lease.Record, error) {
	if err := b.validate(record.OwnerInstanceID); err != nil {
		return lease.Record{}, err
	}
	if newOwner == "" {
		return lease.Record{}, fmt.Errorf("new lease owner is required")
	}
	now := b.now()
	output, err := b.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:           &b.table,
		Key:                 b.key(leaseRecordID),
		UpdateExpression:    ptr("SET owner_instance_id = :new_owner, expires_at = :expires, updated_at = :now ADD generation :one"),
		ConditionExpression: ptr("owner_instance_id = :owner AND generation = :generation AND expires_at > :now"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":owner":      s(record.OwnerInstanceID),
			":new_owner":  s(newOwner),
			":generation": n(int64(record.Generation)),
			":now":        n(now.Unix()),
			":expires":    n(now.Add(b.ttl).Unix()),
			":one":        n(1),
		},
		ReturnValues: types.ReturnValueAllNew,
	})
	if err != nil {
		return lease.Record{}, fmt.Errorf("dynamodb transfer coordination lease: %w", err)
	}
	return leaseRecordFromItem(output.Attributes)
}

func (b *Backend) Current(ctx context.Context) (lease.Record, error) {
	if b.db == nil {
		return lease.Record{}, fmt.Errorf("dynamodb client is required")
	}
	output, err := b.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &b.table,
		Key:       b.key(leaseRecordID),
	})
	if err != nil {
		return lease.Record{}, fmt.Errorf("dynamodb current coordination lease: %w", err)
	}
	if len(output.Item) == 0 {
		return lease.Record{}, fmt.Errorf("lease is not held")
	}
	return leaseRecordFromItem(output.Item)
}

func (b *Backend) PutAgent(ctx context.Context, record AgentRecord, ttl time.Duration) error {
	if b.db == nil {
		return fmt.Errorf("dynamodb client is required")
	}
	if record.NodeID == "" && record.InstanceID != "" {
		record.NodeID = record.InstanceID
	}
	if record.NodeID == "" {
		return fmt.Errorf("agent node id is required")
	}
	if record.HAGroupID == "" {
		record.HAGroupID = b.haGroupID
	}
	now := b.now()
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = now
	}
	if ttl <= 0 {
		ttl = 20 * time.Second
	}
	if record.ExpiresAt.IsZero() {
		record.ExpiresAt = record.UpdatedAt.Add(ttl)
	}
	_, err := b.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:        &b.table,
		Key:              b.key(agentRecordID(record.NodeID)),
		UpdateExpression: ptr("SET #gateway_id = :gateway, #node_id = :node, #cloud = :cloud, #region = :region, #availability_zone = :az, #private_ip = :private_ip, #public_ip = :public_ip, #metrics_url = :metrics_url, #control_url = :control_url, #version = :version, #commit = :commit, #datapath_engine = :datapath_engine, #datapath_ready = :datapath_ready, #ha_state = :ha_state, #lease_generation = :lease_generation, #route_target_match = :route_target_match, #public_identity_match = :public_identity_match, #updated_at = :updated, #expires_at = :expires REMOVE #instance_id"),
		ExpressionAttributeNames: map[string]string{
			"#gateway_id":            "gateway_id",
			"#node_id":               "node_id",
			"#instance_id":           "instance_id",
			"#cloud":                 "cloud",
			"#region":                "region",
			"#availability_zone":     "availability_zone",
			"#private_ip":            "private_ip",
			"#public_ip":             "public_ip",
			"#metrics_url":           "metrics_url",
			"#control_url":           "control_url",
			"#version":               "version",
			"#commit":                "commit",
			"#datapath_engine":       "datapath_engine",
			"#datapath_ready":        "datapath_ready",
			"#ha_state":              "ha_state",
			"#lease_generation":      "lease_generation",
			"#route_target_match":    "route_target_match",
			"#public_identity_match": "public_identity_match",
			"#updated_at":            "updated_at",
			"#expires_at":            "expires_at",
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":gateway":               s(record.GatewayID),
			":node":                  s(record.NodeID),
			":cloud":                 s(record.Cloud),
			":region":                s(record.Region),
			":az":                    s(record.AvailabilityZone),
			":private_ip":            s(record.PrivateIP),
			":public_ip":             s(record.PublicIP),
			":metrics_url":           s(record.MetricsURL),
			":control_url":           s(record.ControlURL),
			":version":               s(record.Version),
			":commit":                s(record.Commit),
			":datapath_engine":       s(record.DatapathEngine),
			":datapath_ready":        boolAttr(record.DatapathReady),
			":ha_state":              s(record.HAState),
			":lease_generation":      n(int64(record.LeaseGeneration)),
			":route_target_match":    boolAttr(record.RouteTargetMatch),
			":public_identity_match": boolAttr(record.PublicIdentityMatch),
			":updated":               n(record.UpdatedAt.Unix()),
			":expires":               n(record.ExpiresAt.Unix()),
		},
	})
	if err != nil {
		return fmt.Errorf("dynamodb put agent record: %w", err)
	}
	return nil
}

func (b *Backend) DeleteAgent(ctx context.Context, nodeID string) error {
	if b.db == nil {
		return fmt.Errorf("dynamodb client is required")
	}
	if nodeID == "" {
		return fmt.Errorf("agent node id is required")
	}
	_, err := b.db.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: &b.table,
		Key:       b.key(agentRecordID(nodeID)),
	})
	if err != nil {
		return fmt.Errorf("dynamodb delete agent record: %w", err)
	}
	return nil
}

func (b *Backend) ListAgents(ctx context.Context) ([]AgentRecord, error) {
	if b.db == nil {
		return nil, fmt.Errorf("dynamodb client is required")
	}
	output, err := b.db.Query(ctx, &dynamodb.QueryInput{
		TableName:              &b.table,
		KeyConditionExpression: ptr("ha_group_id = :ha_group AND begins_with(record_id, :prefix)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":ha_group": s(b.haGroupID),
			":prefix":   s(agentPrefix),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("dynamodb list agent records: %w", err)
	}
	now := b.now().Add(time.Duration(defaultNowSlack))
	records := make([]AgentRecord, 0, len(output.Items))
	for _, item := range output.Items {
		record := agentRecordFromItem(item)
		if !record.ExpiresAt.IsZero() && record.ExpiresAt.Before(now) {
			continue
		}
		records = append(records, record)
	}
	return records, nil
}

func (b *Backend) CreateHandover(ctx context.Context, record HandoverRecord, ttl time.Duration) (HandoverRecord, error) {
	if b.db == nil {
		return HandoverRecord{}, fmt.Errorf("dynamodb client is required")
	}
	if record.RequestID == "" {
		return HandoverRecord{}, fmt.Errorf("handover request id is required")
	}
	if record.SourceNodeID == "" && record.SourceInstanceID != "" {
		record.SourceNodeID = record.SourceInstanceID
	}
	if record.TargetNodeID == "" && record.TargetInstanceID != "" {
		record.TargetNodeID = record.TargetInstanceID
	}
	if record.SourceNodeID == "" {
		return HandoverRecord{}, fmt.Errorf("handover source node id is required")
	}
	now := b.now()
	if record.HAGroupID == "" {
		record.HAGroupID = b.haGroupID
	}
	if record.Status == "" {
		record.Status = "requested"
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = now
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	if record.ExpiresAt.IsZero() {
		record.ExpiresAt = record.UpdatedAt.Add(ttl)
	}
	_, err := b.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                &b.table,
		Key:                      b.key(handoverRecordID(record.RequestID)),
		UpdateExpression:         ptr("SET #request_id = :request_id, #status = :status, #source_node_id = :source, #target_node_id = :target, #reason = :reason, #lease_generation = :generation, #message = :message, #error = :error, #created_at = :created, #updated_at = :updated, #expires_at = :expires"),
		ConditionExpression:      ptr("attribute_not_exists(record_id)"),
		ExpressionAttributeNames: handoverExpressionNames(),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":request_id": s(record.RequestID),
			":status":     s(record.Status),
			":source":     s(record.SourceNodeID),
			":target":     s(record.TargetNodeID),
			":reason":     s(record.Reason),
			":generation": n(int64(record.LeaseGeneration)),
			":message":    s(record.Message),
			":error":      s(record.Error),
			":created":    n(record.CreatedAt.Unix()),
			":updated":    n(record.UpdatedAt.Unix()),
			":expires":    n(record.ExpiresAt.Unix()),
		},
	})
	if err != nil {
		return HandoverRecord{}, fmt.Errorf("dynamodb create handover record: %w", err)
	}
	return record, nil
}

func (b *Backend) UpdateHandover(ctx context.Context, record HandoverRecord, ttl time.Duration) (HandoverRecord, error) {
	if b.db == nil {
		return HandoverRecord{}, fmt.Errorf("dynamodb client is required")
	}
	if record.RequestID == "" {
		return HandoverRecord{}, fmt.Errorf("handover request id is required")
	}
	if record.TargetNodeID == "" && record.TargetInstanceID != "" {
		record.TargetNodeID = record.TargetInstanceID
	}
	now := b.now()
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = now
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	if record.ExpiresAt.IsZero() {
		record.ExpiresAt = record.UpdatedAt.Add(ttl)
	}
	_, err := b.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                &b.table,
		Key:                      b.key(handoverRecordID(record.RequestID)),
		UpdateExpression:         ptr("SET #status = :status, #target_node_id = :target, #lease_generation = :generation, #message = :message, #error = :error, #updated_at = :updated, #expires_at = :expires"),
		ExpressionAttributeNames: handoverUpdateExpressionNames(),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":status":     s(record.Status),
			":target":     s(record.TargetNodeID),
			":generation": n(int64(record.LeaseGeneration)),
			":message":    s(record.Message),
			":error":      s(record.Error),
			":updated":    n(record.UpdatedAt.Unix()),
			":expires":    n(record.ExpiresAt.Unix()),
		},
	})
	if err != nil {
		return HandoverRecord{}, fmt.Errorf("dynamodb update handover record: %w", err)
	}
	return record, nil
}

func (b *Backend) GetHandover(ctx context.Context, requestID string) (HandoverRecord, error) {
	if b.db == nil {
		return HandoverRecord{}, fmt.Errorf("dynamodb client is required")
	}
	if requestID == "" {
		return HandoverRecord{}, fmt.Errorf("handover request id is required")
	}
	output, err := b.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &b.table,
		Key:       b.key(handoverRecordID(requestID)),
	})
	if err != nil {
		return HandoverRecord{}, fmt.Errorf("dynamodb get handover record: %w", err)
	}
	if len(output.Item) == 0 {
		return HandoverRecord{}, fmt.Errorf("handover request %q not found", requestID)
	}
	return handoverRecordFromItem(output.Item), nil
}

func (b *Backend) ListHandovers(ctx context.Context) ([]HandoverRecord, error) {
	if b.db == nil {
		return nil, fmt.Errorf("dynamodb client is required")
	}
	output, err := b.db.Query(ctx, &dynamodb.QueryInput{
		TableName:              &b.table,
		KeyConditionExpression: ptr("ha_group_id = :ha_group AND begins_with(record_id, :prefix)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":ha_group": s(b.haGroupID),
			":prefix":   s(handoverPrefix),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("dynamodb list handover records: %w", err)
	}
	records := make([]HandoverRecord, 0, len(output.Items))
	for _, item := range output.Items {
		records = append(records, handoverRecordFromItem(item))
	}
	return records, nil
}

func (b *Backend) validate(owner string) error {
	if b.db == nil {
		return fmt.Errorf("dynamodb client is required")
	}
	if b.table == "" {
		return fmt.Errorf("coordination table is required")
	}
	if b.haGroupID == "" {
		return fmt.Errorf("ha group id is required")
	}
	if owner == "" {
		return fmt.Errorf("lease owner is required")
	}
	return nil
}

func (b *Backend) key(recordID string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		"ha_group_id": s(b.haGroupID),
		"record_id":   s(recordID),
	}
}

func agentRecordID(instanceID string) string {
	return agentPrefix + instanceID
}

func handoverRecordID(requestID string) string {
	return handoverPrefix + requestID
}

func leaseRecordFromItem(item map[string]types.AttributeValue) (lease.Record, error) {
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

func agentRecordFromItem(item map[string]types.AttributeValue) AgentRecord {
	updatedAt, _ := number(item, "updated_at")
	expiresAt, _ := number(item, "expires_at")
	generation, _ := number(item, "lease_generation")
	return AgentRecord{
		GatewayID:           stringValue(item, "gateway_id"),
		HAGroupID:           stringValue(item, "ha_group_id"),
		NodeID:              firstStringValue(item, "node_id", "instance_id"),
		InstanceID:          stringValue(item, "instance_id"),
		Cloud:               stringValue(item, "cloud"),
		Region:              stringValue(item, "region"),
		AvailabilityZone:    stringValue(item, "availability_zone"),
		PrivateIP:           stringValue(item, "private_ip"),
		PublicIP:            stringValue(item, "public_ip"),
		MetricsURL:          stringValue(item, "metrics_url"),
		ControlURL:          stringValue(item, "control_url"),
		Version:             stringValue(item, "version"),
		Commit:              stringValue(item, "commit"),
		DatapathEngine:      stringValue(item, "datapath_engine"),
		DatapathReady:       boolValue(item, "datapath_ready"),
		HAState:             stringValue(item, "ha_state"),
		LeaseGeneration:     uint64(generation),
		RouteTargetMatch:    boolValue(item, "route_target_match"),
		PublicIdentityMatch: boolValue(item, "public_identity_match"),
		UpdatedAt:           time.Unix(updatedAt, 0),
		ExpiresAt:           time.Unix(expiresAt, 0),
	}
}

func handoverRecordFromItem(item map[string]types.AttributeValue) HandoverRecord {
	createdAt, _ := number(item, "created_at")
	updatedAt, _ := number(item, "updated_at")
	expiresAt, _ := number(item, "expires_at")
	generation, _ := number(item, "lease_generation")
	return HandoverRecord{
		RequestID:        stringValue(item, "request_id"),
		HAGroupID:        stringValue(item, "ha_group_id"),
		Status:           stringValue(item, "status"),
		SourceNodeID:     firstStringValue(item, "source_node_id", "source_instance_id"),
		TargetNodeID:     firstStringValue(item, "target_node_id", "target_instance_id"),
		SourceInstanceID: stringValue(item, "source_instance_id"),
		TargetInstanceID: stringValue(item, "target_instance_id"),
		Reason:           stringValue(item, "reason"),
		LeaseGeneration:  uint64(generation),
		Message:          stringValue(item, "message"),
		Error:            stringValue(item, "error"),
		CreatedAt:        time.Unix(createdAt, 0),
		UpdatedAt:        time.Unix(updatedAt, 0),
		ExpiresAt:        time.Unix(expiresAt, 0),
	}
}

func handoverExpressionNames() map[string]string {
	return map[string]string{
		"#request_id":       "request_id",
		"#status":           "status",
		"#source_node_id":   "source_node_id",
		"#target_node_id":   "target_node_id",
		"#reason":           "reason",
		"#lease_generation": "lease_generation",
		"#message":          "message",
		"#error":            "error",
		"#created_at":       "created_at",
		"#updated_at":       "updated_at",
		"#expires_at":       "expires_at",
	}
}

func handoverUpdateExpressionNames() map[string]string {
	return map[string]string{
		"#status":           "status",
		"#target_node_id":   "target_node_id",
		"#lease_generation": "lease_generation",
		"#message":          "message",
		"#error":            "error",
		"#updated_at":       "updated_at",
		"#expires_at":       "expires_at",
	}
}

func stringValue(item map[string]types.AttributeValue, key string) string {
	value, ok := item[key].(*types.AttributeValueMemberS)
	if !ok {
		return ""
	}
	return value.Value
}

func firstStringValue(item map[string]types.AttributeValue, keys ...string) string {
	for _, key := range keys {
		if value := stringValue(item, key); value != "" {
			return value
		}
	}
	return ""
}

func boolValue(item map[string]types.AttributeValue, key string) bool {
	value, ok := item[key].(*types.AttributeValueMemberBOOL)
	return ok && value.Value
}

func number(item map[string]types.AttributeValue, key string) (int64, error) {
	value, ok := item[key].(*types.AttributeValueMemberN)
	if !ok {
		return 0, fmt.Errorf("dynamodb coordination field %q is missing or not numeric", key)
	}
	parsed, err := strconv.ParseInt(value.Value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse dynamodb coordination field %q: %w", key, err)
	}
	return parsed, nil
}

func s(value string) types.AttributeValue {
	return &types.AttributeValueMemberS{Value: value}
}

func n(value int64) types.AttributeValue {
	return &types.AttributeValueMemberN{Value: strconv.FormatInt(value, 10)}
}

func boolAttr(value bool) types.AttributeValue {
	return &types.AttributeValueMemberBOOL{Value: value}
}

func ptr(value string) *string {
	return &value
}

func IsAgentRecordID(recordID string) bool {
	return strings.HasPrefix(recordID, agentPrefix)
}

func IsHandoverRecordID(recordID string) bool {
	return strings.HasPrefix(recordID, handoverPrefix)
}
