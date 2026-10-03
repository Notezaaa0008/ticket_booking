package handler

import (
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ticketbooking/internal/middleware"
)

func TestAuth_RegisterAndLogin(t *testing.T) {
	env := newBkEnv(t, bkOpts{})
	reg := map[string]any{"email": "New.User@Example.com", "password": "password123", "name": "New User", "role": "admin"}

	code, m := env.do(http.MethodPost, "/api/v1/auth/register", "", reg)
	require.Equal(t, http.StatusCreated, code)
	user, _ := m["user"].(map[string]any)
	assert.Equal(t, "user", user["role"], "client must not be able to choose a role")
	assert.Equal(t, "new.user@example.com", user["email"])
	assert.NotContains(t, user, "password_hash")
	assert.EqualValues(t, 1, env.count(t, `SELECT COUNT(*) FROM users WHERE email = 'new.user@example.com' AND role = 'user'`))
	assert.EqualValues(t, 0, env.count(t, `SELECT COUNT(*) FROM users WHERE password_hash = 'password123'`))

	code, m = env.do(http.MethodPost, "/api/v1/auth/register", "", reg)
	assert.Equal(t, http.StatusConflict, code)
	assert.Equal(t, "EMAIL_TAKEN", errCode(m))

	for name, body := range map[string]map[string]any{
		"bad email":      {"email": "not-an-email", "password": "password123", "name": "A"},
		"short password": {"email": "a@example.com", "password": "short", "name": "A"},
		"empty name":     {"email": "a@example.com", "password": "password123", "name": " "},
	} {
		code, m = env.do(http.MethodPost, "/api/v1/auth/register", "", body)
		assert.Equal(t, http.StatusBadRequest, code, name)
		assert.Equal(t, "VALIDATION_FAILED", errCode(m), name)
	}

	code, m = env.do(http.MethodPost, "/api/v1/auth/login", "", map[string]any{"email": "new.user@example.com", "password": "password123"})
	require.Equal(t, http.StatusOK, code)
	token := str(m, "token")
	require.NotEmpty(t, token)

	code, m = env.do(http.MethodGet, "/api/v1/me", token, nil)
	require.Equal(t, http.StatusOK, code)
	me, _ := m["user"].(map[string]any)
	assert.Equal(t, "new.user@example.com", me["email"])
	assert.Equal(t, "user", me["role"])
}

func TestAuth_LoginFailuresAreIndistinguishable(t *testing.T) {
	env := newBkEnv(t, bkOpts{})
	code, _ := env.do(http.MethodPost, "/api/v1/auth/register", "", map[string]any{"email": "real@example.com", "password": "password123", "name": "Real"})
	require.Equal(t, http.StatusCreated, code)

	c1, wrongPw := env.do(http.MethodPost, "/api/v1/auth/login", "", map[string]any{"email": "real@example.com", "password": "wrong-password"})
	c2, noUser := env.do(http.MethodPost, "/api/v1/auth/login", "", map[string]any{"email": "ghost@example.com", "password": "wrong-password"})
	assert.Equal(t, http.StatusUnauthorized, c1)
	assert.Equal(t, http.StatusUnauthorized, c2)
	assert.Equal(t, "INVALID_CREDENTIALS", errCode(wrongPw))
	assert.Equal(t, "INVALID_CREDENTIALS", errCode(noUser))
	assert.Equal(t, errMessage(wrongPw), errMessage(noUser))
}

func TestAuth_TokenChecks(t *testing.T) {
	env := newBkEnv(t, bkOpts{})
	uid, userTok := env.newUser(t, "plain@example.com", "user")
	_, adminTok := env.newUser(t, "boss@example.com", "admin")

	code, m := env.do(http.MethodGet, "/api/v1/me", "", nil)
	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Equal(t, "UNAUTHENTICATED", errCode(m))

	code, m = env.do(http.MethodGet, "/api/v1/me", "garbage.token.value", nil)
	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Equal(t, "UNAUTHENTICATED", errCode(m))

	expired, err := env.auth.IssueToken(uid, "user", time.Now().Add(-48*time.Hour))
	require.NoError(t, err)
	code, m = env.do(http.MethodGet, "/api/v1/me", expired, nil)
	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Equal(t, "UNAUTHENTICATED", errCode(m))

	code, _ = env.do(http.MethodGet, "/api/v1/bookings", expired, nil)
	assert.Equal(t, http.StatusUnauthorized, code)

	env.r.GET("/api/v1/admin-probe", middleware.Auth(env.auth.VerifyToken), middleware.RequireAdmin(),
		func(c *gin.Context) { c.Status(http.StatusNoContent) })
	code, m = env.do(http.MethodGet, "/api/v1/admin-probe", userTok, nil)
	assert.Equal(t, http.StatusForbidden, code)
	assert.Equal(t, "FORBIDDEN", errCode(m))
	code, _ = env.do(http.MethodGet, "/api/v1/admin-probe", adminTok, nil)
	assert.Equal(t, http.StatusNoContent, code)
}

func TestAuth_LoginRateLimit(t *testing.T) {
	env := newBkEnv(t, bkOpts{loginLimit: 5})
	body := map[string]any{"email": "brute@example.com", "password": "wrong-password"}
	for i := 0; i < 5; i++ {
		code, _ := env.do(http.MethodPost, "/api/v1/auth/login", "", body)
		assert.Equal(t, http.StatusUnauthorized, code)
	}
	code, m := env.do(http.MethodPost, "/api/v1/auth/login", "", body)
	assert.Equal(t, http.StatusTooManyRequests, code)
	assert.Equal(t, "RATE_LIMITED", errCode(m))

	// A different email has its own counter.
	code, _ = env.do(http.MethodPost, "/api/v1/auth/login", "", map[string]any{"email": "other@example.com", "password": "x"})
	assert.Equal(t, http.StatusUnauthorized, code)
}
