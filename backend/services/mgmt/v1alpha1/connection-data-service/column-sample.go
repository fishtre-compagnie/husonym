package v1alpha1_connectiondataservice

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
)

const (
	defaultSampleLimit = 20
	maxSampleLimit     = 200
	// Tronque les valeurs très longues : l'aperçu sert à juger la nature d'une
	// colonne, pas à afficher un document entier.
	maxSampleValueRunes = 500
)

// GetColumnSampleValues retourne les premières valeurs d'une colonne.
//
// Sert la levée de doute : quand une détection est marquée « à vérifier », voir
// les données réelles est le moyen le plus direct de trancher. Contrairement au
// scan PII, aucune analyse n'est faite ici — on renvoie les valeurs telles quelles.
func (s *Service) GetColumnSampleValues(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetColumnSampleValuesRequest],
) (*connect.Response[mgmtv1alpha1.GetColumnSampleValuesResponse], error) {
	logger := logger_interceptor.GetLoggerFromContextOrDefault(ctx)

	limit := req.Msg.GetLimit()
	if limit == 0 {
		limit = defaultSampleLimit
	}
	if limit > maxSampleLimit {
		limit = maxSampleLimit
	}

	connResp, err := s.connectionService.GetConnection(
		ctx,
		connect.NewRequest(&mgmtv1alpha1.GetConnectionRequest{Id: req.Msg.GetConnectionId()}),
	)
	if err != nil {
		return nil, err
	}
	dataconn, err := s.connectiondatabuilder.NewDataConnection(logger, connResp.Msg.GetConnection())
	if err != nil {
		return nil, err
	}

	collector := &rowCollector{}
	if err := dataconn.SampleData(
		ctx,
		collector,
		req.Msg.GetSchema(),
		req.Msg.GetTable(),
		uint(limit),
	); err != nil {
		return nil, fmt.Errorf("unable to sample column data: %w", err)
	}

	column := req.Msg.GetColumn()
	values := make([]*mgmtv1alpha1.ColumnSampleValue, 0, len(collector.rows))
	for _, rowbytes := range collector.rows {
		row := map[string]any{}
		if err := gob.NewDecoder(bytes.NewReader(rowbytes)).Decode(&row); err != nil {
			logger.Warn(fmt.Sprintf("skipping undecodable sampled row: %v", err))
			continue
		}
		raw, ok := row[column]
		if !ok {
			// La colonne n'existe pas dans la table : autant le dire clairement
			// plutôt que de renvoyer une liste vide qui ressemble à « aucune donnée ».
			return nil, connect.NewError(
				connect.CodeNotFound,
				fmt.Errorf("colonne %q absente de %s.%s",
					column, req.Msg.GetSchema(), req.Msg.GetTable()),
			)
		}
		if raw == nil {
			values = append(values, &mgmtv1alpha1.ColumnSampleValue{IsNull: true})
			continue
		}
		values = append(values, &mgmtv1alpha1.ColumnSampleValue{
			Value: truncateRunes(valueToText(raw), maxSampleValueRunes),
		})
	}

	return connect.NewResponse(&mgmtv1alpha1.GetColumnSampleValuesResponse{
		Values: values,
	}), nil
}
