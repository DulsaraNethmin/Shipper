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

	"github.com/redis/go-redis/v9"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/buildinfo"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/config"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/idempotency"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/logging"
)

// newRedisClient builds the Redis client from the configured URL.
//
// Parsing here rather than at configuration time keeps config free of a dependency on the
// client library: config validates strings, and the composition root turns them into
// connections.
func newRedisClient(cfg config.Redis) (*redis.Client, error) {
	opts, err := redis.ParseURL(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("REDIS_URL: %w", err)
	}
	return redis.NewClient(opts), nil
}

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

	// Redis backs idempotency keys, and will back refresh-token state, the device
	// registry, and rate limits (Docs/06 §2.1). It is the composition root's job to
	// build it: httpx knows only the interface it needs, and the store knows nothing
	// about HTTP.
	redisClient, err := newRedisClient(cfg.Redis)
	if err != nil {
		return err
	}
	defer func() { _ = redisClient.Close() }()

	// Reachability is checked at startup for the log line, not as a condition of
	// starting. A Redis that is briefly unavailable during a deployment must not stop
	// every instance from coming up — the endpoints that need it fail closed on their
	// own, per request, and the ones that do not keep serving.
	pingCtx, cancelPing := context.WithTimeout(context.Background(), 3*time.Second)
	if err := redisClient.Ping(pingCtx).Err(); err != nil {
		log.Warn("redis is not reachable at startup; "+
			"state-changing endpoints will be refused until it is",
			slog.String("error", err.Error()))
	}
	cancelPing()

	idempotencyStore := idempotency.NewRedisStore(redisClient,
		cfg.Idempotency.TTL, cfg.Idempotency.InFlightTTL)

	deps := Deps{
		Config:    cfg,
		Logger:    log,
		Clock:     clock.System{},
		StartedAt: startedAt,
	}

	srv := &http.Server{
		Addr:    cfg.HTTP.Addr(),
		Handler: newRouter(deps, idempotencyStore),

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
