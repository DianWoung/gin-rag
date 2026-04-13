package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func AdminAuthMiddleware(expectedKey string) gin.HandlerFunc {
	return APIKeyAuthMiddleware(expectedKey, "admin authentication failed")
}

func APIKeyAuthMiddleware(expectedKey, message string) gin.HandlerFunc {
	expectedKey = strings.TrimSpace(expectedKey)

	return func(c *gin.Context) {
		if expectedKey == "" {
			c.Next()
			return
		}

		token := strings.TrimSpace(c.GetHeader("X-API-Key"))
		if token == "" {
			token = parseBearerToken(c.GetHeader("Authorization"))
		}

		if token == "" || token != expectedKey {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"message": message,
					"type":    "authentication_error",
				},
			})
			return
		}

		c.Next()
	}
}

func parseBearerToken(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}

	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}

	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}
