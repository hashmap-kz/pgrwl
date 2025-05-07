package httpsrv

import (
	"net/http"
	"time"
)

var cleanupLock = make(chan struct{}, 1)

func walRetentionHandler(w http.ResponseWriter, _ *http.Request) {
	select {
	case cleanupLock <- struct{}{}:
		// Acquired lock
		defer func() { <-cleanupLock }()
	default:
		WriteJSON(w, http.StatusConflict, map[string]string{
			"error": "cleanup already in progress",
		})
		return
	}

	// Long-running cleanup here...
	time.Sleep(5 * time.Second)
	WriteJSON(w, http.StatusOK, map[string]string{"status": "cleanup done"})
}
