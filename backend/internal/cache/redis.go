// Package cache owns the Redis client (cache, rate limits, job queue backend).
package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Connect opens a Redis client and verifies connectivity.
func Connect(ctx context.Context, url string) (*redis.Client, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse REDIS_URL: %w", err)
	}
	opt.DialTimeout = 3 * time.Second
	opt.ReadTimeout = 2 * time.Second
	opt.WriteTimeout = 2 * time.Second
	c := redis.NewClient(opt)
	pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := c.Ping(pctx).Err(); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return c, nil
}

// Pinger adapts *redis.Client to the readiness-check interface.
type Pinger struct{ C *redis.Client }

func (p Pinger) Ping(ctx context.Context) error { return p.C.Ping(ctx).Err() }
