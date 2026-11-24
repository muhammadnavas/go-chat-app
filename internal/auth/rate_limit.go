package auth

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// RateLimitMiddleware limits requests per client IP within a sliding time window.
func RateLimitMiddleware(maxRequests int, window time.Duration) gin.HandlerFunc {
	var mu sync.Mutex
	hits := make(map[string][]time.Time)

	return func(c *gin.Context) {
		ip := c.ClientIP()
		now := time.Now()
		cutoff := now.Add(-window)

		mu.Lock()
		times := hits[ip][:0]
		for _, t := range hits[ip] {
			if t.After(cutoff) {
				times = append(times, t)
			}
		}
		if len(times) >= maxRequests {
			hits[ip] = times
			mu.Unlock()
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "too many requests, please try again shortly",
			})
			return
		}
		hits[ip] = append(times, now)
		mu.Unlock()

		c.Next()
	}
}

