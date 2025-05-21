package loops

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

type HTTPSrv struct {
	l      *slog.Logger
	port   int
	router http.Handler
}

func NewHTTPSrv(port int, router http.Handler) *HTTPSrv {
	return &HTTPSrv{
		l:      slog.With("component", "httpsrv"),
		port:   port,
		router: router,
	}
}

func (s *HTTPSrv) log() *slog.Logger {
	if s.l != nil {
		return s.l
	}
	return slog.With("component", "httpsrv")
}

func (s *HTTPSrv) Run(ctx context.Context) error {
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", s.port),
		Handler:           s.router,
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		// Context was cancelled, shut down the HTTP server gracefully
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			s.log().Error("HTTP server shutdown error", slog.Any("err", err))
		} else {
			s.log().Debug("HTTP server shut down")
		}
	}()

	s.log().Info("starting HTTP server", slog.String("addr", srv.Addr))

	// Start the server (blocking)
	err := srv.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err // real error
	}
	return nil
}
