package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"ticketbooking/internal/domain"
	"ticketbooking/internal/service"
)

type CatalogHandler struct {
	svc *service.CatalogService
}

func NewCatalogHandler(svc *service.CatalogService) *CatalogHandler {
	return &CatalogHandler{svc: svc}
}

func (h *CatalogHandler) ListEvents(c *gin.Context) {
	resp, err := h.svc.ListEvents(c.Request.Context(), service.ListEventsInput{
		Q:     c.Query("q"),
		From:  c.Query("from"),
		To:    c.Query("to"),
		Page:  c.Query("page"),
		Limit: c.Query("limit"),
	})
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *CatalogHandler) GetEvent(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		RespondError(c, domain.NewError(domain.ReasonValidationFailed, "invalid event id"))
		return
	}
	resp, err := h.svc.GetEvent(c.Request.Context(), id)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *CatalogHandler) GetShowtimeSeats(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		RespondError(c, domain.NewError(domain.ReasonValidationFailed, "invalid showtime id"))
		return
	}
	resp, err := h.svc.GetShowtimeSeats(c.Request.Context(), id)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}
