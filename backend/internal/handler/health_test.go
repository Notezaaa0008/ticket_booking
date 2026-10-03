package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func callHealth(t *testing.T, dbErr, redisErr error) (int, HealthResponse) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/healthz", Health(
		func(ctx context.Context) error { return dbErr },
		func(ctx context.Context) error { return redisErr },
	))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	var res HealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	return w.Code, res
}

func TestHealthAllUp(t *testing.T) {
	code, res := callHealth(t, nil, nil)
	if code != 200 || res.Status != "ok" || res.DB != "ok" || res.Redis != "ok" {
		t.Fatalf("unexpected: %d %+v", code, res)
	}
}

func TestHealthRedisDownIsDegradedButServing(t *testing.T) {
	code, res := callHealth(t, nil, errors.New("redis down"))
	if code != 200 || res.Status != "degraded" || res.Redis != "down" || res.DB != "ok" {
		t.Fatalf("unexpected: %d %+v", code, res)
	}
}

func TestHealthDBDownIs503(t *testing.T) {
	code, res := callHealth(t, errors.New("db down"), nil)
	if code != 503 || res.Status != "degraded" || res.DB != "down" {
		t.Fatalf("unexpected: %d %+v", code, res)
	}
}
