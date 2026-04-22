package rest

import (
	"net/http"
	"sync/atomic"
)

// ModeRouter is an http.Handler whose inner handler can be swapped atomically
// without restarting the HTTP server. In-flight requests complete against the
// old handler; new requests see the new one immediately after SwapHandler.
type ModeRouter struct {
	handler atomic.Pointer[http.Handler]
}

func NewModeRouter(initial http.Handler) *ModeRouter {
	r := &ModeRouter{}
	r.handler.Store(&initial)
	return r
}

func (r *ModeRouter) SwapHandler(h http.Handler) {
	r.handler.Store(&h)
}

func (r *ModeRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	(*r.handler.Load()).ServeHTTP(w, req)
}
