package v1alpha1_connectiondataservice

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"connectrpc.com/connect"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/internal/connectiondata"
)

// sampleBound is how long one read of the sample may take: the service's own bound when it has
// one, sampleTimeout otherwise.
func (s *Service) sampleBound() time.Duration {
	if s.sampleTimeout > 0 {
		return s.sampleTimeout
	}
	return sampleTimeout
}

// sampledValues draws the values the scan reads, per column.
//
// Without column names, whole rows of the table are sampled. With names, each column is sampled
// on its own, so that a column filled in few rows still gives values; a connection that cannot
// sample a column (anything but SQL) is sampled by rows, keeping the named columns.
//
// A deadline of its own bounds each read: past it, the error is explicit and actionable. Without
// it the client gives up first, and the server only reports a "context canceled" that the UI
// surfaces as an opaque HTTP 500.
func (s *Service) sampledValues(
	ctx context.Context,
	dataconn connectiondata.ConnectionDataService,
	schema, table string,
	columns []string,
	sampleSize uint,
) (map[string][]string, error) {
	wanted := wantedColumns(columns)
	if wanted == nil {
		return s.sampledRowValues(ctx, dataconn, schema, table, sampleSize, nil)
	}

	values := make(map[string][]string, len(wanted))
	for _, column := range slices.Sorted(maps.Keys(wanted)) {
		sampled, err := s.sampledColumnValues(ctx, dataconn, schema, table, column, sampleSize)
		if errors.Is(err, errors.ErrUnsupported) {
			return s.sampledRowValues(ctx, dataconn, schema, table, sampleSize, wanted)
		}
		if err != nil {
			return nil, err
		}
		maps.Copy(values, sampled)
	}
	return values, nil
}

// wantedColumns is the set of the named columns, nil when none is named.
func wantedColumns(columns []string) map[string]struct{} {
	if len(columns) == 0 {
		return nil
	}
	wanted := make(map[string]struct{}, len(columns))
	for _, column := range columns {
		wanted[column] = struct{}{}
	}
	return wanted
}

// sampledRowValues samples whole rows of the table and groups their values by column, keeping the
// wanted columns only (all of them when wanted is nil).
func (s *Service) sampledRowValues(
	ctx context.Context,
	dataconn connectiondata.ConnectionDataService,
	schema, table string,
	sampleSize uint,
	wanted map[string]struct{},
) (map[string][]string, error) {
	sampleCtx, cancel := context.WithTimeout(ctx, s.sampleBound())
	defer cancel()

	collector := &rowCollector{}
	if err := dataconn.SampleData(sampleCtx, collector, schema, table, sampleSize); err != nil {
		if errors.Is(sampleCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
			return nil, connect.NewError(connect.CodeDeadlineExceeded, fmt.Errorf(
				"l'échantillonnage de %s.%s a dépassé %s : table volumineuse ou base surchargée. "+
					"Décochez cette table ou relancez le scan hors période de charge",
				schema, table, s.sampleBound(),
			))
		}
		return nil, fmt.Errorf("unable to sample data for pii scan: %w", err)
	}
	return groupSampledRows(ctx, collector.rows, wanted), nil
}

// sampledColumnValues samples the filled values of one column, for no longer than the sample bound.
func (s *Service) sampledColumnValues(
	ctx context.Context,
	dataconn connectiondata.ConnectionDataService,
	schema, table, column string,
	sampleSize uint,
) (map[string][]string, error) {
	sampleCtx, cancel := context.WithTimeout(ctx, s.sampleBound())
	defer cancel()

	collector := &rowCollector{}
	if err := dataconn.SampleColumn(sampleCtx, collector, schema, table, column, sampleSize); err != nil {
		if errors.Is(sampleCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
			return nil, connect.NewError(connect.CodeDeadlineExceeded, fmt.Errorf(
				"l'échantillonnage de la colonne %q de %s.%s a dépassé %s : table volumineuse ou base surchargée. "+
					"Décochez cette colonne ou relancez le scan hors période de charge",
				column, schema, table, s.sampleBound(),
			))
		}
		if errors.Is(err, errors.ErrUnsupported) || connect.CodeOf(err) == connect.CodeNotFound {
			return nil, err
		}
		return nil, fmt.Errorf("unable to sample column %q for pii scan: %w", column, err)
	}
	return groupSampledRows(ctx, collector.rows, map[string]struct{}{column: {}}), nil
}

// groupSampledRows decodes the sampled rows and groups the text of their values by column, the
// empty ones dropped. wanted limits the columns kept; nil keeps them all.
func groupSampledRows(ctx context.Context, rows [][]byte, wanted map[string]struct{}) map[string][]string {
	logger := logger_interceptor.GetLoggerFromContextOrDefault(ctx)
	values := map[string][]string{}
	for _, rowbytes := range rows {
		row := map[string]any{}
		if err := gob.NewDecoder(bytes.NewReader(rowbytes)).Decode(&row); err != nil {
			logger.Warn(fmt.Sprintf("skipping undecodable sampled row: %v", err))
			continue
		}
		for col, v := range row {
			if wanted != nil {
				if _, ok := wanted[col]; !ok {
					continue
				}
			}
			if text := valueToText(v); text != "" {
				values[col] = append(values[col], text)
			}
		}
	}
	return values
}
