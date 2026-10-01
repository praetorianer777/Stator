// Command worker runs everything that happens outside a request: the outbox
// with the notifications it fans out and the page links it syncs to Armature,
// the notifications' digests, and the file reaper.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/praetorianer777/stator/backend/internal/armature"
	"github.com/praetorianer777/stator/backend/internal/attachment"
	"github.com/praetorianer777/stator/backend/internal/config"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/events"
	"github.com/praetorianer777/stator/backend/internal/mail"
	"github.com/praetorianer777/stator/backend/internal/netguard"
	"github.com/praetorianer777/stator/backend/internal/notify"
	"github.com/praetorianer777/stator/backend/internal/objectstore"
	"github.com/praetorianer777/stator/backend/internal/observability"
	"github.com/praetorianer777/stator/backend/internal/secret"
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

	store, err := objectstore.Open(objectstore.Config{
		Endpoint: cfg.S3.Endpoint, Bucket: cfg.S3.Bucket, AccessKey: cfg.S3.AccessKey,
		SecretKey: cfg.S3.SecretKey, Region: cfg.S3.Region, UseSSL: cfg.S3.UseSSL,
	})
	if err != nil {
		return err
	}
	if objectstore.IsUnavailable(store) {
		log.Warn("the attachment reaper is off: STATOR_S3_ENDPOINT is not set")
	} else {
		reaper := attachment.NewReaper(attachment.NewService(cluster, store, nil).WithLogger(log), log, attachment.DefaultReapInterval)
		go reaper.Run(ctx)
	}

	var mailer mail.Mailer
	if cfg.Mail.SMTPAddr == "" {
		log.Warn("mail is off: STATOR_SMTP_ADDR is not set, so notifications are written in the app only")
	} else {
		mailer = mail.SMTPMailer{Addr: cfg.Mail.SMTPAddr, From: cfg.Mail.From}
	}
	fanOut := notify.NewFanOut(cluster, mailer, cfg.AppBaseURL, log)
	var box *secret.Box
	if cfg.SecretKey != nil {
		if box, err = secret.New(cfg.SecretKey); err != nil {
			return err
		}
	} else {
		log.Warn("STATOR_SECRET_KEY is not set, so no stored Armature token opens and every page link to Armature fails")
	}
	allow := netguard.ParseAllow(cfg.Armature.OutboundAllow)
	armatures := armature.NewService(cluster, box, armature.NewClient(allow, cfg.Armature.Backchannel), nil,
		armature.Options{AppURL: cfg.AppBaseURL, Allow: allow, Development: cfg.Env == config.EnvDevelopment, Log: log})
	handlers := events.NewMux(fanOut).Route(events.TopicArmatureLinks, armature.NewLinkSync(armatures, log))
	go events.NewWorker(cluster, handlers, log).Run(ctx)
	if mailer != nil {
		go notify.NewDigester(cluster, mailer, cfg.AppBaseURL, log).Run(ctx)
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
