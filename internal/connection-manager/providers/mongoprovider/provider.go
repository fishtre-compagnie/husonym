package mongoprovider

import (
	"context"
	"errors"
	"log/slog"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	connectionmanager "github.com/fishtre-compagnie/husonym/internal/connection-manager"
	husonym_benthos_mongodb "github.com/fishtre-compagnie/husonym/worker/pkg/benthos/mongodb"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Provider struct{}

func NewProvider() *Provider {
	return &Provider{}
}

var _ connectionmanager.ConnectionProvider[husonym_benthos_mongodb.MongoClient] = &Provider{}

// this is currently untested as it isn't really used anywhere
func (p *Provider) GetConnectionClient(
	cc *mgmtv1alpha1.ConnectionConfig,
	logger *slog.Logger,
) (husonym_benthos_mongodb.MongoClient, error) {
	connStr := cc.GetMongoConfig().GetUrl()
	if connStr == "" {
		return nil, errors.New("unable to find mongodb url on connection config")
	}

	serverAPI := options.ServerAPI(options.ServerAPIVersion1)
	opts2 := options.Client().ApplyURI(connStr).SetServerAPIOptions(serverAPI)

	client, err := mongo.Connect(context.Background(), opts2)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func (p *Provider) CloseClientConnection(client husonym_benthos_mongodb.MongoClient) error {
	return client.Disconnect(context.Background())
}
