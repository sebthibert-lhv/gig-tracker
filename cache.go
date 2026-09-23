package main

import (
	"context"
	"log"
	"os"

	"github.com/redis/go-redis/v9"
)

func connectRedis() *redis.Client {
	redisURL := os.Getenv("REDIS_URL")

	var opts *redis.Options
	var err error

	if redisURL != "" {
		opts, err = redis.ParseURL(redisURL)
		if err != nil {
			log.Fatalf("failed to parse REDIS_URL: %v", err)
		}
	} else {
		addr := os.Getenv("REDIS_ADDR")
		if addr == "" {
			addr = "localhost:6379"
		}
		opts = &redis.Options{Addr: addr}
	}

	client := redis.NewClient(opts)

	if err := client.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("failed to connect to redis: %v", err)
	}

	return client
}