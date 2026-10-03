package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"ticketbooking/internal/middleware"
	"ticketbooking/internal/service"
)

type BookingHandler struct {
	svc *service.BookingService
}

func NewBookingHandler(svc *service.BookingService) *BookingHandler {
	return &BookingHandler{svc: svc}
}

// createBookingRequest deliberately has no price or total fields: prices come from the database.
type createBookingRequest struct {
	ShowtimeID string   `json:"showtime_id"`
	SeatIDs    []string `json:"seat_ids"`
}

func reqCtx(c *gin.Context) context.Context {
	return context.WithValue(c.Request.Context(), service.RequestIDKey, c.GetString("request_id"))
}

// respondBookingError adds the conflicting seat ids to SEAT_UNAVAILABLE responses.
func respondBookingError(c *gin.Context, err error) {
	var conflict *service.SeatConflictError
	if errors.As(err, &conflict) {
		c.AbortWithStatusJSON(conflict.Status, gin.H{"error": gin.H{
			"code": conflict.Code, "message": conflict.Message, "seat_ids": conflict.SeatIDs,
		}})
		return
	}
	RespondError(c, err)
}

func (h *BookingHandler) Create(c *gin.Context) {
	userID := c.GetString(middleware.CtxUserID)
	var req createBookingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, h.svc.RejectInvalid(reqCtx(c), userID, "invalid request body"))
		return
	}
	v, err := h.svc.Create(reqCtx(c), userID, service.CreateInput{ShowtimeID: req.ShowtimeID, SeatIDs: req.SeatIDs})
	if err != nil {
		respondBookingError(c, err)
		return
	}
	c.JSON(http.StatusCreated, v)
}

func (h *BookingHandler) List(c *gin.Context) {
	items, err := h.svc.List(reqCtx(c), c.GetString(middleware.CtxUserID))
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h *BookingHandler) Get(c *gin.Context) {
	v, err := h.svc.Get(reqCtx(c), c.GetString(middleware.CtxUserID), c.Param("id"))
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

func (h *BookingHandler) Cancel(c *gin.Context) {
	v, err := h.svc.Cancel(reqCtx(c), c.GetString(middleware.CtxUserID), c.Param("id"))
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}
