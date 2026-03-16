package config

import (
	"fmt"
	"os"
)

type Config struct {
	DynamoEndpoint string
	DynamoRegion   string
	DynamoTable    string
	ServerPort     string
}

func LoadConfig() (Config, error) {
	c := Config{
		DynamoEndpoint: os.Getenv("DYNAMO_ENDPOINT"),
		DynamoRegion:   os.Getenv("DYNAMO_REGION"),
		DynamoTable:    os.Getenv("DYNAMO_TABLE"),
		ServerPort:     os.Getenv("SERVER_PORT"),
	}

	if c.DynamoEndpoint == "" || c.DynamoRegion == "" || c.DynamoTable == "" || c.ServerPort == "" {
		return Config{}, fmt.Errorf("missing required config: DYNAMO_ENDPOINT, DYNAMO_REGION, DYNAMO_TABLE, SERVER_PORT")
	}

	return c, nil
}
