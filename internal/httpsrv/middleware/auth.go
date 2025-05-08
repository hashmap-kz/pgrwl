package middleware

import (
	"net/http"
	"strings"

	"github.com/hashmap-kz/pgrwl/internal/httpsrv/httputils"
)

type AuthMiddleware struct {
	Token string
}

// Middleware primitive authorization (for future use with any IPD providers)
func (m *AuthMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			httputils.WriteJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "missing or incorrect token",
			})
			return
		}
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if m.Token == "" || token != m.Token {
			httputils.WriteJSON(w, http.StatusForbidden, map[string]string{
				"error": "missing or incorrect token",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}
