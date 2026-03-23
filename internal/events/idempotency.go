package events

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

func (s *EventStore) IsProcessed(ctx context.Context, eventID string) (bool, error) {
	resp, err := s.Client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.TableName),
		Key: map[string]types.AttributeValue{
			"event_id": &types.AttributeValueMemberS{Value: eventID},
		},
	})
	if err != nil {
		return false, err
	}
	if resp.Item == nil || len(resp.Item) == 0 {
		return false, nil
	}
	return true, nil
}

func (s *EventStore) MarkProcessed(ctx context.Context, eventID string) error {
	expiresAt := strconv.FormatInt(time.Now().Add(24*time.Hour).Unix(), 10)

	// Use conditional write to ensure only one concurrent writer for the same event ID succeeds.
	// If two requests race, one will get ConditionalCheckFailedException and is treated as duplicate.
	_, err := s.Client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(s.TableName),
		ConditionExpression: aws.String("attribute_not_exists(event_id)"),
		Item: map[string]types.AttributeValue{
			"event_id":   &types.AttributeValueMemberS{Value: eventID},
			"expires_at": &types.AttributeValueMemberN{Value: expiresAt},
		},
	})
	if err != nil {
		var cfe *types.ConditionalCheckFailedException
		if errors.As(err, &cfe) {
			return ErrDuplicateEvent{EventID: eventID}
		}
		return err
	}

	return nil
}
