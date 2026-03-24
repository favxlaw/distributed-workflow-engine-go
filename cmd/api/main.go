package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/favxlaw/distributed-workflow-engine-go/internal/api"
	"github.com/favxlaw/distributed-workflow-engine-go/internal/config"
	"github.com/favxlaw/distributed-workflow-engine-go/internal/events"
	"github.com/favxlaw/distributed-workflow-engine-go/internal/storage"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.DynamoRegion),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Use service-specific BaseEndpoint for local development.
	// When DYNAMO_ENDPOINT is empty, the client points at real AWS automatically.
	var dynamoClient *dynamodb.Client
	if cfg.DynamoEndpoint != "" {
		dynamoClient = dynamodb.NewFromConfig(awsCfg, func(o *dynamodb.Options) {
			o.BaseEndpoint = aws.String(cfg.DynamoEndpoint)
		})
	} else {
		dynamoClient = dynamodb.NewFromConfig(awsCfg)
	}

	store := storage.NewDynamoStore(dynamoClient, cfg.DynamoTable)
	eventsStore := events.NewEventStore(dynamoClient, cfg.EventsTable)

	handler := api.NewHandler(store, eventsStore)
	router := api.NewRouter(handler)

	server := &http.Server{Addr: ":" + cfg.ServerPort, Handler: router}

	log.Printf("server starting on :%s", cfg.ServerPort)

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server failed: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctxShutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctxShutdown); err != nil {
		log.Fatalf("shutdown failed: %v", err)
	}

	log.Println("server stopped")
}
