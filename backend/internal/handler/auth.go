package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ticketbooking/internal/domain"
	"ticketbooking/internal/middleware"
	"ticketbooking/internal/service"
)

type AuthHandler struct {
	svc        *service.AuthService
	limiter    *middleware.RateLimiter
	loginLimit int
}

// NewAuthHandler: loginLimit is the max login attempts per minute per IP+email.
func NewAuthHandler(svc *service.AuthService, limiter *middleware.RateLimiter, loginLimit int) *AuthHandler {
	return &AuthHandler{svc: svc, limiter: limiter, loginLimit: loginLimit}
}

// The request structs have no role field on purpose: clients can never choose a role.
type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *AuthHandler) Register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.NewError(domain.ReasonValidationFailed, "invalid request body"))
		return
	}
	u, err := h.svc.Register(c.Request.Context(), req.Email, req.Password, req.Name)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"user": u})
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.NewError(domain.ReasonValidationFailed, "invalid request body"))
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if len(email) > 254 {
		email = email[:254] // keep Redis keys bounded
	}
	key := "rl:login:" + c.ClientIP() + ":" + email
	if !h.limiter.Allow(c.Request.Context(), key, h.loginLimit, time.Minute) {
		RespondError(c, domain.NewError(domain.ReasonRateLimited, "too many login attempts, try again later"))
		return
	}
	token, u, err := h.svc.Login(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token, "user": u})
}

func (h *AuthHandler) Me(c *gin.Context) {
	u, err := h.svc.Me(c.Request.Context(), c.GetString(middleware.CtxUserID))
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": u})
}
