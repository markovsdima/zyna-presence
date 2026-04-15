package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/markovsdima/zyna-presence/internal/auth"
	"github.com/markovsdima/zyna-presence/internal/config"
	"github.com/markovsdima/zyna-presence/internal/handler"
	"github.com/markovsdima/zyna-presence/internal/presence"
	"github.com/markovsdima/zyna-presence/internal/ws"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Last-seen store.
	store, err := presence.NewStore(cfg.LastSeenFile)
	if err != nil {
		slog.Error("failed to load last_seen store", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go store.StartPeriodicFlush(ctx, time.Minute)

	// Presence tracker.
	tracker := presence.NewTracker(store)

	// Auth.
	var authHandler *handler.AuthHandler
	if cfg.AuthMode == "dev" {
		slog.Warn("running in dev auth mode — do not use in production")
		authHandler = handler.NewDevAuthHandler(cfg.JWTSecret, cfg.DevPassword)
	} else {
		synapse := auth.NewSynapseClient(cfg.SynapseURL)
		authHandler = handler.NewAuthHandler(synapse, cfg.JWTSecret)
	}
	wsHandler := ws.NewHandler(tracker, cfg.JWTSecret)

	// Router.
	r := chi.NewRouter()
	r.Use(chimw.Recoverer)

	r.Get("/health", handler.Health)
	r.Post("/presence/auth", authHandler.ServeHTTP)
	r.Get("/presence/ws", wsHandler.ServeHTTP)

	// Server — no ReadTimeout/WriteTimeout for long-lived WebSocket connections.
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Graceful shutdown.
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		slog.Info("shutting down", "signal", sig.String())

		cancel() // stops periodic flush, triggers final flush

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("shutdown error", "error", err)
		}
	}()

	slog.Info("starting server", "port", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server error", "error", err)
		store.Flush()
		os.Exit(1)
	}
	slog.Info("server stopped")
}
