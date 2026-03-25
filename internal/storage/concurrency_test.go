package storage

import (
	"context"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/favxlaw/distributed-workflow-engine-go/internal/workflow"
)

const concurrencyTestTableName = "orders-concurrent"

func setupConcurrencyStore(t *testing.T) (*DynamoStore, func()) {
	t.Helper()
	ctx := context.Background()

	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("x", "y", "z")),
	)
	if err != nil {
		t.Fatalf("failed to load aws config: %v", err)
	}

	client := dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
		o.BaseEndpoint = aws.String(dynamoTestEndpoint())
	})

	_, err = client.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName: aws.String(concurrencyTestTableName),
		AttributeDefinitions: []types.AttributeDefinition{
			{AttributeName: aws.String("id"), AttributeType: types.ScalarAttributeTypeS},
		},
		KeySchema:   []types.KeySchemaElement{{AttributeName: aws.String("id"), KeyType: types.KeyTypeHash}},
		BillingMode: types.BillingModePayPerRequest,
	})
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	waitForTableActive(t, client, ctx, concurrencyTestTableName)

	store := NewDynamoStore(client, concurrencyTestTableName)
	cleanup := func() {
		_, _ = client.DeleteTable(ctx, &dynamodb.DeleteTableInput{TableName: aws.String(concurrencyTestTableName)})
	}

	return store, cleanup
}

// TestConcurrentTransitionOnlyOneSucceeds proves that optimistic locking works.
// Two goroutines race to transition the same order from Created to Paid.
// Without optimistic locking, both could succeed and data would be corrupted.
// This test verifies that exactly one succeeds and one gets a version conflict,
// and that the final order state is consistent (state=Paid, version=1).
func TestConcurrentTransitionOnlyOneSucceeds(t *testing.T) {
	store, cleanup := setupConcurrencyStore(t)
	defer cleanup()

	ctx := context.Background()
	orderID := "order-concurrent"
	order := &workflow.Order{ID: orderID, State: workflow.Created}
	if err := store.SaveOrder(ctx, order); err != nil {
		t.Fatalf("SaveOrder failed: %v", err)
	}

	// Collect errors from goroutines.
	errChan := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	// Launch two goroutines attempting to transition simultaneously.
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			// Each goroutine gets its own copy for the transition.
			fetchedOrder, err := store.GetOrder(ctx, orderID)
			if err != nil {
				errChan <- err
				return
			}
			err = store.TransitionOrder(ctx, fetchedOrder, workflow.Paid)
			errChan <- err
		}()
	}

	wg.Wait()
	close(errChan)

	// Verify error distribution.
	var successCount int
	var conflictCount int
	for err := range errChan {
		if err == nil {
			successCount++
		} else if _, ok := err.(ErrVersionConflict); ok {
			conflictCount++
		} else {
			t.Errorf("unexpected error type: %T: %v", err, err)
		}
	}

	if successCount != 1 {
		t.Errorf("expected 1 success, got %d", successCount)
	}
	if conflictCount != 1 {
		t.Errorf("expected 1 version conflict, got %d", conflictCount)
	}

	// Verify final order state is consistent.
	finalOrder, err := store.GetOrder(ctx, orderID)
	if err != nil {
		t.Fatalf("GetOrder failed: %v", err)
	}
	if finalOrder.State != workflow.Paid {
		t.Errorf("expected state Paid, got %s", finalOrder.State)
	}
	if finalOrder.Version != 1 {
		t.Errorf("expected version 1, got %d", finalOrder.Version)
	}
}
