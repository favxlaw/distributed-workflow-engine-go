package storage

import "os"

// dynamoTestEndpoint returns the DynamoDB endpoint for tests.
// Reads from DYNAMO_ENDPOINT env var, falls back to standard local port.
func dynamoTestEndpoint() string {
	if ep := os.Getenv("DYNAMO_ENDPOINT"); ep != "" {
		return ep
	}
	return "http://localhost:8000"
}
