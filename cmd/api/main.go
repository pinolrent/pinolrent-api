// Package main runs the pinolrent-api HTTP server.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/pinolrent/pinolrent-api/internal/auth"
	"github.com/pinolrent/pinolrent-api/internal/config"
	"github.com/pinolrent/pinolrent-api/internal/db"
	"github.com/pinolrent/pinolrent-api/internal/handlers"
	"github.com/pinolrent/pinolrent-api/internal/ratelimit"
)

// version is the build version reported by GET /health. Release builds set it
// via -ldflags "-X main.version=<version>".
var version = "dev"

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))

	// Load .env if present. Values already set in the environment win over it.
	_ = godotenv.Load()

	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		slog.Error("invalid config", "error", err)
		os.Exit(1)
	}

	// Set up signal handling early so background goroutines (like the
	// revoked_tokens GC) can listen on the same ctx.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	d, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		slog.Error("open db", "error", err)
		os.Exit(1)
	}
	defer func() { _ = d.Close() }()

	a := auth.New(cfg.JWTSecret, d)
	h := handlers.New(d, a)
	h.Version = version

	// Periodically drop rows from revoked_tokens whose tokens have
	// already expired. Keeps the table small so the per-request
	// IsRevoked check stays cheap.
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := a.GCRevoked(ctx); err != nil {
					slog.Error("revoked tokens gc", "error", err)
				}
			}
		}
	}()

	origins, err := cfg.CORSOrigins()
	if err != nil {
		slog.Error("invalid config", "error", err)
		os.Exit(1)
	}

	// Nest inside-out so that CORS ends up outermost: preflights short-circuit
	// before reaching the rate limiter, and every response (even 429) ships the
	// CORS headers the browser needs to read it.
	mux := handlers.WithCORS(origins)(handlers.WithRequestLog(handlers.WithRecover(ratelimit.New(0.5, 30).Middleware(handlers.Routes(h), "/auth/"))))

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	go func() {
		slog.Info("listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown", "error", err)
	}
}
