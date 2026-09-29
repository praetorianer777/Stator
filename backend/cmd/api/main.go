// Command api serves the HTTP API.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/praetorianer777/stator/backend/internal/config"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/httpapi"
	"github.com/praetorianer777/stator/backend/internal/observability"
	"github.com/praetorianer777/stator/backend/internal/version"
)

// Server timeouts. The write timeout outlasts the request timeout, so a slow
// handler is answered by its own deadline rather than cut off mid-reply.
const (
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 60 * time.Second
	writeTimeout      = 120 * time.Second
	idleTimeout       = 120 * time.Second
	shutdownGrace     = 25 * time.Second
	healthcheckWait   = 3 * time.Second
)

// devOrigins are where the web client's development server runs.
var devOrigins = []string{"http://localhost:5173", "http://127.0.0.1:5173"}

func main() {
	if err := run(); err != nil {
		slog.Error("api exited", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// The container health check runs the binary itself, so it needs no curl in
	// the image and no drift between what the check probes and what we serve.
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		return healthcheck()
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := observability.NewLogger(cfg.LogLevel, cfg.IsProduction())
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	tel, err := observability.Setup(ctx, telemetryConfig(cfg, "stator-api"), log)
	if err != nil {
		return err
	}

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

	server := &httpapi.Server{
		DB:             cluster,
		Log:            log,
		Telemetry:      tel,
		CookieName:     cfg.Auth.SessionCookie,
		Secure:         cfg.Auth.SecureCookies,
		AppBaseURL:     cfg.AppBaseURL,
		CheckOrigin:    cfg.IsProduction(),
		RequestTimeout: cfg.RequestTimeout,
	}
	origins := cfg.CORSOrigins
	if len(origins) == 0 && !cfg.IsProduction() {
		origins = devOrigins
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           server.Routes(origins),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	// Exactly one value is ever sent on this channel. Closing it instead would
	// make the select below read a nil and report a clean exit for a failure.
	serveErr := make(chan error, 1)
	go func() {
		build := version.Current()
		log.Info("api listening", "addr", cfg.HTTPAddr, "env", cfg.Env, "version", build.Version, "commit", build.Commit)
		err := srv.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveErr <- err
	}()
	go func() {
		if err := tel.Serve(ctx); err != nil {
			log.Warn("metrics listener stopped", "error", err)
		}
	}()

	select {
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("serve: %w", err)
		}
		return errors.New("http server stopped serving without being asked to")
	case <-ctx.Done():
		log.Info("shutdown requested, draining connections")
	}

	// Give in-flight requests a chance to finish before the pools close.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownGrace)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	if err := tel.Shutdown(shutdownCtx); err != nil {
		log.Warn("traces still in hand were not all sent", "error", err)
	}
	log.Info("api stopped cleanly")
	return nil
}

func telemetryConfig(cfg config.Config, service string) observability.Config {
	return observability.Config{
		Service:      service,
		Env:          cfg.Env,
		MetricsAddr:  cfg.Telemetry.MetricsAddr,
		OTLPEndpoint: cfg.Telemetry.OTLPEndpoint,
		SampleRatio:  cfg.Telemetry.SampleRatio,
	}
}

// healthcheck probes the local readiness endpoint and fails unless it is ready,
// which is exactly what the container runtime wants.
func healthcheck() error {
	addr := os.Getenv("STATOR_HTTP_ADDR")
	if addr == "" {
		addr = config.DefaultHTTPAddr
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}

	client := &http.Client{Timeout: healthcheckWait}
	resp, err := client.Get("http://" + addr + "/readyz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("readiness returned %s", resp.Status)
	}
	return nil
}
