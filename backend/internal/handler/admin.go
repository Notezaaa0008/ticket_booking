package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"ticketbooking/internal/domain"
	"ticketbooking/internal/middleware"
	"ticketbooking/internal/service"
)

type AdminHandler struct {
	svc *service.AdminService
}

func NewAdminHandler(svc *service.AdminService) *AdminHandler {
	return &AdminHandler{svc: svc}
}

// AdminDeps wires the /admin routes; every route requires a valid token AND the admin role.
type AdminDeps struct {
	Admin  *AdminHandler
	Verify middleware.TokenVerifier
}

func RegisterAdminRoutes(v1 *gin.RouterGroup, d AdminDeps) {
	a := v1.Group("/admin", middleware.Auth(d.Verify), middleware.RequireAdmin())
	a.GET("/events", d.Admin.ListEvents)
	a.POST("/events", d.Admin.CreateEvent)
	a.PUT("/events/:id", d.Admin.UpdateEvent)
	a.DELETE("/events/:id", d.Admin.DeleteEvent)
	a.POST("/events/:id/showtimes", d.Admin.CreateShowtime)
	a.PUT("/showtimes/:id", d.Admin.UpdateShowtime)
	a.GET("/bookings", d.Admin.ListBookings)
	a.GET("/bookings/:id/timeline", d.Admin.Timeline)
	a.GET("/payments", d.Admin.ListPayments)
	a.POST("/payments/:id/refunds", d.Admin.RequestRefund)
	a.GET("/refunds", d.Admin.ListRefunds)
	a.POST("/refunds/:id/process", d.Admin.ProcessRefund)
	a.POST("/tickets/:code/check-in", d.Admin.CheckIn)
	a.GET("/stats", d.Admin.Stats)
}

func bindJSON(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		RespondError(c, domain.NewError(domain.ReasonValidationFailed, "invalid request body"))
		return false
	}
	return true
}

func pageParam(c *gin.Context) (int, bool) {
	raw := c.DefaultQuery("page", "1")
	p, err := strconv.Atoi(raw)
	if err != nil || p < 1 || p > 100000 {
		RespondError(c, domain.NewError(domain.ReasonValidationFailed, "page must be a positive integer"))
		return 0, false
	}
	return p, true
}

func respond(c *gin.Context, status int, v any, err error) {
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(status, v)
}

func (h *AdminHandler) ListEvents(c *gin.Context) {
	items, err := h.svc.ListEvents(reqCtx(c))
	respond(c, http.StatusOK, gin.H{"items": items}, err)
}

func (h *AdminHandler) CreateEvent(c *gin.Context) {
	var in service.EventInput
	if !bindJSON(c, &in) {
		return
	}
	v, err := h.svc.CreateEvent(reqCtx(c), in)
	respond(c, http.StatusCreated, v, err)
}

func (h *AdminHandler) UpdateEvent(c *gin.Context) {
	var in service.EventInput
	if !bindJSON(c, &in) {
		return
	}
	v, err := h.svc.UpdateEvent(reqCtx(c), c.Param("id"), in)
	respond(c, http.StatusOK, v, err)
}

func (h *AdminHandler) DeleteEvent(c *gin.Context) {
	res, err := h.svc.DeleteEvent(reqCtx(c), c.Param("id"))
	respond(c, http.StatusOK, gin.H{"result": res}, err)
}

func (h *AdminHandler) CreateShowtime(c *gin.Context) {
	var in service.ShowtimeInput
	if !bindJSON(c, &in) {
		return
	}
	v, err := h.svc.CreateShowtime(reqCtx(c), c.Param("id"), in)
	respond(c, http.StatusCreated, v, err)
}

func (h *AdminHandler) UpdateShowtime(c *gin.Context) {
	var in service.ShowtimeUpdate
	if !bindJSON(c, &in) {
		return
	}
	v, err := h.svc.UpdateShowtime(reqCtx(c), c.Param("id"), in)
	respond(c, http.StatusOK, v, err)
}

func (h *AdminHandler) ListBookings(c *gin.Context) {
	page, ok := pageParam(c)
	if !ok {
		return
	}
	v, err := h.svc.ListBookings(reqCtx(c), service.AdminBookingListQuery{
		Status: c.Query("status"), UserID: c.Query("user_id"), Email: c.Query("email"),
		ShowtimeID: c.Query("showtime_id"), Page: page,
	})
	respond(c, http.StatusOK, v, err)
}

func (h *AdminHandler) Timeline(c *gin.Context) {
	v, err := h.svc.Timeline(reqCtx(c), c.Param("id"))
	respond(c, http.StatusOK, v, err)
}

func (h *AdminHandler) ListPayments(c *gin.Context) {
	page, ok := pageParam(c)
	if !ok {
		return
	}
	v, err := h.svc.ListPayments(reqCtx(c), c.Query("status"), page)
	respond(c, http.StatusOK, v, err)
}

func (h *AdminHandler) RequestRefund(c *gin.Context) {
	var in service.RefundRequest
	if !bindJSON(c, &in) {
		return
	}
	v, created, err := h.svc.RequestRefund(reqCtx(c), c.GetString(middleware.CtxUserID), c.Param("id"), c.GetHeader("Idempotency-Key"), in)
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	respond(c, status, v, err)
}

func (h *AdminHandler) ProcessRefund(c *gin.Context) {
	v, err := h.svc.ProcessRefund(reqCtx(c), c.GetString(middleware.CtxUserID), c.Param("id"))
	respond(c, http.StatusOK, v, err)
}

func (h *AdminHandler) ListRefunds(c *gin.Context) {
	page, ok := pageParam(c)
	if !ok {
		return
	}
	v, err := h.svc.ListRefunds(reqCtx(c), c.Query("status"), page)
	respond(c, http.StatusOK, v, err)
}

func (h *AdminHandler) CheckIn(c *gin.Context) {
	v, err := h.svc.CheckIn(reqCtx(c), c.Param("code"))
	respond(c, http.StatusOK, v, err)
}

func (h *AdminHandler) Stats(c *gin.Context) {
	v, err := h.svc.Stats(reqCtx(c))
	respond(c, http.StatusOK, v, err)
}
