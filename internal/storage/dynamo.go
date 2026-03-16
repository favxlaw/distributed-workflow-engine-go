package storage

import "github.com/aws/aws-sdk-go-v2/service/dynamodb"

type DynamoStore struct {
	Client    *dynamodb.Client
	TableName string
}

func NewDynamoStore(client *dynamodb.Client, tableName string) *DynamoStore {
	return &DynamoStore{Client: client, TableName: tableName}
}
