package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/cpmetrics"
	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/controlplane/intake"
	"github.com/fishtre-compagnie/husonym/controlplane/migrations"
	"github.com/fishtre-compagnie/husonym/controlplane/publicapi"
	"github.com/fishtre-compagnie/husonym/controlplane/renewal"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
)

const (
	listenAddrEnv      = "CONTROLPLANE_LISTEN_ADDR"
	defaultListenAddr  = ":8080"
	metricsAddrEnv     = "CONTROLPLANE_METRICS_ADDR"
	defaultMetricsAddr = ":9090"
	metricsPath        = "/metrics"

	maintenanceTick   = time.Hour
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 15 * time.Second
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 10 * time.Second
)

// newServeCmd builds `serve`; keyring is for the backoffice only: the public server signs nothing
// and reads no signing key.
func newServeCmd(keyring func() (license.Keyring, error)) *cobra.Command {
	serve := &cobra.Command{Use: "serve", Short: "Run a server of the control plane"}
	serve.AddCommand(&cobra.Command{
		Use:   "public",
		Short: "Receive the usage reports of the instances and answer their license renewals",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			databaseURL := os.Getenv(databaseURLEnv)
			if databaseURL == "" {
				return fmt.Errorf("%s is not set", databaseURLEnv)
			}
			addr := os.Getenv(listenAddrEnv)
			if addr == "" {
				addr = defaultListenAddr
			}
			metricsAddr := os.Getenv(metricsAddrEnv)
			if metricsAddr == "" {
				metricsAddr = defaultMetricsAddr
			}
			logger := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), nil))

			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGTERM, syscall.SIGINT)
			defer stop()

			if err := migrations.Up(ctx, databaseURL, logger); err != nil {
				return err
			}
			pool, err := pgxpool.New(ctx, databaseURL)
			if err != nil {
				return errors.New("unable to open the database")
			}
			defer pool.Close()
			listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
			if err != nil {
				return fmt.Errorf("unable to listen on %s: %w", addr, err)
			}
			metricsListener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", metricsAddr)
			if err != nil {
				_ = listener.Close()
				return fmt.Errorf("unable to listen on %s: %w", metricsAddr, err)
			}
			return servePublic(ctx, pool, listener, metricsListener, logger, maintenanceTick)
		},
	})
	serve.AddCommand(newServeBackofficeCmd(keyring))
	return serve
}

// newServer is a server of the control plane around handler, with its timeouts. What net/http
// logs by itself is dropped: its lines name the remote address of the caller, and the line of a
// panic carries what was panicked with. The handler says in its own words what there is to say.
// The lines of a failing accept are dropped with the rest: a listener that no longer accepts
// shows in the health check.
func newServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ErrorLog:          log.New(io.Discard, "", 0),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}

// metricsHandler serves GET /metrics from metrics and answers 404 to anything else.
func metricsHandler(metrics http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != metricsPath || r.Method != http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		metrics.ServeHTTP(w, r)
	})
}

// servePublic serves the public API, the reports and the renewals, on listener and the metrics on
// metricsListener, and runs the maintenance of the pending reports, until ctx ends; it then stops both servers gracefully,
// within shutdownTimeout. Should either server stop by itself, the other is stopped too.
func servePublic(
	ctx context.Context, pool *pgxpool.Pool, listener, metricsListener net.Listener, logger *slog.Logger,
	tick time.Duration,
) error {
	store := cpstore.New(pool)
	now := time.Now
	receiver := intake.New(store, now)
	metrics := cpmetrics.New(publicapi.Outcomes()...)
	metrics.StartRenewals(publicapi.RenewalOutcomes()...)
	metrics.WatchAttention(func(ctx context.Context) (cpstore.AttentionCounts, error) {
		return store.AttentionCounts(ctx, now())
	}, now, logger)
	// The renewal reads the licenses as they were issued and stored: this server signs nothing.
	server := newServer(publicapi.NewHandler(receiver, renewal.New(store, now, logger, metrics), metrics, logger))
	metricsServer := newServer(metricsHandler(metrics.Handler()))

	maintenanceCtx, cancelMaintenance := context.WithCancel(ctx)
	defer cancelMaintenance()
	maintenanceDone := make(chan struct{})
	go func() {
		defer close(maintenanceDone)
		publicapi.RunMaintenance(maintenanceCtx, receiver, store, logger, now, tick)
	}()

	served := make(chan error, 2)
	go func() { served <- server.Serve(listener) }()
	go func() { served <- metricsServer.Serve(metricsListener) }()
	logger.Info("the public server is listening")

	var stoppedBy error
	select {
	case err := <-served:
		// A server stopped by itself: the other one and the maintenance have no reason to go on.
		stoppedBy = fmt.Errorf("a server of the control plane stopped: %w", err)
		cancelMaintenance()
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	err := errors.Join(server.Shutdown(shutdownCtx), metricsServer.Shutdown(shutdownCtx))
	<-maintenanceDone
	if stoppedBy != nil {
		return stoppedBy
	}
	if err != nil {
		return fmt.Errorf("unable to stop the public server gracefully: %w", err)
	}
	logger.Info("the public server is stopped")
	return nil
}
