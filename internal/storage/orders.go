package storage

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/favxlaw/distributed-workflow-engine-go/internal/workflow"
)

func (s *DynamoStore) SaveOrder(ctx context.Context, order *workflow.Order) error {
	if order == nil {
		return fmt.Errorf("order is nil")
	}

	order.Version = 0
	now := time.Now().UTC()
	order.CreatedAt = now
	order.UpdatedAt = now

	item := map[string]types.AttributeValue{
		"id":         &types.AttributeValueMemberS{Value: order.ID},
		"state":      &types.AttributeValueMemberS{Value: string(order.State)},
		"version":    &types.AttributeValueMemberN{Value: strconv.Itoa(order.Version)},
		"created_at": &types.AttributeValueMemberS{Value: order.CreatedAt.Format(time.RFC3339Nano)},
		"updated_at": &types.AttributeValueMemberS{Value: order.UpdatedAt.Format(time.RFC3339Nano)},
	}

	_, err := s.Client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           &s.TableName,
		Item:                item,
		ConditionExpression: awsString("attribute_not_exists(id)"),
	})
	if err != nil {
		var ccfe *types.ConditionalCheckFailedException
		if errors.As(err, &ccfe) {
			return ErrOrderAlreadyExists{ID: order.ID}
		}
		return err
	}

	return nil
}

func (s *DynamoStore) TransitionOrder(ctx context.Context, order *workflow.Order, newState workflow.OrderState) error {
	if order == nil {
		return fmt.Errorf("order is nil")
	}

	if !workflow.IsValidTransition(order.State, newState) {
		return workflow.ErrInvalidTransition{From: order.State, To: newState}
	}

	// Optimistic locking: only update if the current version matches the expected version.
	expectedVersion := order.Version
	newVersion := expectedVersion + 1
	updatedAt := time.Now().UTC().Format(time.RFC3339Nano)

	input := &dynamodb.UpdateItemInput{
		TableName: &s.TableName,
		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: order.ID},
		},
		ExpressionAttributeNames: map[string]string{
			"#s": "state",
			"#v": "version",
			"#u": "updated_at",
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":new_state":        &types.AttributeValueMemberS{Value: string(newState)},
			":expected_version": &types.AttributeValueMemberN{Value: strconv.Itoa(expectedVersion)},
			":one":              &types.AttributeValueMemberN{Value: "1"},
			":updated_at":       &types.AttributeValueMemberS{Value: updatedAt},
		},
		UpdateExpression:    awsString("SET #s = :new_state, #v = #v + :one, #u = :updated_at"),
		ConditionExpression: awsString("#v = :expected_version"),
		ReturnValues:        types.ReturnValueUpdatedNew,
	}

	_, err := s.Client.UpdateItem(ctx, input)
	if err != nil {
		var ccfe *types.ConditionalCheckFailedException
		if errors.As(err, &ccfe) {
			return ErrVersionConflict{ID: order.ID, ExpectedVersion: expectedVersion}
		}
		return err
	}

	order.State = newState
	order.Version = newVersion
	order.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)

	return nil
}

func (s *DynamoStore) GetOrder(ctx context.Context, id string) (*workflow.Order, error) {
	resp, err := s.Client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &s.TableName,
		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: id},
		},
	})
	if err != nil {
		return nil, err
	}

	if len(resp.Item) == 0 {
		return nil, ErrOrderNotFound{ID: id}
	}

	idAttr, ok := resp.Item["id"].(*types.AttributeValueMemberS)
	if !ok {
		return nil, fmt.Errorf("order item missing id")
	}

	stateAttr, ok := resp.Item["state"].(*types.AttributeValueMemberS)
	if !ok {
		return nil, fmt.Errorf("order item missing state")
	}

	versionAttr, ok := resp.Item["version"].(*types.AttributeValueMemberN)
	if !ok {
		return nil, fmt.Errorf("order item missing version")
	}
	version, err := strconv.Atoi(versionAttr.Value)
	if err != nil {
		return nil, err
	}

	createdAtAttr, ok := resp.Item["created_at"].(*types.AttributeValueMemberS)
	if !ok {
		return nil, fmt.Errorf("order item missing created_at")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdAtAttr.Value)
	if err != nil {
		return nil, err
	}

	updatedAtAttr, ok := resp.Item["updated_at"].(*types.AttributeValueMemberS)
	if !ok {
		return nil, fmt.Errorf("order item missing updated_at")
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updatedAtAttr.Value)
	if err != nil {
		return nil, err
	}

	return &workflow.Order{
		ID:        idAttr.Value,
		State:     workflow.OrderState(stateAttr.Value),
		Version:   version,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}

func awsString(s string) *string {
	return &s
}
