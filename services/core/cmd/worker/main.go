// Command worker runs Shipper's scheduled work: the four background tasks in the backlog and
// whatever follows them (SHIP-67a).
//
// Job expiry (SHIP-68), the expiry warning forty-eight hours ahead (SHIP-69), bid expiry
// (SHIP-89) and the seventy-two hour auto-complete (SHIP-119) all need a process that wakes up,
// finds what is due, and does it. None of them creates one, which is why this exists before any
// of them.
//
// It is a separate binary from cmd/api on purpose. The two scale on different things — one on
// requests, the other on the size of the backlog — and a scheduled sweep that runs inside every
// API instance would run once per instance.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/buildinfo"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/config"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/logging"
)

func main() {
	if err := run(); err != nil {
		// Deliberately not a structured log: reaching here can mean the configuration
		// that would have built the logger is the very thing that failed.
		fmt.Fprintf(os.Stderr, "shipper-worker: %v\n", err)
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
	log.Info("starting shipper worker",
		slog.String("version", info.Version),
		slog.String("commit", buildinfo.ShortCommit()),
		slog.Bool("dirty", info.Dirty))

	// Unlike cmd/api, this refuses to start without a database, and the difference is
	// deliberate.
	//
	// The API warns and starts because it has requests to serve and a rolling deployment
	// during a failover must not take every instance down at once. A worker has nothing to
	// serve: every task it could run is a claim against PostgreSQL, so a worker without one
	// is a process that does nothing while reporting itself healthy. Failing is honest, a
	// supervisor restarting it costs nobody a request, and the work it missed is still due
	// when it comes back — which is exactly the property claiming from domain tables buys.
	openCtx, cancelOpen := context.WithTimeout(context.Background(), 5*time.Second)
	pool, err := db.Open(openCtx, cfg.Database.URL, db.PoolOptions{
		MaxConns:        cfg.Database.MaxOpenConns,
		MinConns:        cfg.Database.MaxIdleConns,
		MaxConnLifetime: cfg.Database.ConnMaxLifetime,
	})
	cancelOpen()
	if err != nil {
		return err
	}
	defer pool.Close()

	built, err := tasks(Deps{
		Config: cfg,
		Logger: log,
		Clock:  clock.System{},
		Pool:   pool,
	})
	if err != nil {
		return err
	}

	scheduler, err := NewScheduler(pool, log, built)
	if err != nil {
		return err
	}

	// SIGTERM is what a container runtime sends before SIGKILL. Handling it is what lets a
	// pass in flight finish its transaction rather than have it rolled back mid-way — which
	// would be safe, since an abandoned claim is simply released, but would also mean every
	// deployment redid whatever was in progress.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := scheduler.Run(ctx); err != nil {
		return err
	}

	log.Info("stopped cleanly", slog.Duration("uptime", time.Since(startedAt).Round(time.Second)))
	return nil
}
