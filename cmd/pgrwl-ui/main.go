package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/pgrwl/pgrwl/ui"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	server := ui.NewServer(ui.Options{
		Logger: logger,
		Receivers: []ui.Receiver{
			{Label: "localhost", Addr: "http://127.0.0.1:7070"},
			{Label: "prod-db-01", Addr: "http://10.0.0.11:9090"},
			{Label: "prod-db-02", Addr: "http://10.0.0.12:9090"},
			{Label: "staging-db", Addr: "http://10.1.0.5:9090"},
		},
	})

	mux := http.NewServeMux()
	server.Mount(mux)

	addr := ":8080"
	logger.Info("serving pgrwl ui", slog.String("addr", addr))

	// TODO: timeout

	//nolint:gosec
	if err := http.ListenAndServe(addr, mux); err != nil {
		logger.Error("server stopped", slog.Any("err", err))
		os.Exit(1)
	}
}
