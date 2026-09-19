package sqlprovider

import (
	"log/slog"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlconnect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqldbtx"
	connectionmanager "github.com/fishtre-compagnie/husonym/internal/connection-manager"
	husonym_benthos_sql "github.com/fishtre-compagnie/husonym/worker/pkg/benthos/sql"
)

type Provider struct {
	connector sqlconnect.SqlConnector
}

func NewProvider(
	sqlconnector sqlconnect.SqlConnector,
) *Provider {
	return &Provider{connector: sqlconnector}
}

var _ connectionmanager.ConnectionProvider[husonym_benthos_sql.SqlDbtx] = &Provider{}

type sqlDbtxWrapper struct {
	sqldbtx.DBTX
	close func() error
}

func (s *sqlDbtxWrapper) Close() error {
	return s.close()
}

const defaultConnectionTimeoutSeconds = uint32(10)

// GetConnectionClient opens a connection to a data source.
//
// MySQL dates and times are read as the text the server prints, not as a Go time: a date
// MySQL accepts and Go cannot hold — a zero date, a month or a day at zero, kept by a
// laxer sql_mode — comes back silently moved otherwise, "2024-00-00" read as the last day
// of November 2023. A tool that copies data must read what the database holds.
func (p *Provider) GetConnectionClient(
	cc *mgmtv1alpha1.ConnectionConfig,
	logger *slog.Logger,
) (husonym_benthos_sql.SqlDbtx, error) {
	container, err := p.connector.NewDbFromConnectionConfig(
		cc,
		logger,
		sqlconnect.WithConnectionTimeout(defaultConnectionTimeoutSeconds),
		sqlconnect.WithMysqlParseTimeDisabled(),
	)
	if err != nil {
		return nil, err
	}
	dbtx, err := container.Open()
	if err != nil {
		return nil, err
	}
	return &sqlDbtxWrapper{DBTX: dbtx, close: func() error {
		return container.Close()
	}}, nil
}

func (p *Provider) CloseClientConnection(client husonym_benthos_sql.SqlDbtx) error {
	return client.Close()
}
