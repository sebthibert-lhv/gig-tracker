package main

import (
	"context"
	"log"

	"github.com/redis/go-redis/v9"
)

func connectRedis() *redis.Client {
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	if err := client.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("failed to connect to redis: %v", err)
	}

	return client
}