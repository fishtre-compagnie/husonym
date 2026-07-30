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
	defaultScoreThresh   = 0.35 // PHONE_NUMBER sort ~0.40 chez Presidio -> seuil bas
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

		// ÉTAGE 1 — validation déterministe, AVANT tout appel à Presidio.
		// Ce qui se prouve par une clé de contrôle n'a pas à être deviné par un
		// modèle : c'est plus fiable (Presidio classe un NIR en CREDIT_CARD avec
		// un score de 1.00 quand celui-ci passe Luhn par hasard) et ça évite un
		// appel HTTP par valeur.
		if cc, ok := piidetect.ClassifyValues(values, ""); ok {
			detections = append(detections, &mgmtv1alpha1.ColumnPiiDetection{
				Schema:                     req.Msg.GetSchema(),
				Table:                      req.Msg.GetTable(),
				Column:                     col,
				EntityType:                 strings.ToUpper(cc.Category),
				Score:                      1,
				SuggestedTransformerSource: cc.Suggested,
				IsSensitive:                cc.Sensitive,
				MatchCount:                 uint32(len(values)),
				SampledCount:               uint32(len(values)),
				DataCategory:               cc.Category,
				PiiConfidence:              cc.Confidence,
				PiiDetectionMethod:         cc.Method,
				PiiEvidence:                cc.Evidence,
			})
			continue
		}

		// ÉTAGE 2 — dates stockées en texte : le format s'infère, il ne se devine
		// pas. Une colonne de dates n'est personnelle que si son nom l'indique.
		if df, ok := piidetect.DetectDateFormat(values); ok {
			sensitive := piidetect.IsBirthDateName(col)
			confidence := mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_CONFIRMED
			if df.Ambiguous {
				confidence = mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_NEEDS_REVIEW
			}
			if sensitive || df.Ambiguous {
				detections = append(detections, &mgmtv1alpha1.ColumnPiiDetection{
					Schema:       req.Msg.GetSchema(),
					Table:        req.Msg.GetTable(),
					Column:       col,
					EntityType:   "DATE",
					Score:        1,
					IsSensitive:  sensitive,
					MatchCount:   uint32(len(values)),
					SampledCount: uint32(len(values)),
					DataCategory: dateCategory(sensitive),
					// Le transformer reste au choix de l'utilisateur : aucun
					// générateur ne sait restituer la date dans le format source.
					SuggestedTransformerSource: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED,
					PiiConfidence:              confidence,
					PiiDetectionMethod:         mgmtv1alpha1.PiiDetectionMethod_PII_DETECTION_METHOD_FORMAT,
					PiiEvidence:                df.Evidence,
				})
			}
			continue
		}

		// ÉTAGE 3 — Presidio en dernier recours, sur ce qui n'est pas décidable
		// autrement : noms de personnes, lieux, texte libre. Résultat toujours
		// marqué NEEDS_REVIEW, un modèle statistique ne prouve rien.
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
			DataCategory:               suggestion.Category,
			PiiConfidence:              mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_NEEDS_REVIEW,
			PiiDetectionMethod:         mgmtv1alpha1.PiiDetectionMethod_PII_DETECTION_METHOD_CONTENT,
			PiiEvidence: fmt.Sprintf("%s reconnu par analyse de contenu sur %d/%d valeurs (score moyen %.2f)",
				entity, matchCount, len(values), avgScore),
		})
	}

	return connect.NewResponse(&mgmtv1alpha1.DetectPiiInConnectionDataResponse{
		Detections: detections,
	}), nil
}

// analyzeColumn analyse chaque valeur individuellement (le NER reconnaît mieux un
// nom/lieu isolé qu'au sein d'une liste) et retourne l'entité dominante parmi les
// entités mappables, son score moyen et le nombre de valeurs où elle apparaît.
func (s *Service) analyzeColumn(
	ctx context.Context,
	values []string,
	threshold float64,
	language string,
	logger interface{ Warn(string, ...any) },
) (entity string, avgScore float64, matchCount int, ok bool) {
	type agg struct {
		count    int
		scoreSum float64
	}
	byEntity := map[string]*agg{}
	analyzed := 0
	for _, v := range values {
		text := truncateRunes(v, maxValueRunes)
		if strings.TrimSpace(text) == "" {
			continue
		}
		analyzed++
		results, err := s.analyze.Analyze(ctx, presidio.AnalyzeRequest{
			Text:           text,
			Language:       language,
			ScoreThreshold: threshold,
		})
		if err != nil {
			logger.Warn(fmt.Sprintf("presidio analyze failed: %v", err))
			continue
		}
		// Meilleur score par entité DANS cette valeur (on compte des VALEURS, pas des spans).
		bestPerEntity := map[string]float64{}
		for _, r := range results {
			if sc, seen := bestPerEntity[r.EntityType]; !seen || r.Score > sc {
				bestPerEntity[r.EntityType] = r.Score
			}
		}
		for e, sc := range bestPerEntity {
			a := byEntity[e]
			if a == nil {
				a = &agg{}
				byEntity[e] = a
			}
			a.count++
			a.scoreSum += sc
		}
	}
	if analyzed == 0 {
		return "", 0, 0, false
	}

	// Entité dominante = présente dans le plus de VALEURS, PARMI les entités
	// mappables vers un transformer. On ignore le bruit non exploitable
	// (URL, DATE_TIME...). Départage par nom d'entité pour un résultat déterministe.
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

// dateCategory retourne la catégorie affichée pour une colonne de dates.
func dateCategory(sensitive bool) string {
	if sensitive {
		return "birth_date"
	}
	return "date"
}
