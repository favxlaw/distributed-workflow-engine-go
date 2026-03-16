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
		return fmt.Errorf("invalid transition from %s to %s", order.State, newState)
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

func awsString(s string) *string {
	return &s
}
