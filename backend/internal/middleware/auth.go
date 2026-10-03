package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	"ticketbooking/internal/domain"
)

const (
	CtxUserID = "user_id"
	CtxRole   = "user_role"
)

// TokenVerifier validates a bearer token and returns the user id and role.
type TokenVerifier func(token string) (userID, role string, err error)

func abort(c *gin.Context, code domain.ReasonCode, msg string) {
	c.AbortWithStatusJSON(domain.StatusFor(code), gin.H{"error": gin.H{"code": code, "message": msg}})
}

// Auth requires a valid "Authorization: Bearer <jwt>" header, else 401 UNAUTHENTICATED.
func Auth(verify TokenVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		const prefix = "Bearer "
		if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
			abort(c, domain.ReasonUnauthenticated, "authentication required")
			return
		}
		uid, role, err := verify(strings.TrimSpace(h[len(prefix):]))
		if err != nil {
			abort(c, domain.ReasonUnauthenticated, "invalid or expired token")
			return
		}
		c.Set(CtxUserID, uid)
		c.Set(CtxRole, role)
		c.Next()
	}
}

// RequireAdmin must run after Auth; non-admins get 403 FORBIDDEN.
func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetString(CtxRole) != "admin" {
			abort(c, domain.ReasonForbidden, "admin access required")
			return
		}
		c.Next()
	}
}
