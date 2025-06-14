package middleware

import (
	"net/http"

	"golang.org/x/time/rate"
)

type RateLimiterMiddleware struct {
	Limiter *rate.Limiter
}

func (m *RateLimiterMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !m.Limiter.Allow() {
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
