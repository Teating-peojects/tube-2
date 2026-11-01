// Package ratelimit is a Redis fixed-window counter. It is atomic (one Lua
// script), shared by all API instances, and reports how long until the
// window resets so the API can send Retry-After.
package ratelimit

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

type Limiter struct {
	rdb    *redis.Client
	prefix string
}

func New(rdb *redis.Client, prefix string) *Limiter { return &Limiter{rdb: rdb, prefix: prefix} }

// INCR + set the expiry on the first hit, and repair a key that somehow lost its TTL.
var hit = redis.NewScript(`
local c = redis.call('INCR', KEYS[1])
if c == 1 then redis.call('PEXPIRE', KEYS[1], ARGV[1]) end
local t = redis.call('PTTL', KEYS[1])
if t < 0 then redis.call('PEXPIRE', KEYS[1], ARGV[1]); t = tonumber(ARGV[1]) end
return {c, t}
`)

// Hit counts one event and returns the count in the current window and the time left in it.
func (l *Limiter) Hit(ctx context.Context, key string, window time.Duration) (int64, time.Duration, error) {
	res, err := hit.Run(ctx, l.rdb, []string{l.prefix + key}, window.Milliseconds()).Int64Slice()
	if err != nil {
		return 0, 0, err
	}
	return res[0], time.Duration(res[1]) * time.Millisecond, nil
}

// Peek reads the counter without counting (0 when absent).
func (l *Limiter) Peek(ctx context.Context, key string) (int64, time.Duration, error) {
	pipe := l.rdb.Pipeline()
	get := pipe.Get(ctx, l.prefix+key)
	ttl := pipe.PTTL(ctx, l.prefix+key)
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return 0, 0, err
	}
	n, err := get.Int64()
	if err == redis.Nil {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	d := ttl.Val()
	if d < 0 {
		d = 0
	}
	return n, d, nil
}

func (l *Limiter) Reset(ctx context.Context, key string) error {
	return l.rdb.Del(ctx, l.prefix+key).Err()
}
