// Command worker will run everything that happens outside a request. For now it
// connects, serves its metrics and waits to be stopped.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/praetorianer777/stator/backend/internal/config"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/observability"
	"github.com/praetorianer777/stator/backend/internal/version"
)

// flushTimeout bounds sending the traces still in hand at shutdown.
const flushTimeout = 5 * time.Second

func main() {
	if err := run(); err != nil {
		slog.Error("worker exited", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := observability.NewLogger(cfg.LogLevel, cfg.IsProduction())
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	tel, err := observability.Setup(ctx, observability.Config{
		Service: "stator-worker", Env: cfg.Env, MetricsAddr: cfg.Telemetry.MetricsAddr,
		OTLPEndpoint: cfg.Telemetry.OTLPEndpoint, SampleRatio: cfg.Telemetry.SampleRatio,
	}, log)
	if err != nil {
		return err
	}
	defer func() {
		flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), flushTimeout)
		defer cancel()
		if err := tel.Shutdown(flushCtx); err != nil {
			log.Warn("traces still in hand were not all sent", "error", err)
		}
	}()

	cluster, err := db.Open(ctx, cfg.DB, log, db.WithQueryTracer(tel.QueryTracer))
	if err != nil {
		return err
	}
	defer cluster.Close()
	// Outside development the tenant wall is the database's to keep, and a
	// role that ignores it would make every policy decoration.
	if cfg.Env != config.EnvDevelopment {
		if err := cluster.RefuseSuperuser(ctx); err != nil {
			return err
		}
	}
	if err := tel.Register(observability.NewClusterCollector(cluster)); err != nil {
		return err
	}

	build := version.Current()
	log.Info("worker started", "env", cfg.Env, "version", build.Version, "commit", build.Commit)
	if err := tel.Serve(ctx); err != nil {
		log.Warn("metrics listener stopped", "error", err)
	}
	<-ctx.Done()
	log.Info("worker stopped cleanly")
	return nil
}
