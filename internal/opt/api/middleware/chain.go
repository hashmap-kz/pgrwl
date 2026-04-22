package middleware

import "net/http"

// ---- Middleware Chain ----

type Middleware func(http.Handler) http.Handler

func Chain(middleware ...Middleware) Middleware {
	return func(final http.Handler) http.Handler {
		for i := len(middleware) - 1; i >= 0; i-- {
			final = middleware[i](final)
		}
		return final
	}
}
