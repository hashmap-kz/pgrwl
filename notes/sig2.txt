package app

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	slogLogger "go-project-template-v5/pkg/logger"

	"go-project-template-v5/internal/server/middlewares"

	"go-project-template-v5/internal/api"

	"go-project-template-v5/config"
	httpserver "go-project-template-v5/internal/server/http"
	"go-project-template-v5/pkg/storage/postgres"
)

func Run(configFilePath string) {
	ctx := context.TODO()

	log.Printf("Starting api server. Config-path: %s\n", configFilePath)

	// load config
	config.LoadConfigFromFile(configFilePath)
	cfg := config.Cfg()

	// init logger
	logger := slogLogger.InitLogger(cfg.Logger.Format, cfg.Logger.Level)
	slog.SetDefault(logger)

	// init pgxpool
	pg, err := postgres.New(logger, cfg.Postgres.URL, postgres.MaxPoolSize(cfg.Postgres.PoolMax))
	if err != nil {
		logger.Error("Postgresql init error", slog.String("err", err.Error()))
	} else {
		logger.Info("Postgres connected")
	}
	defer pg.Close()

	// init router, register routes for all module
	router := httpserver.InitRouter(ctx)
	repositories := api.NewRepositories(ctx, pg)
	services := api.NewServices(ctx, api.Deps{
		Repos: repositories,
	})
	handler := api.NewHandler(services)
	handler.Init(router)

	// HTTP server
	middlewareChain := middlewares.MiddlewareChain(
		// TODO: other middlewares here (oauth2, etc...)
		middlewares.LoggingMiddleware,
		// middlewares.AuthorizeMiddleware,
	)
	srv := httpserver.NewServer(middlewareChain(router))

	go func() {
		if err := srv.Run(); !errors.Is(err, http.ErrServerClosed) {
			logger.Error("error occurred while running http server", slog.String("err", err.Error()))
		}
	}()

	logger.Info("Server started")

	// Graceful Shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)

	<-quit

	const timeout = 5 * time.Second

	ctx, shutdown := context.WithTimeout(context.Background(), timeout)
	defer shutdown()

	if err := srv.Stop(ctx); err != nil {
		logger.Error("failed to stop server", slog.String("err", err.Error()))
	}
}
