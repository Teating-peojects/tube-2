package ratelimit

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func testLimiter(t *testing.T) *Limiter {
	t.Helper()
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("TEST_REDIS_URL not set")
	}
	opt, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	c := redis.NewClient(opt)
	t.Cleanup(func() { c.Close() })
	return New(c, fmt.Sprintf("test:%d:", time.Now().UnixNano()))
}

func TestHitCountsAndExpires(t *testing.T) {
	l, ctx := testLimiter(t), context.Background()
	for i := int64(1); i <= 3; i++ {
		n, ttl, err := l.Hit(ctx, "k", 300*time.Millisecond)
		if err != nil || n != i || ttl <= 0 || ttl > 300*time.Millisecond {
			t.Fatalf("hit %d: n=%d ttl=%v err=%v", i, n, ttl, err)
		}
	}
	if n, _, _ := l.Peek(ctx, "k"); n != 3 {
		t.Fatalf("peek = %d", n)
	}
	time.Sleep(350 * time.Millisecond)
	if n, _, _ := l.Hit(ctx, "k", time.Minute); n != 1 {
		t.Fatalf("window should have reset, n=%d", n)
	}
}

func TestPeekDoesNotCountAndResetClears(t *testing.T) {
	l, ctx := testLimiter(t), context.Background()
	if n, ttl, err := l.Peek(ctx, "none"); n != 0 || ttl != 0 || err != nil {
		t.Fatalf("absent key: %d %v %v", n, ttl, err)
	}
	l.Hit(ctx, "r", time.Minute)
	l.Reset(ctx, "r")
	if n, _, _ := l.Peek(ctx, "r"); n != 0 {
		t.Fatal("reset did not clear")
	}
}

func TestHitIsAtomicUnderConcurrency(t *testing.T) {
	l, ctx := testLimiter(t), context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); l.Hit(ctx, "c", time.Minute) }()
	}
	wg.Wait()
	if n, _, _ := l.Peek(ctx, "c"); n != 200 {
		t.Fatalf("lost updates: %d of 200", n)
	}
}

func TestKeysAreIsolated(t *testing.T) {
	l, ctx := testLimiter(t), context.Background()
	l.Hit(ctx, "a", time.Minute)
	l.Hit(ctx, "a", time.Minute)
	if n, _, _ := l.Hit(ctx, "b", time.Minute); n != 1 {
		t.Fatalf("keys leaked into each other: %d", n)
	}
}
