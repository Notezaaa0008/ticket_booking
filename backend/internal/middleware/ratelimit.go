package middleware

import (
	"context"
	"log"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"ticketbooking/internal/domain"
)

// RateLimiter is a Redis fixed-window counter. When Redis is unavailable it fails open
// (requests are allowed) and logs a warning; the database constraints stay the safety net.
type RateLimiter struct {
	rdb *redis.Client
}

func NewRateLimiter(rdb *redis.Client) *RateLimiter { return &RateLimiter{rdb: rdb} }

// Allow counts one hit for key in a fixed window and reports whether it is within limit.
func (l *RateLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) bool {
	if l == nil || l.rdb == nil {
		return true
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	pipe := l.rdb.Pipeline()
	incr := pipe.Incr(ctx, key)
	pipe.ExpireNX(ctx, key, window) // window starts at the first hit and is not extended
	if _, err := pipe.Exec(ctx); err != nil {
		log.Printf("rate limit warning (failing open): %v", err)
		return true
	}
	return incr.Val() <= int64(limit)
}

// LimitByUser limits an authenticated route per user id (must run after Auth).
func (l *RateLimiter) LimitByUser(name string, limit int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !l.Allow(c.Request.Context(), "rl:"+name+":"+c.GetString(CtxUserID), limit, window) {
			abort(c, domain.ReasonRateLimited, "too many requests, try again later")
			return
		}
		c.Next()
	}
}
