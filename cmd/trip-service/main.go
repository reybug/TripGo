package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/reybug/TripGo/internal/config"
	api "github.com/reybug/TripGo/internal/generated"
	"github.com/reybug/TripGo/internal/trip"
)

func main() {
	if err := run(); err != nil {
		slog.Error("service stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)
	root, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pc, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return errors.New("invalid DATABASE_URL")
	}
	pc.MaxConns, pc.MinConns = cfg.MaxConns, cfg.MinConns
	pc.MaxConnLifetime, pc.ConnConfig.ConnectTimeout = cfg.MaxConnLifetime, cfg.ConnectTimeout
	startup, cancel := context.WithTimeout(root, cfg.ConnectTimeout)
	pool, err := pgxpool.NewWithConfig(startup, pc)
	if err != nil {
		cancel()
		return fmt.Errorf("create database pool: %w", err)
	}
	err = pool.Ping(startup)
	cancel()
	if err != nil {
		pool.Close()
		return fmt.Errorf("database startup ping: %w", err)
	}
	defer pool.Close()
	repo := trip.NewRepository(pool, cfg.QueryTimeout)
	service := trip.NewService(repo, trip.NewManager(pool, cfg.QueryTimeout))
	handler, err := trip.NewHandler(service, pool, cfg.QueryTimeout, logger)
	if err != nil {
		return fmt.Errorf("load API: %w", err)
	}
	router := chi.NewRouter()
	api.HandlerWithOptions(handler, api.ChiServerOptions{BaseRouter: router, ErrorHandlerFunc: handler.InvalidRequest})
	server := &http.Server{Addr: cfg.HTTPAddr, Handler: router,
		ReadTimeout: cfg.ReadTimeout, ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		WriteTimeout: cfg.WriteTimeout, IdleTimeout: cfg.IdleTimeout}
	failed := make(chan error, 1)
	go func() { failed <- server.ListenAndServe() }()
	logger.Info("HTTP server starting", "address", cfg.HTTPAddr)
	select {
	case err := <-failed:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("HTTP server: %w", err)
		}
		return nil
	case <-root.Done():
	}
	stop()
	logger.Info("shutdown started")
	shutdown, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		logger.Error("shutdown deadline exceeded", "error", err)
		os.Exit(1)
	}
	logger.Info("shutdown completed")
	return nil
}
