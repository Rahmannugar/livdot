package cache

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

func Open(ctx context.Context, address, password string) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     address,
		Password: password,
	})
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping Redis: %w", err)
	}
	return client, nil
}
