package storage

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/favxlaw/distributed-workflow-engine-go/internal/workflow"
)

const testTableName = "orders"

func setupTestStore(t *testing.T) (*DynamoStore, func()) {
	t.Helper()
	ctx := context.Background()

	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("x", "y", "z")),
	)
	if err != nil {
		t.Fatalf("failed to load aws config: %v", err)
	}

	// Point at DynamoDB Local using the modern service-specific BaseEndpoint.
	client := dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
		o.BaseEndpoint = aws.String(dynamoTestEndpoint())
	})

	_, err = client.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName: aws.String(testTableName),
		AttributeDefinitions: []types.AttributeDefinition{
			{AttributeName: aws.String("id"), AttributeType: types.ScalarAttributeTypeS},
		},
		KeySchema:   []types.KeySchemaElement{{AttributeName: aws.String("id"), KeyType: types.KeyTypeHash}},
		BillingMode: types.BillingModePayPerRequest,
	})
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	waitForTableActive(t, client, ctx, testTableName)

	store := NewDynamoStore(client, testTableName)
	cleanup := func() {
		_, _ = client.DeleteTable(ctx, &dynamodb.DeleteTableInput{TableName: aws.String(testTableName)})
	}

	return store, cleanup
}

func waitForTableActive(t *testing.T, client *dynamodb.Client, ctx context.Context, tableName string) {
	for i := 0; i < 20; i++ {
		desc, err := client.DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: aws.String(tableName)})
		if err == nil && desc.Table != nil && desc.Table.TableStatus == types.TableStatusActive {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal("table did not become active")
}

func TestSaveOrderSucceeds(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	order := &workflow.Order{ID: "order1", State: workflow.Created}
	if err := store.SaveOrder(context.Background(), order); err != nil {
		t.Fatalf("SaveOrder failed: %v", err)
	}
}

func TestSaveOrderAlreadyExists(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	order := &workflow.Order{ID: "order1", State: workflow.Created}
	if err := store.SaveOrder(context.Background(), order); err != nil {
		t.Fatalf("initial SaveOrder failed: %v", err)
	}

	order2 := &workflow.Order{ID: "order1", State: workflow.Created}
	err := store.SaveOrder(context.Background(), order2)
	if err == nil {
		t.Fatal("expected ErrOrderAlreadyExists, got nil")
	}
	if _, ok := err.(ErrOrderAlreadyExists); !ok {
		t.Fatalf("expected ErrOrderAlreadyExists, got %T: %v", err, err)
	}
}

func TestTransitionOrderSucceeds(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	order := &workflow.Order{ID: "order1", State: workflow.Created}
	if err := store.SaveOrder(context.Background(), order); err != nil {
		t.Fatalf("SaveOrder failed: %v", err)
	}

	if err := store.TransitionOrder(context.Background(), order, workflow.Paid); err != nil {
		t.Fatalf("TransitionOrder failed: %v", err)
	}

	if order.State != workflow.Paid || order.Version != 1 {
		t.Fatalf("unexpected order after transition: %+v", order)
	}
}

func TestTransitionOrderVersionConflict(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	order := &workflow.Order{ID: "order1", State: workflow.Created}
	if err := store.SaveOrder(context.Background(), order); err != nil {
		t.Fatalf("SaveOrder failed: %v", err)
	}

	ctx := context.Background()
	_, err := store.Client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 aws.String(testTableName),
		Key:                       map[string]types.AttributeValue{"id": &types.AttributeValueMemberS{Value: order.ID}},
		UpdateExpression:          aws.String("SET #v = :v"),
		ExpressionAttributeNames:  map[string]string{"#v": "version"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":v": &types.AttributeValueMemberN{Value: "1"}},
	})
	if err != nil {
		t.Fatalf("manual UpdateItem failed: %v", err)
	}

	err = store.TransitionOrder(ctx, order, workflow.Paid)
	if err == nil {
		t.Fatal("expected version conflict error")
	}
	if _, ok := err.(ErrVersionConflict); !ok {
		t.Fatalf("expected ErrVersionConflict, got %T: %v", err, err)
	}
}

func TestTransitionOrderInvalidTransition(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	order := &workflow.Order{ID: "order1", State: workflow.Created}
	if err := store.SaveOrder(context.Background(), order); err != nil {
		t.Fatalf("SaveOrder failed: %v", err)
	}

	err := store.TransitionOrder(context.Background(), order, workflow.Delivered)
	if err == nil {
		t.Fatal("expected invalid transition error")
	}
}
