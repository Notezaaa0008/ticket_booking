package handler

import (
	"errors"
	"log"

	"github.com/gin-gonic/gin"

	"ticketbooking/internal/domain"
)

// RespondError writes the standard error body: {"error":{"code":"...","message":"..."}}.
// Unknown errors are logged with the request id and returned as a generic 500.
func RespondError(c *gin.Context, err error) {
	var appErr *domain.AppError
	if !errors.As(err, &appErr) {
		log.Printf("request_id=%s unexpected error: %v", c.GetString("request_id"), err)
		appErr = domain.NewError(domain.ReasonInternalError, "internal server error")
	}
	c.AbortWithStatusJSON(appErr.Status, gin.H{
		"error": gin.H{"code": appErr.Code, "message": appErr.Message},
	})
}
