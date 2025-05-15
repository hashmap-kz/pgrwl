package httpsrv

import (
	"log/slog"
	"net/http"
	"os"
	"strings"

	controlCrt "github.com/hashmap-kz/pgrwl/internal/opt/httpsrv/controller"
	controlSvc "github.com/hashmap-kz/pgrwl/internal/opt/httpsrv/service"

	"github.com/hashmap-kz/pgrwl/internal/core/xlog"
	"github.com/hashmap-kz/pgrwl/internal/opt/httpsrv/middleware"

	"golang.org/x/time/rate"
)

type HTTPHandlersDeps struct {
	PGRW    *xlog.PgReceiveWal
	BaseDir string
}

func InitHTTPHandlers(deps *HTTPHandlersDeps) http.Handler {
	verbose := strings.EqualFold(os.Getenv("LOG_LEVEL"), "trace")

	service := controlSvc.NewControlService(deps.PGRW, deps.BaseDir)
	controller := controlCrt.NewController(service)

	// init middlewares
	loggingMiddleware := middleware.LoggingMiddleware{
		Logger:  slog.With("component", "http-server"),
		Verbose: verbose,
	}
	rateLimitMiddleware := middleware.RateLimiterMiddleware{Limiter: rate.NewLimiter(5, 10)}

	// Build middleware chain
	secureChain := middleware.MiddlewareChain(
		middleware.SafeHandlerMiddleware,
		loggingMiddleware.Middleware,
		rateLimitMiddleware.Middleware,
	)

	// Init handlers
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Streaming mode (requires that wal-streaming process is running)
	mux.Handle("/status", secureChain(http.HandlerFunc(controller.StatusHandler)))
	mux.Handle("POST /retention", secureChain(http.HandlerFunc(controller.RetentionHandler)))

	// Standalone mode (i.e. just serving wal-archive during restore)
	mux.Handle("/archive/size", secureChain(http.HandlerFunc(controller.ArchiveSizeHandler)))
	mux.Handle("/wal/{filename}", secureChain(http.HandlerFunc(controller.WalFileDownloadHandler)))

	return mux
}
