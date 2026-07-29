package v1alpha1_connectiondataservice

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"sort"
	"strings"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/backend/pkg/piidetect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
)

const (
	defaultSampleSize    = 20
	defaultScoreThresh   = 0.5
	defaultLanguage      = "en"
	maxValueRunes        = 200 // tronque les longues valeurs envoyées à Presidio
	minMatchesFloor      = 2   // faux positif improbable au-delà de ce plancher
	matchRatioNumerator  = 1
	matchRatioDenominato = 3 // une entité doit couvrir ~1/3 des valeurs échantillonnées
)

// rowCollector accumule les lignes échantillonnées (gob) en mémoire.
type rowCollector struct {
	rows [][]byte
}

func (c *rowCollector) Send(resp *mgmtv1alpha1.GetConnectionDataStreamResponse) error {
	// Copie défensive : le buffer sous-jacent peut être réutilisé par l'appelant.
	b := make([]byte, len(resp.GetRowBytes()))
	copy(b, resp.GetRowBytes())
	c.rows = append(c.rows, b)
	return nil
}

// DetectPiiInConnectionData échantillonne le contenu des colonnes d'une table et
// l'analyse via Presidio pour détecter des données personnelles (scan approfondi).
func (s *Service) DetectPiiInConnectionData(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.DetectPiiInConnectionDataRequest],
) (*connect.Response[mgmtv1alpha1.DetectPiiInConnectionDataResponse], error) {
	logger := logger_interceptor.GetLoggerFromContextOrDefault(ctx)

	if s.analyze == nil || s.cfg == nil || !s.cfg.IsPresidioEnabled {
		return nil, connect.NewError(
			connect.CodeFailedPrecondition,
			errors.New(
				"le scan de contenu PII nécessite Presidio, qui n'est pas configuré (définir PRESIDIO_ANALYZER_URL)",
			),
		)
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

	sampleSize := req.Msg.GetSampleSize()
	if sampleSize == 0 {
		sampleSize = defaultSampleSize
	}

	collector := &rowCollector{}
	if err := dataconn.SampleData(
		ctx,
		collector,
		req.Msg.GetSchema(),
		req.Msg.GetTable(),
		uint(sampleSize),
	); err != nil {
		return nil, fmt.Errorf("unable to sample data for pii scan: %w", err)
	}

	// Filtre optionnel sur un sous-ensemble de colonnes.
	var wanted map[string]struct{}
	if cols := req.Msg.GetColumns(); len(cols) > 0 {
		wanted = make(map[string]struct{}, len(cols))
		for _, c := range cols {
			wanted[c] = struct{}{}
		}
	}

	// Regroupe les valeurs par colonne (ordre stable des colonnes).
	colValues := map[string][]string{}
	var colOrder []string
	for _, rowbytes := range collector.rows {
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
			text := valueToText(v)
			if text == "" {
				continue
			}
			if _, seen := colValues[col]; !seen {
				colOrder = append(colOrder, col)
			}
			colValues[col] = append(colValues[col], text)
		}
	}
	sort.Strings(colOrder)

	threshold := float64(req.Msg.GetScoreThreshold())
	if threshold <= 0 {
		threshold = defaultScoreThresh
	}
	language := req.Msg.GetLanguage()
	if language == "" {
		if s.cfg.PresidioDefaultLanguage != nil && *s.cfg.PresidioDefaultLanguage != "" {
			language = *s.cfg.PresidioDefaultLanguage
		} else {
			language = defaultLanguage
		}
	}

	detections := make([]*mgmtv1alpha1.ColumnPiiDetection, 0, len(colOrder))
	for _, col := range colOrder {
		values := colValues[col]
		if len(values) == 0 {
			continue
		}
		entity, avgScore, matchCount, ok := s.analyzeColumn(ctx, values, threshold, language, logger)
		if !ok {
			continue
		}
		// Une entité doit couvrir une fraction suffisante des valeurs.
		minMatches := len(values) * matchRatioNumerator / matchRatioDenominato
		if minMatches < minMatchesFloor {
			minMatches = minMatchesFloor
		}
		if matchCount < minMatches {
			continue
		}
		suggestion, ok := piidetect.SuggestionForEntity(entity, "")
		if !ok {
			continue
		}
		detections = append(detections, &mgmtv1alpha1.ColumnPiiDetection{
			Schema:                     req.Msg.GetSchema(),
			Table:                      req.Msg.GetTable(),
			Column:                     col,
			EntityType:                 entity,
			Score:                      float32(avgScore),
			SuggestedTransformerSource: suggestion.Suggested,
			IsSensitive:                suggestion.Sensitive,
			MatchCount:                 uint32(matchCount),
			SampledCount:               uint32(len(values)),
		})
	}

	return connect.NewResponse(&mgmtv1alpha1.DetectPiiInConnectionDataResponse{
		Detections: detections,
	}), nil
}

// analyzeColumn envoie les valeurs concaténées d'une colonne à Presidio et
// retourne l'entité dominante, son score moyen et le nombre d'occurrences.
func (s *Service) analyzeColumn(
	ctx context.Context,
	values []string,
	threshold float64,
	language string,
	logger interface{ Warn(string, ...any) },
) (entity string, avgScore float64, matchCount int, ok bool) {
	parts := make([]string, 0, len(values))
	for _, v := range values {
		parts = append(parts, truncateRunes(v, maxValueRunes))
	}
	text := strings.Join(parts, "\n")
	if strings.TrimSpace(text) == "" {
		return "", 0, 0, false
	}

	results, err := s.analyze.Analyze(ctx, presidio.AnalyzeRequest{
		Text:           text,
		Language:       language,
		ScoreThreshold: threshold,
	})
	if err != nil {
		logger.Warn(fmt.Sprintf("presidio analyze failed: %v", err))
		return "", 0, 0, false
	}

	type agg struct {
		count    int
		scoreSum float64
	}
	byEntity := map[string]*agg{}
	for _, r := range results {
		a := byEntity[r.EntityType]
		if a == nil {
			a = &agg{}
			byEntity[r.EntityType] = a
		}
		a.count++
		a.scoreSum += r.Score
	}
	// Entité dominante = plus grand nombre d'occurrences PARMI les entités mappables
	// vers un transformer. On ignore le bruit non exploitable (URL, DATE_TIME...) qui
	// sinon supplanterait EMAIL_ADDRESS (Presidio émet aussi des URL sur les emails).
	// Départage par nom d'entité pour un résultat déterministe.
	best := ""
	for e, a := range byEntity {
		if _, ok := piidetect.SuggestionForEntity(e, ""); !ok {
			continue
		}
		if best == "" ||
			a.count > byEntity[best].count ||
			(a.count == byEntity[best].count && e < best) {
			best = e
		}
	}
	if best == "" {
		return "", 0, 0, false
	}
	a := byEntity[best]
	return best, a.scoreSum / float64(a.count), a.count, true
}

func valueToText(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	case []byte:
		return strings.TrimSpace(string(t))
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", t))
	}
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
