package events

import "os"

func dynamoTestEndpoint() string {
	if ep := os.Getenv("DYNAMO_ENDPOINT"); ep != "" {
		return ep
	}
	return "http://localhost:8000"
}
