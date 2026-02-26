package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/use-agent/purify/models"
)

// Auth returns API-key authentication middleware.
//
// Supports two header styles:
//
//	X-API-Key: <key>
//	Authorization: Bearer <key>
//
// If apiKeys is empty, the middleware is a no-op (open access).
func Auth(apiKeys []string) gin.HandlerFunc {
	if len(apiKeys) == 0 {
		return func(c *gin.Context) { c.Next() }
	}

	keySet := make(map[string]struct{}, len(apiKeys))
	for _, k := range apiKeys {
		if k != "" {
			keySet[k] = struct{}{}
		}
	}

	return func(c *gin.Context) {
		key := extractAPIKey(c)
		if key == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, models.ScrapeResponse{
				Success: false,
				Error: &models.ErrorDetail{
					Code:    models.ErrCodeUnauthorized,
					Message: "missing API key: provide X-API-Key header or Authorization: Bearer <key>",
				},
			})
			return
		}

		if _, valid := keySet[key]; !valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, models.ScrapeResponse{
				Success: false,
				Error: &models.ErrorDetail{
					Code:    models.ErrCodeUnauthorized,
					Message: "invalid API key",
				},
			})
			return
		}

		c.Set("api_key", key)
		c.Next()
	}
}

// extractAPIKey tries X-API-Key first, then Authorization: Bearer.
func extractAPIKey(c *gin.Context) string {
	if key := c.GetHeader("X-API-Key"); key != "" {
		return key
	}
	if auth := c.GetHeader("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}
