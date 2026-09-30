package husonym_gcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"cloud.google.com/go/storage"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/cloudidentity"
	"google.golang.org/api/option"
)

type ManagerInterface interface {
	GetClient(
		ctx context.Context,
		config *mgmtv1alpha1.GcpCloudStorageConnectionConfig,
		logger *slog.Logger,
	) (ClientInterface, error)
}

// Manager hands out the storage clients of GCS connections, with the credentials of each.
type Manager struct {
	// identity says whether a connection may act with the server's own identity.
	identity cloudidentity.Policy
}

var _ ManagerInterface = &Manager{}

func NewManager(identity cloudidentity.Policy) *Manager {
	return &Manager{identity: identity}
}

func (m *Manager) GetClient(
	ctx context.Context,
	config *mgmtv1alpha1.GcpCloudStorageConnectionConfig,
	logger *slog.Logger,
) (ClientInterface, error) {
	sc, err := NewStorageClient(ctx, config, m.identity)
	if err != nil {
		return nil, err
	}
	return NewClient(sc, logger), nil
}

// NewStorageClient returns the storage client of a GCS connection: authenticated with its
// service account, or with the application default credentials of the server where the
// policy allows it. The credentials must be a service account's: another type of credentials
// may name a file or a URL the server would read.
func NewStorageClient(
	ctx context.Context,
	config *mgmtv1alpha1.GcpCloudStorageConnectionConfig,
	identity cloudidentity.Policy,
) (*storage.Client, error) {
	credentials := config.GetServiceAccountCredentials()
	if err := identity.CheckGcs(credentials); err != nil {
		return nil, err
	}
	var opts []option.ClientOption
	if credentials != "" {
		if err := checkServiceAccount(credentials); err != nil {
			return nil, err
		}
		opts = append(opts, option.WithAuthCredentialsJSON(option.ServiceAccount, []byte(credentials)))
	}
	client, err := storage.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("unable to create the storage client of the connection: %w", err)
	}
	return client, nil
}

// checkServiceAccount refuses credentials that are not a service account's key, before the
// client library reads them: it only checks their type once it first uses them.
func checkServiceAccount(credentials string) error {
	var file struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(credentials), &file); err != nil {
		return errors.New("the service account credentials are not a JSON key file")
	}
	if file.Type != string(option.ServiceAccount) {
		return fmt.Errorf("the credentials are of type %q: only a service account's are accepted", file.Type)
	}
	return nil
}
