package shared

import (
	"log/slog"
	"net/http"
	"net/http/pprof"

	"github.com/hashmap-kz/pgrwl/config"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func InitOptionalHandlers(cfg *config.Config, mux *http.ServeMux, l *slog.Logger) {
	if cfg.Metrics.Enable {
		l.Debug("enable metric endpoints")
		mux.Handle("/metrics", promhttp.Handler())
	}

	if cfg.DevConfig.Pprof.Enable {
		l.Debug("enable pprof endpoints")
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	}
}
