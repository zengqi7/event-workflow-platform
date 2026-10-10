package redisrepo

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

type Cache struct {
	client *redis.Client
}

func NewCache(client *redis.Client) *Cache {
	return &Cache{client: client}
}

func (c *Cache) AcquireIdempotency(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return c.client.SetNX(ctx, key, "1", ttl).Result()
}

func (c *Cache) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}
