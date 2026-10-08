package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/accessgate"
	"github.com/fishtre-compagnie/husonym/controlplane/console"
	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/controlplane/intake"
	"github.com/fishtre-compagnie/husonym/controlplane/issuing"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
)

const (
	backofficeHostEnv   = "CONTROLPLANE_BACKOFFICE_HOST"
	accessTeamDomainEnv = "CONTROLPLANE_ACCESS_TEAM_DOMAIN"
	accessAudienceEnv   = "CONTROLPLANE_ACCESS_AUD"
	// signingKeyFileEnv names the file of the private key the console issues licenses with. Only
	// `serve backoffice` reads it; without it the console issues nothing.
	signingKeyFileEnv = "CONTROLPLANE_SIGNING_KEY_FILE"

	insecureNoAccessFlag = "insecure-no-access"
	healthPath           = "/healthz"

	// backofficeMaxHeaderBytes caps the headers of a request to the backoffice. The Access gate
	// puts no cap of its own on the token it reads from a header, and the default of net/http,
	// 1 MiB, is far more than a token and a browser need.
	backofficeMaxHeaderBytes = 64 << 10
)

// backofficeConfig is what `serve backoffice` reads of its environment and of its flags.
type backofficeConfig struct {
	databaseURL string
	addr        string
	// insecure tells both gates are skipped; host, teamDomain and audience are then not read.
	insecure   bool
	host       string
	teamDomain string
	audience   string
	// signingKeyFile is the path of the signing key, empty for a console that issues nothing.
	signingKeyFile string
}

// backofficeGates is the two gates in front of the console.
type backofficeGates struct {
	host   string
	access *accessgate.Gate
}

// newServeBackofficeCmd builds `serve backoffice`; keyring gives the public keys a license is
// verified against, which the signing key must be one of.
func newServeBackofficeCmd(keyring func() (license.Keyring, error)) *cobra.Command {
	var insecure bool
	cmd := &cobra.Command{
		Use:   "backoffice",
		Short: "Serve the operator console",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := readBackofficeConfig(insecure)
			if err != nil {
				return err
			}
			// Before anything is reached: a key that cannot sign is told at once.
			signer, err := loadSigner(cfg.signingKeyFile, keyring)
			if err != nil {
				return err
			}
			logger := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), nil))
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGTERM, syscall.SIGINT)
			defer stop()
			return runBackoffice(ctx, cfg, signer, logger)
		},
	}
	cmd.Flags().BoolVar(&insecure, insecureNoAccessFlag, false,
		"serve without the host and Access gates; refused unless the listen address is a loopback address")
	return cmd
}

// readBackofficeConfig reads the settings of the backoffice and says which one is missing or
// wrong, by its name. The one value echoed is the listen address, when --insecure-no-access is
// refused for it: it is no secret, and it is what the operator has to change.
func readBackofficeConfig(insecure bool) (*backofficeConfig, error) {
	cfg := &backofficeConfig{
		databaseURL:    os.Getenv(databaseURLEnv),
		addr:           os.Getenv(listenAddrEnv),
		insecure:       insecure,
		signingKeyFile: os.Getenv(signingKeyFileEnv),
	}
	if cfg.databaseURL == "" {
		return nil, fmt.Errorf("%s is not set", databaseURLEnv)
	}
	if cfg.addr == "" {
		cfg.addr = defaultListenAddr
	}
	if insecure {
		if !loopbackAddr(cfg.addr) {
			return nil, fmt.Errorf("--%s is refused: the listen address %s is not a loopback address", insecureNoAccessFlag, cfg.addr)
		}
		return cfg, nil
	}
	required := []struct {
		name  string
		value *string
	}{
		{backofficeHostEnv, &cfg.host},
		{accessTeamDomainEnv, &cfg.teamDomain},
		{accessAudienceEnv, &cfg.audience},
	}
	for _, setting := range required {
		*setting.value = os.Getenv(setting.name)
		if *setting.value == "" {
			return nil, fmt.Errorf("%s is not set", setting.name)
		}
	}
	// accessgate.Host cannot fail: with a host that is not a bare name it would answer 404 to
	// everything, and the start would look sound.
	if err := accessgate.ValidHost(cfg.host); err != nil {
		return nil, fmt.Errorf("%s: %w", backofficeHostEnv, err)
	}
	return cfg, nil
}

// loadSigner reads the signing key at path and holds it to the keyring. Without a path there is no
// signer, and the console issues nothing. Any failure names the variable and says why in the fixed
// words of the signing package: never the path, and nothing of what the file holds.
func loadSigner(path string, keyring func() (license.Keyring, error)) (*issuing.Signer, error) {
	if path == "" {
		return nil, nil //nolint:nilnil // no signer is a way to run, not a failure
	}
	ring, err := keyring()
	if err != nil {
		return nil, fmt.Errorf("%s: unable to read the keyring licenses are verified against: %w", signingKeyFileEnv, err)
	}
	signer, err := issuing.LoadSigner(path, ring)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", signingKeyFileEnv, err)
	}
	return signer, nil
}

// runBackoffice builds what the backoffice needs and serves it until ctx ends. It does not apply
// the migrations: the public server owns them. signer is nil for a console that issues nothing.
func runBackoffice(ctx context.Context, cfg *backofficeConfig, signer *issuing.Signer, logger *slog.Logger) error {
	var gates *backofficeGates
	if !cfg.insecure {
		access, err := accessgate.New(ctx, accessgate.Config{
			TeamDomain: cfg.teamDomain,
			Audience:   cfg.audience,
			Now:        time.Now,
			Logger:     logger,
		})
		if err != nil {
			return fmt.Errorf("unable to set up the Access gate from %s and %s: %w", accessTeamDomainEnv, accessAudienceEnv, err)
		}
		gates = &backofficeGates{host: cfg.host, access: access}
	}

	pool, err := pgxpool.New(ctx, cfg.databaseURL)
	if err != nil {
		return errors.New("unable to open the database")
	}
	defer pool.Close()
	// Nothing else reads the database before the first page is asked for.
	if err := pool.Ping(ctx); err != nil {
		return errors.New("unable to reach the database")
	}
	store := cpstore.New(pool)
	pages := &console.Config{Reader: store, Writer: store, Now: time.Now, Logger: logger}
	// Left nil without a key: a nil *issuing.Signer in the interface would not be a nil Signer.
	if signer != nil {
		pages.Signer = signer
		pages.Promoter = intake.New(store, time.Now)
		logger.Info("the backoffice issues licenses", "kid", signer.Kid())
	} else {
		logger.Info("the backoffice issues no license: " + signingKeyFileEnv + " is not set")
	}
	handler, err := newBackofficeHandler(pages, gates)
	if err != nil {
		return err
	}

	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.addr)
	if err != nil {
		return fmt.Errorf("unable to listen on %s: %w", cfg.addr, err)
	}
	if cfg.insecure {
		// The name was judged; what it resolved to is what is listened on.
		if !loopbackBound(listener.Addr()) {
			_ = listener.Close()
			return fmt.Errorf("--%s is refused: %s does not resolve to a loopback address", insecureNoAccessFlag, cfg.addr)
		}
		logger.Warn("the backoffice is served without its host and Access gates")
	}
	return serveBackoffice(ctx, listener, handler, logger)
}

// newBackofficeHandler is everything the backoffice answers: the console behind the host gate,
// then the Access gate, and GET /healthz in front of both, since the kubelet that asks for it has
// neither the host nor a token. It is the only path outside the gates. Without gates, the console
// is the one that says so on every page, and it answers under the names of this machine only.
func newBackofficeHandler(cfg *console.Config, gates *backofficeGates) (http.Handler, error) {
	var guarded http.Handler
	if gates == nil {
		pages, err := console.NewUnguarded(cfg)
		if err != nil {
			return nil, err
		}
		guarded = localNames(pages)
	} else {
		pages, err := console.New(cfg)
		if err != nil {
			return nil, err
		}
		guarded = accessgate.Host(gates.host, gates.access.Wrap(pages))
	}
	return outermost(guarded, cfg.Logger), nil
}

// outermost is the first handler a request to the backoffice meets: it answers the health check
// and hands anything else to guarded.
//
// The console recovers from its own panics, the gates in front of it do not, and what net/http
// would say of a panic is dropped by the server: one that comes this far is answered 500 and
// logged here.
//
// Every answer carries the security headers of the console, whoever writes it: the refusal of a
// gate and the health check as much as a page.
func outermost(guarded http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		console.SetSecurityHeaders(w.Header())
		defer func() {
			if recover() != nil {
				// Fixed words only: what a panic carries may hold something of the request. A
				// gate writes nothing before it refuses or hands over, so nothing was sent yet.
				logger.Error("the backoffice panicked outside the console", "status", http.StatusInternalServerError)
				w.WriteHeader(http.StatusInternalServerError)
			}
		}()
		if r.URL.Path != healthPath {
			guarded.ServeHTTP(w, r)
			return
		}
		// The health check does not ask the database: a database that is away is not mended by
		// restarting the console.
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}

// newBackofficeServer is the server of the backoffice around handler: the one of the public
// server, with a cap on the headers.
func newBackofficeServer(handler http.Handler) *http.Server {
	server := newServer(handler)
	server.MaxHeaderBytes = backofficeMaxHeaderBytes
	return server
}

// serveBackoffice serves handler on listener until ctx ends; it then stops the server gracefully,
// within shutdownTimeout.
func serveBackoffice(ctx context.Context, listener net.Listener, handler http.Handler, logger *slog.Logger) error {
	server := newBackofficeServer(handler)
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	logger.Info("the backoffice server is listening")

	select {
	case err := <-served:
		return fmt.Errorf("the backoffice server stopped: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("unable to stop the backoffice server gracefully: %w", err)
	}
	logger.Info("the backoffice server is stopped")
	return nil
}
