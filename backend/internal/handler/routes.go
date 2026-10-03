package handler

import (
	"time"

	"github.com/gin-gonic/gin"

	"ticketbooking/internal/middleware"
)

// AuthBookingDeps wires the auth and booking routes (shared by main and the integration tests).
type AuthBookingDeps struct {
	Auth         *AuthHandler
	Booking      *BookingHandler
	Verify       middleware.TokenVerifier
	Limiter      *middleware.RateLimiter
	BookingLimit int // bookings per minute per user
}

func RegisterAuthBookingRoutes(v1 *gin.RouterGroup, d AuthBookingDeps) {
	v1.POST("/auth/register", d.Auth.Register)
	v1.POST("/auth/login", d.Auth.Login)

	authed := v1.Group("", middleware.Auth(d.Verify))
	authed.GET("/me", d.Auth.Me)
	authed.POST("/bookings", d.Limiter.LimitByUser("booking", d.BookingLimit, time.Minute), d.Booking.Create)
	authed.GET("/bookings", d.Booking.List)
	authed.GET("/bookings/:id", d.Booking.Get)
	authed.DELETE("/bookings/:id", d.Booking.Cancel)
}

// PaymentDeps wires the payment, webhook, ticket and (optional) mock gateway routes.
type PaymentDeps struct {
	Payment     *PaymentHandler
	Ticket      *TicketHandler
	MockGateway *MockGatewayHandler // nil when MOCK_GATEWAY_ENABLED is false: the route then 404s
	Verify      middleware.TokenVerifier
}

func RegisterPaymentRoutes(v1 *gin.RouterGroup, d PaymentDeps) {
	v1.POST("/payments/webhook", d.Payment.Webhook)
	if d.MockGateway != nil {
		v1.POST("/mock-gateway/:payment_id/pay", d.MockGateway.Pay)
	}

	authed := v1.Group("", middleware.Auth(d.Verify))
	authed.POST("/bookings/:id/payments", d.Payment.Create)
	authed.GET("/payments/:id", d.Payment.Get)
	authed.GET("/tickets", d.Ticket.List)
	authed.GET("/tickets/:code", d.Ticket.Get)
}
