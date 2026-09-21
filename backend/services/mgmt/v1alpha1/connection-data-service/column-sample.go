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
// This serves doubt resolution: when a detection is flagged "needs review", seeing
// the real data is the most direct way to settle it. Unlike the PII scan, nothing is
// examined here — the values are returned as they are.
func (s *Service) GetColumnSampleValues(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetColumnSampleValuesRequest],
) (*connect.Response[mgmtv1alpha1.GetColumnSampleValuesResponse], error) {
	raws, err := s.sampleColumn(
		ctx,
		req.Msg.GetConnectionId(),
		req.Msg.GetSchema(),
		req.Msg.GetTable(),
		req.Msg.GetColumn(),
		clampLimit(req.Msg.GetLimit(), defaultSampleLimit, maxSampleLimit),
	)
	if err != nil {
		return nil, err
	}

	values := make([]*mgmtv1alpha1.ColumnSampleValue, 0, len(raws))
	for _, raw := range raws {
		values = append(values, toSampleValue(raw))
	}
	return connect.NewResponse(&mgmtv1alpha1.GetColumnSampleValuesResponse{
		Values: values,
	}), nil
}

// sampleColumn reads the first rows of a table and returns one column's values as the driver
// gave them, nil standing for NULL. The raw values are kept, rather than their text, because a
// transformer has to be handed a value of the column's own type.
func (s *Service) sampleColumn(
	ctx context.Context,
	connectionId, schema, table, column string,
	limit uint32,
) ([]any, error) {
	logger := logger_interceptor.GetLoggerFromContextOrDefault(ctx)

	connResp, err := s.connectionService.GetConnection(
		ctx,
		connect.NewRequest(&mgmtv1alpha1.GetConnectionRequest{Id: connectionId}),
	)
	if err != nil {
		return nil, err
	}
	dataconn, err := s.connectiondatabuilder.NewDataConnection(logger, connResp.Msg.GetConnection())
	if err != nil {
		return nil, err
	}

	collector := &rowCollector{}
	if err := dataconn.SampleData(ctx, collector, schema, table, uint(limit)); err != nil {
		return nil, fmt.Errorf("unable to sample column data: %w", err)
	}

	raws := make([]any, 0, len(collector.rows))
	for _, rowbytes := range collector.rows {
		row := map[string]any{}
		if err := gob.NewDecoder(bytes.NewReader(rowbytes)).Decode(&row); err != nil {
			logger.Warn(fmt.Sprintf("skipping undecodable sampled row: %v", err))
			continue
		}
		raw, ok := row[column]
		if !ok {
			// The column is absent from the table: say so plainly rather than return
			// an empty list, which would read as "no data".
			return nil, connect.NewError(
				connect.CodeNotFound,
				fmt.Errorf("colonne %q absente de %s.%s", column, schema, table),
			)
		}
		raws = append(raws, raw)
	}
	return raws, nil
}

// toSampleValue renders a raw value for display.
func toSampleValue(raw any) *mgmtv1alpha1.ColumnSampleValue {
	if raw == nil {
		return &mgmtv1alpha1.ColumnSampleValue{IsNull: true}
	}
	return &mgmtv1alpha1.ColumnSampleValue{
		Value: truncateRunes(valueToText(raw), maxSampleValueRunes),
	}
}

func clampLimit(limit, defaultLimit, maxLimit uint32) uint32 {
	if limit == 0 {
		return defaultLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}
