package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// Pinger checks one dependency.
type Pinger func(ctx context.Context) error

type HealthResponse struct {
	Status string `json:"status"` // ok | degraded
	DB     string `json:"db"`     // ok | down
	Redis  string `json:"redis"`  // ok | down
}

// Health returns 200 when the database is up (Redis down => "degraded" but still 200)
// and 503 when the database is down.
func Health(pingDB, pingRedis Pinger) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()

		res := HealthResponse{Status: "ok", DB: "ok", Redis: "ok"}
		code := http.StatusOK
		if err := pingDB(ctx); err != nil {
			res.DB, res.Status, code = "down", "degraded", http.StatusServiceUnavailable
		}
		if err := pingRedis(ctx); err != nil {
			res.Redis, res.Status = "down", "degraded"
		}
		c.JSON(code, res)
	}
}
