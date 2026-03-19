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
	"github.com/redis/go-redis/v9"

	"github.com/markovsdima/zyna-presence/internal/config"
	"github.com/markovsdima/zyna-presence/internal/handler"
	"github.com/markovsdima/zyna-presence/internal/middleware"
	"github.com/markovsdima/zyna-presence/internal/service"
	"github.com/markovsdima/zyna-presence/internal/storage"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Redis client
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		slog.Error("failed to connect to Redis", "error", err)
		os.Exit(1)
	}

	// Layers
	store := storage.NewRedisStore(rdb)
	svc := service.NewPresenceService(store, cfg.PresenceTTL)
	h := handler.NewPresenceHandler(svc)

	// Router
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimw.Recoverer)

	// Health check — no auth or rate limiting (for probes)
	r.Get("/health", handler.Health)

	// Authenticated routes: HMAC → IP rate limit → handlers
	r.Group(func(r chi.Router) {
		r.Use(middleware.HMACAuth(cfg.HMACSecret))
		r.Use(middleware.IPRateLimit(store, cfg.RateLimitIP))

		r.Route("/presence", func(r chi.Router) {
			r.With(middleware.UserHeartbeatRateLimit(store, cfg.RateLimitHeartbeat)).
				Put("/{userID}", h.Heartbeat)
			r.Post("/status", h.BatchStatus)
		})
	})

	// Server
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		slog.Info("shutting down", "signal", sig.String())

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("shutdown error", "error", err)
		}
	}()

	slog.Info("starting server", "port", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
	slog.Info("server stopped")
}
