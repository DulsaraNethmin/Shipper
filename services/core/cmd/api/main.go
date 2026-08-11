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
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
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

	// PostgreSQL is the source of truth for every business decision (Docs/06 §4), and from
	// wave 2 onwards it is what the endpoints are made of. It is built here, once, and
	// handed to every domain through Deps.
	//
	// # Why this warns rather than refusing to start
	//
	// db.Open pings and returns an error, and its own comment argues the opposite case —
	// that nothing works without PostgreSQL, so a bad DATABASE_URL should fail at startup
	// rather than at the first request. That reasoning is about a *misconfigured* service
	// and it is right about one: a URL pointing nowhere should never reach production
	// quietly.
	//
	// This is the other case. A rolling deployment during a database failover, or a
	// restart while PostgreSQL is briefly away, would take down every instance at once and
	// keep them down — a crash loop timed exactly when the database is least able to
	// absorb a stampede of reconnections. The same argument the Redis ping above already
	// makes, and the same conclusion: log it, start, and let the requests that need it
	// fail per request.
	//
	// The endpoints that need the pool are not yet written, so nothing is degraded by this
	// today. When they are, /health deliberately keeps saying "ok" — it answers "should
	// this instance be restarted", and a database blip is not a reason to kill the fleet.
	// A readiness endpoint that does check dependencies is a separate thing.
	// Bounded for the same reason the Redis ping is: db.Open pings before returning, and an
	// unreachable host answers a TCP connect by not answering. Without a deadline the
	// "warn and start" path above would wait on the operating system's connect timeout,
	// which on a dropped-packet failure is minutes — long enough that a deployment gives up
	// on the instance first, turning a warning into the crash loop it exists to avoid.
	openCtx, cancelOpen := context.WithTimeout(context.Background(), 5*time.Second)
	pool, err := db.Open(openCtx, cfg.Database.URL, db.PoolOptions{
		MaxConns:        cfg.Database.MaxOpenConns,
		MinConns:        cfg.Database.MaxIdleConns,
		MaxConnLifetime: cfg.Database.ConnMaxLifetime,
	})
	cancelOpen()
	if err != nil {
		log.Warn("postgresql is not reachable at startup; "+
			"endpoints that need it will fail until it is",
			slog.String("error", err.Error()))
	} else {
		defer pool.Close()
	}

	deps := Deps{
		Config: cfg,
		Logger: log,
		Clock:  clock.System{},

		// Both may be nil, and a domain must not treat either as a promise. See Deps.
		Pool:  pool,
		Redis: redisClient,

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
