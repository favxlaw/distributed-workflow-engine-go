package events

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const testEventsTable = "processed_events"

func setupTestStore(t *testing.T) (*EventStore, func()) {
	t.Helper()
	ctx := context.Background()

	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("x", "y", "z")),
		awsconfig.WithEndpointResolver(aws.EndpointResolverFunc(func(service, region string) (aws.Endpoint, error) {
			if strings.EqualFold(service, dynamodb.ServiceID) {
				return aws.Endpoint{URL: "http://localhost:8006", SigningRegion: "us-east-1"}, nil
			}
			return aws.Endpoint{}, &aws.EndpointNotFoundError{}
		})),
	)
	if err != nil {
		t.Fatalf("failed to load aws config: %v", err)
	}

	client := dynamodb.NewFromConfig(cfg)

	_, err = client.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName: aws.String(testEventsTable),
		AttributeDefinitions: []types.AttributeDefinition{
			{AttributeName: aws.String("event_id"), AttributeType: types.ScalarAttributeTypeS},
		},
		KeySchema:   []types.KeySchemaElement{{AttributeName: aws.String("event_id"), KeyType: types.KeyTypeHash}},
		BillingMode: types.BillingModePayPerRequest,
	})
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	waitForTableActive(t, client, ctx, testEventsTable)

	_, err = client.UpdateTimeToLive(ctx, &dynamodb.UpdateTimeToLiveInput{
		TableName: aws.String(testEventsTable),
		TimeToLiveSpecification: &types.TimeToLiveSpecification{
			AttributeName: aws.String("expires_at"),
			Enabled:       aws.Bool(true),
		},
	})
	if err != nil {
		t.Fatalf("failed to enable TTL: %v", err)
	}

	store := NewEventStore(client, testEventsTable)

	cleanup := func() {
		_, _ = client.DeleteTable(ctx, &dynamodb.DeleteTableInput{TableName: aws.String(testEventsTable)})
	}

	return store, cleanup
}

func waitForTableActive(t *testing.T, client *dynamodb.Client, ctx context.Context, tableName string) {
	t.Helper()
	for i := 0; i < 20; i++ {
		desc, err := client.DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: aws.String(tableName)})
		if err == nil && desc.Table != nil && desc.Table.TableStatus == types.TableStatusActive {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal("table did not become active")
}

func TestMarkProcessedSucceeds(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	if err := store.MarkProcessed(context.Background(), "event-1"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestMarkProcessedDuplicate(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	if err := store.MarkProcessed(context.Background(), "event-dup"); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	err := store.MarkProcessed(context.Background(), "event-dup")
	if err == nil {
		t.Fatal("expected duplicate error, got nil")
	}
	if _, ok := err.(ErrDuplicateEvent); !ok {
		t.Fatalf("expected ErrDuplicateEvent, got %T: %v", err, err)
	}
}

func TestIsProcessedFalseForUnknown(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	processed, err := store.IsProcessed(context.Background(), "unknown-event")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if processed {
		t.Fatal("expected false for unknown event")
	}
}

func TestIsProcessedTrueAfterMark(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	eventID := "event-check"
	if err := store.MarkProcessed(context.Background(), eventID); err != nil {
		t.Fatalf("MarkProcessed failed: %v", err)
	}

	processed, err := store.IsProcessed(context.Background(), eventID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !processed {
		t.Fatal("expected true after MarkProcessed")
	}
}

func TestConcurrentMarkProcessedOnlyOneSucceeds(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	const eventID = "event-race"
	var wg sync.WaitGroup
	results := make(chan error, 2)

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- store.MarkProcessed(context.Background(), eventID)
		}()
	}

	wg.Wait()
	close(results)

	okCount, dupCount := 0, 0
	for err := range results {
		if err == nil {
			okCount++
			continue
		}
		if _, ok := err.(ErrDuplicateEvent); ok {
			dupCount++
			continue
		}
		t.Fatalf("unexpected error type: %T %v", err, err)
	}

	if okCount != 1 || dupCount != 1 {
		t.Fatalf("expected 1 success and 1 duplicate; got %d success, %d duplicate", okCount, dupCount)
	}
}
