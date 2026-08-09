// Command api is the Shipper core platform: the versioned public API and the
// authoritative business rules, consumed by the Flutter app, the admin panel, and the
// driver portal alike (Docs/06 §2.1).
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
	"time"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/buildinfo"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/config"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/logging"
)

func main() {
	if err := run(); err != nil {
		// Deliberately not a structured log: reaching here can mean the configuration
		// that would have built the logger is the very thing that failed.
		fmt.Fprintf(os.Stderr, "shipper-api: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	startedAt := time.Now()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := logging.New(os.Stdout, cfg.Log.Level, cfg.Log.Format)
	slog.SetDefault(log)

	info := buildinfo.Get()
	log.Info("starting shipper core",
		slog.String("version", info.Version),
		slog.String("commit", buildinfo.ShortCommit()),
		slog.Bool("dirty", info.Dirty),
		slog.Any("config", cfg),
	)

	srv := &http.Server{
		Addr:    cfg.HTTP.Addr(),
		Handler: newRouter(log, startedAt),

		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
		IdleTimeout:  cfg.HTTP.IdleTimeout,

		// Bounded separately from ReadTimeout so a client that opens a connection and
		// dribbles headers cannot hold a goroutine for the full body allowance.
		ReadHeaderTimeout: 10 * time.Second,

		// Route net/http's own errors through the structured logger rather than the
		// standard library's default, which writes unstructured lines to stderr and
		// would be invisible to log search (SHIP-174).
		ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	// SIGTERM is what a container runtime sends before SIGKILL; handling it is what
	// makes a rolling deployment drain rather than drop in-flight requests.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		log.Info("http server listening", slog.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()

	select {
	case err := <-serveErr:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}

	log.Info("shutdown signal received, draining",
		slog.Duration("timeout", cfg.HTTP.ShutdownTimeout))

	// A fresh context: the one from NotifyContext is already cancelled, and passing it
	// to Shutdown would abandon every in-flight request immediately.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown failed after %s: %w", cfg.HTTP.ShutdownTimeout, err)
	}

	log.Info("stopped cleanly", slog.Duration("uptime", time.Since(startedAt).Round(time.Second)))
	return nil
}
