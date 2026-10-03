package handler

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"ticketbooking/internal/domain"
	"ticketbooking/internal/middleware"
	"ticketbooking/internal/service"
)

const maxWebhookBody = 64 << 10

type PaymentHandler struct {
	svc *service.PaymentService
}

func NewPaymentHandler(svc *service.PaymentService) *PaymentHandler {
	return &PaymentHandler{svc: svc}
}

func (h *PaymentHandler) Create(c *gin.Context) {
	v, created, err := h.svc.Create(reqCtx(c), c.GetString(middleware.CtxUserID), c.Param("id"), c.GetHeader("Idempotency-Key"))
	if err != nil {
		RespondError(c, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.JSON(status, v)
}

func (h *PaymentHandler) Get(c *gin.Context) {
	v, err := h.svc.Get(reqCtx(c), c.GetString(middleware.CtxUserID), c.Param("id"))
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

// Webhook reads the RAW body first: the signature is verified over these exact bytes.
// A read error (e.g. body too large) leaves a truncated body, which then fails the signature check.
func (h *PaymentHandler) Webhook(c *gin.Context) {
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxWebhookBody))
	if err != nil {
		raw = nil
	}
	res, err := h.svc.HandleWebhook(reqCtx(c), raw, c.GetHeader("X-Signature"))
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

type MockGatewayHandler struct {
	gw *service.MockGateway
}

func NewMockGatewayHandler(gw *service.MockGateway) *MockGatewayHandler {
	return &MockGatewayHandler{gw: gw}
}

type mockPayRequest struct {
	Result string `json:"result"`
}

func (h *MockGatewayHandler) Pay(c *gin.Context) {
	var req mockPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.NewError(domain.ReasonValidationFailed, "invalid request body"))
		return
	}
	res, err := h.gw.Pay(reqCtx(c), c.Param("payment_id"), req.Result)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

type TicketHandler struct {
	svc *service.PaymentService
}

func NewTicketHandler(svc *service.PaymentService) *TicketHandler {
	return &TicketHandler{svc: svc}
}

func (h *TicketHandler) List(c *gin.Context) {
	items, err := h.svc.ListTickets(reqCtx(c), c.GetString(middleware.CtxUserID))
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h *TicketHandler) Get(c *gin.Context) {
	v, err := h.svc.GetTicket(reqCtx(c), c.GetString(middleware.CtxUserID), c.Param("code"))
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}
