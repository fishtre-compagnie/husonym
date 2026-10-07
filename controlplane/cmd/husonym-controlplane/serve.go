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

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/controlplane/intake"
	"github.com/fishtre-compagnie/husonym/controlplane/migrations"
	"github.com/fishtre-compagnie/husonym/controlplane/publicapi"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
)

const (
	listenAddrEnv     = "CONTROLPLANE_LISTEN_ADDR"
	defaultListenAddr = ":8080"

	maintenanceTick   = time.Hour
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 15 * time.Second
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 10 * time.Second
)

func newServeCmd() *cobra.Command {
	serve := &cobra.Command{Use: "serve", Short: "Run a server of the control plane"}
	serve.AddCommand(&cobra.Command{
		Use:   "public",
		Short: "Receive the usage reports of the instances",
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
			return servePublic(ctx, pool, listener, logger, maintenanceTick)
		},
	})
	return serve
}

// newPublicServer is the server of the public API around handler. What net/http logs by itself
// is dropped: its lines name the remote address of the caller, and the line of a panic carries
// what was panicked with. The handler says in its own words what there is to say.
func newPublicServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ErrorLog:          log.New(io.Discard, "", 0),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}

// servePublic serves the public API on listener and runs the maintenance of the pending reports,
// until ctx ends; it then stops the server gracefully, within shutdownTimeout.
func servePublic(
	ctx context.Context, pool *pgxpool.Pool, listener net.Listener, logger *slog.Logger, tick time.Duration,
) error {
	store := cpstore.New(pool)
	receiver := intake.New(store, time.Now)
	server := newPublicServer(publicapi.NewHandler(receiver, logger))

	maintenanceCtx, cancelMaintenance := context.WithCancel(ctx)
	defer cancelMaintenance()
	maintenanceDone := make(chan struct{})
	go func() {
		defer close(maintenanceDone)
		publicapi.RunMaintenance(maintenanceCtx, receiver, store, logger, tick)
	}()

	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	logger.Info("the public server is listening")

	select {
	case err := <-served:
		// The server stopped by itself: the maintenance has no reason to go on.
		cancelMaintenance()
		<-maintenanceDone
		return fmt.Errorf("the public server stopped: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	err := server.Shutdown(shutdownCtx)
	<-maintenanceDone
	if err != nil {
		return fmt.Errorf("unable to stop the public server gracefully: %w", err)
	}
	logger.Info("the public server is stopped")
	return nil
}
