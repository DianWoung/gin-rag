package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/dianwang-mac/go-rag/internal/apperr"
)

func writeError(c *gin.Context, err error) {
	status := apperr.StatusCode(err)
	c.JSON(status, gin.H{
		"error": gin.H{
			"message": err.Error(),
			"type":    errorType(status),
		},
	})
}

func errorType(status int) string {
	switch {
	case status >= http.StatusBadRequest && status < http.StatusInternalServerError:
		return "invalid_request_error"
	default:
		return "internal_server_error"
	}
}

func badRequest(message string) error {
	return apperr.New(http.StatusBadRequest, errors.New(message))
}
