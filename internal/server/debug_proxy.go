package server

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func phoenixSpansProxyHandler() gin.HandlerFunc {
	client := &http.Client{Timeout: 20 * time.Second}

	return func(c *gin.Context) {
		project := strings.TrimSpace(c.Query("project"))
		if project == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{
					"message": "project is required",
					"type":    "invalid_request_error",
				},
			})
			return
		}

		base := strings.TrimSpace(c.Query("phoenix_base"))
		if base == "" {
			base = "http://127.0.0.1:6006"
		}
		base = strings.TrimRight(base, "/")
		parsedBase, err := url.Parse(base)
		if err != nil || (parsedBase.Scheme != "http" && parsedBase.Scheme != "https") || parsedBase.Host == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{
					"message": "phoenix_base must be a valid http(s) url",
					"type":    "invalid_request_error",
				},
			})
			return
		}

		limit := 300
		if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": gin.H{
						"message": "limit must be a positive integer",
						"type":    "invalid_request_error",
					},
				})
				return
			}
			if n > 1000 {
				n = 1000
			}
			limit = n
		}

		candidates := []string{base}
		if host := strings.ToLower(parsedBase.Hostname()); host == "localhost" || host == "127.0.0.1" {
			candidates = append(candidates, "http://phoenix:6006")
		}

		phoenixAPIKey := strings.TrimSpace(c.Query("phoenix_api_key"))
		var lastErr error
		for _, candidate := range candidates {
			endpoint := strings.TrimRight(candidate, "/") + "/v1/projects/" + url.PathEscape(project) + "/spans?limit=" + strconv.Itoa(limit)
			req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, endpoint, nil)
			if err != nil {
				lastErr = err
				continue
			}
			if phoenixAPIKey != "" {
				req.Header.Set("Authorization", "Bearer "+phoenixAPIKey)
			}

			resp, err := client.Do(req)
			if err != nil {
				lastErr = err
				continue
			}

			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr != nil {
				lastErr = readErr
				continue
			}

			contentType := resp.Header.Get("Content-Type")
			if contentType == "" {
				contentType = "application/json"
			}
			c.Data(resp.StatusCode, contentType, body)
			return
		}

		if lastErr == nil {
			lastErr = errors.New("unknown proxy error")
		}
		c.JSON(http.StatusBadGateway, gin.H{
			"error": gin.H{
				"message": "proxy phoenix request failed: " + lastErr.Error(),
				"type":    "bad_gateway",
			},
		})
	}
}
