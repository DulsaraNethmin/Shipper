// Command notifier reads domain events off Kafka, works out who has to be told, and sends the
// messages (SHIP-137).
//
// It is the fourth long-running process in this service, after cmd/api, cmd/worker and nothing
// else. Docs/09 calls this ticket the "notification consumer service" and Docs/11 §6 left it to the
// lane whether it became a task in cmd/worker or a service of its own. It is a service, and there
// are four reasons, in the order they mattered.
//
// # 1. A cmd/worker pass is a transaction, and a consumer needs its offsets committed after one
//
// cmd/worker/scheduler.go runs each pass inside db.InTx and a task's Work is handed the
// transaction. A Kafka consumer must commit its topic offsets strictly *after* the database
// transaction that recorded the messages has committed — otherwise a rollback moves the offset past
// events nothing has recorded, and those notifications are never sent by anything. A Work has no
// after-commit hook and adding one would be a hook with a single consumer.
//
// The alternative was to commit offsets inside the pass, which is the version that looks fine and
// loses events in exactly the window cmd/worker/outbox.go documents on the other side of the same
// problem.
//
// # 2. The trigger shape is wrong
//
// Every registered task claims due rows on a ticker. A consumer blocks on a broker. The scheduler's
// ticker, its per-pass timeout and its "claimed n rows" log line are all the wrong instruments for
// something whose idle state is a blocking read.
//
// # 3. Blast radius, which Docs/11 §9 has been tracking for four waves
//
// cmd/worker is one binary and every start runs every registered task, so a consumer registered
// there would begin reading three Kafka topics inside every scripts/verify section that starts a
// worker to demonstrate job expiry or bid expiry. §9's entry lists four occasions that condition
// has already cost this repository. Here the cost is zero: nothing starts this process except the
// section that is demonstrating it.
//
// # 4. They scale on different things
//
// A consumer scales to the partition count, which is three and fixed by the catalogue. A sweep
// scales on the size of the backlog. Running them in one process means choosing one of those.
//
// The cost of the decision is this file: a second lifecycle, a second signal handler, a second
// place that opens a pool. That is roughly eighty lines, and it buys transaction boundaries that
// are correct rather than nearly correct.
package main

import (
	"context"
	"errors"
	"flag"
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
	"github.com/DulsaraNethmin/Shipper/services/core/internal/notifications"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/platform/email"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/platform/push"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/platform/sms"
)

func main() {
	if err := run(); err != nil {
		// Deliberately not a structured log: reaching here can mean the configuration that
		// would have built the logger is the very thing that failed.
		fmt.Fprintf(os.Stderr, "shipper-notifier: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	startedAt := time.Now()

	// The one flag, and consumer.go says why it is a flag rather than configuration.
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := logging.New(os.Stdout, cfg.Log.Level, cfg.Log.Format)
	slog.SetDefault(log)

	info := buildinfo.Get()
	log.Info("starting shipper notifier",
		slog.String("version", info.Version),
		slog.String("commit", buildinfo.ShortCommit()),
		slog.Bool("dirty", info.Dirty))

	// Refused rather than warned about, exactly as cmd/worker refuses to start without a
	// database. A notifier with no brokers has nothing to read; a process that reports itself
	// healthy while consuming nothing is the failure mode that goes unnoticed for a day.
	if len(cfg.Kafka.Brokers) == 0 {
		return errors.New("KAFKA_BROKERS is empty; a notifier with no broker has nothing to " +
			"consume, and a process that starts anyway is one nobody notices is idle")
	}

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

	service := notifications.NewService(jobPartiesLookup{}, clock.System{}, notifications.Senders{
		Email: newEmailSender(cfg),
		SMS:   newSMSSender(cfg),

		// SHIP-139 filled this in. It was nil at SHIP-137 because there was no Firebase
		// adapter and no device token registry, and a stub that logged instead of pushing
		// would have been a channel reporting a success it did not have — which is still
		// exactly what push.Noop must not do, and does not: it records, it never rejects,
		// and nothing marks a row sent that a real adapter would not have sent.
		Push: newPushSender(cfg),
	},
		// SHIP-140. Without this the consumer resolves no push address at all, because it
		// cannot tell a live device session from a signed-out one — see the port.
		notifications.WithSessions(deviceSessionLookup{}),
	)

	consumer, err := newConsumer(cfg, log, pool, service)
	if err != nil {
		return err
	}
	defer func() {
		// A fresh context rather than the cancelled one the loop ran under, for the reason
		// cmd/worker's closeAll gives: a Close inherited from a cancelled context is
		// cancelled before it begins, which is the bug that makes a graceful shutdown
		// quietly not flush.
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := consumer.Close(ctx); err != nil {
			log.Error("the consumer did not shut down cleanly", slog.String("error", err.Error()))
		}
	}()

	// SIGTERM is what a container runtime sends before SIGKILL. Handling it is what lets an
	// in-flight batch finish its transaction and commit its offsets, rather than being cut off
	// between the two and redelivering the batch on the next start.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := consumer.Run(ctx); err != nil {
		return err
	}

	log.Info("stopped cleanly", slog.Duration("uptime", time.Since(startedAt).Round(time.Second)))
	return nil
}

// newEmailSender picks the email implementation for this environment (SHIP-32).
//
// The same shape and the same reasoning as cmd/api's, including that email.UseConsole owns the rule
// rather than a switch written here and leans towards the console for anything it does not
// recognise: choosing wrongly towards the console costs a developer a puzzled minute, and choosing
// wrongly towards the provider sends real email from a machine that should never have had the
// credential.
//
// The two composition roots duplicate the function rather than sharing one, because sharing it
// would need a package both import, and the only such place is infrastructure — which is where
// Docs/06 §4.1 specifically does not want adapter selection to live.
func newEmailSender(cfg *config.Config) notifications.EmailSender {
	if email.UseConsole(cfg.Env) {
		return email.NewConsole()
	}

	sender, err := email.NewProvider(email.Options{
		BaseURL: cfg.Email.ProviderBaseURL,
		APIKey:  cfg.Email.ProviderAPIKey,
		Sender:  cfg.Email.Sender,
	})
	if err != nil {
		panic("cmd/notifier: email provider: " + err.Error() +
			" — set EMAIL_PROVIDER_BASE_URL, EMAIL_PROVIDER_API_KEY and EMAIL_SENDER, or run " +
			"with SHIPPER_ENV=development to log messages to the console instead")
	}
	return sender
}

// newSMSSender picks the SMS implementation for this environment (SHIP-35).
//
// Wired although notifications.Rules routes nothing to it, so that the channel is supported end to
// end and a ticket which decides an event belongs on SMS adds a line to the routing table and
// nothing else. See notifications.ChannelSMS for why no rule does today.
func newSMSSender(cfg *config.Config) notifications.SMSSender {
	if sms.UseConsole(cfg.Env) {
		return sms.NewConsole()
	}

	sender, err := sms.NewProvider(sms.Options{
		BaseURL: cfg.SMS.ProviderBaseURL,
		APIKey:  cfg.SMS.ProviderAPIKey,
		Sender:  cfg.SMS.Sender,
	})
	if err != nil {
		panic("cmd/notifier: sms provider: " + err.Error() +
			" — set SMS_PROVIDER_BASE_URL, SMS_PROVIDER_API_KEY and SMS_SENDER, or run with " +
			"SHIPPER_ENV=development to log messages to the console instead")
	}
	return sender
}

// newPushSender picks the push implementation for this environment (SHIP-139).
//
// The same shape as the two above and the same reasoning, including that push.UseNoop owns the rule
// rather than a switch written here. The fallback leans towards the no-op harder than email's leans
// towards the console, and internal/platform/push/fcm.go says why: a development machine that
// dispatched for real would wake a handset belonging to whoever last held that token, and unlike an
// email there is no address to inspect afterwards to work out who.
//
// # The credential is a closure, and the closure is the honest edge of this ticket
//
// FCM's HTTP v1 API takes a short-lived OAuth access token exchanged from a service-account JSON
// key. **That exchange is not implemented anywhere in this repository**: it needs
// golang.org/x/oauth2/google, a module, and the branch this ticket was built on may not edit
// go.mod. What is here instead is the wire contract and a credential supplied from configuration,
// which is enough to dispatch against a real project only if something else has already minted the
// token.
//
// So a deployment that sets PUSH_PROJECT_ID and PUSH_CREDENTIAL reaches Firebase, and one that sets
// neither runs the no-op. Refusing to start would be the wrong answer for the second, because push
// is one channel of two and email is not affected — Docs/01 §4.5's records copy still goes out.
func newPushSender(cfg *config.Config) notifications.Pusher {
	if push.UseNoop(cfg.Env) || cfg.Push.ProjectID == "" {
		return push.NewNoop()
	}

	sender, err := push.NewFCM(push.Options{
		ProjectID: cfg.Push.ProjectID,
		BaseURL:   cfg.Push.BaseURL,
		Credential: func(context.Context) (string, error) {
			if cfg.Push.Credential == "" {
				return "", errors.New("PUSH_CREDENTIAL is empty; see internal/platform/push/doc.go " +
					"on why nothing here mints one from a service-account key")
			}
			return cfg.Push.Credential, nil
		},
	})
	if err != nil {
		panic("cmd/notifier: push provider: " + err.Error() +
			" — set PUSH_PROJECT_ID and PUSH_CREDENTIAL, or run with SHIPPER_ENV=development to " +
			"log notifications instead of dispatching them")
	}
	return sender
}
