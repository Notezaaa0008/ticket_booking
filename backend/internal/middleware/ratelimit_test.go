package middleware

import (
	"bytes"
	"context"
	"log"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRateLimiter_FailsOpenAndLogsWhenRedisDown(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	rdb := redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  200 * time.Millisecond,
		MaxRetries:   -1,
		ReadTimeout:  200 * time.Millisecond,
		WriteTimeout: 200 * time.Millisecond,
	})
	t.Cleanup(func() { _ = rdb.Close() })

	lim := NewRateLimiter(rdb)
	ctx := context.Background()
	require.True(t, lim.Allow(ctx, "rl:failopen:test", 1, time.Minute))
	require.True(t, lim.Allow(ctx, "rl:failopen:test", 1, time.Minute))

	out := buf.String()
	assert.Contains(t, out, "rate limit warning (failing open)")
}

func TestRateLimiter_NilClientFailsOpen(t *testing.T) {
	lim := NewRateLimiter(nil)
	assert.True(t, lim.Allow(context.Background(), "rl:nil", 1, time.Minute))
}
