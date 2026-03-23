package events

import "github.com/aws/aws-sdk-go-v2/service/dynamodb"

type EventStore struct {
	Client    *dynamodb.Client
	TableName string
}

func NewEventStore(client *dynamodb.Client, tableName string) *EventStore {
	return &EventStore{Client: client, TableName: tableName}
}
