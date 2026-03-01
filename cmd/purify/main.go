package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/use-agent/purify/api"
	"github.com/use-agent/purify/cache"
	"github.com/use-agent/purify/cleaner"
	"github.com/use-agent/purify/config"
	"github.com/use-agent/purify/scraper"
)

func main() {
	// ── 1. Load configuration ───────────────────────────────────────
	cfg := config.Load()

	// ── 2. Initialise structured logging ────────────────────────────
	initLogger(cfg.Log)
	slog.Info("purify starting",
		"host", cfg.Server.Host,
		"port", cfg.Server.Port,
		"mode", cfg.Server.Mode,
		"maxPages", cfg.Browser.MaxPages,
	)

	// ── 3. Initialise scraper (launches browser) ────────────────────
	sc, err := scraper.NewScraper(cfg.Browser, cfg.Scraper)
	if err != nil {
		slog.Error("failed to initialise scraper", "error", err)
		os.Exit(1)
	}
	defer sc.Close()

	// ── 4. Initialise cleaner ───────────────────────────────────────
	cl := cleaner.NewCleaner()

	// ── 4b. Initialise cache ────────────────────────────────────────
	cc := cache.New(cfg.Cache.MaxEntries)

	// ── 5. Setup router ─────────────────────────────────────────────
	startTime := time.Now()
	router := api.NewRouter(sc, cl, cfg, cc, startTime)

	// ── 6. Start HTTP server ────────────────────────────────────────
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	go func() {
		slog.Info("HTTP server listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server error", "error", err)
			os.Exit(1)
		}
	}()

	// ── 7. Graceful shutdown ────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	slog.Info("shutdown signal received", "signal", sig.String())

	// Give in-flight requests 5 seconds to complete.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("HTTP server forced shutdown", "error", err)
	} else {
		slog.Info("HTTP server drained gracefully")
	}

	// sc.Close() runs via defer — drains page pool and kills Chrome.
	slog.Info("purify stopped")
}

// initLogger configures slog based on the LogConfig.
func initLogger(cfg config.LogConfig) {
	var level slog.Level
	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if cfg.Format == "text" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	slog.SetDefault(slog.New(handler))
}
