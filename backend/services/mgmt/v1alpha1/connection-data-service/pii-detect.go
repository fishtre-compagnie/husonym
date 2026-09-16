package v1alpha1_connectiondataservice

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

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
	// sampleTimeout borne la lecture de l'échantillon. 20 lignes doivent revenir
	// en quelques millisecondes ; passé ce délai, le problème est ailleurs (table
	// verrouillée, base saturée, réseau) et insister ne sert à rien.
	sampleTimeout = 15 * time.Second
)

// rowCollector gathers the sampled rows (gob-encoded) in memory.
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

	// A deadline of its own for sampling: past it, the error is explicit and
	// actionable. Without it the client gives up first, and the server only reports
	// a "context canceled" that the UI surfaces as an opaque HTTP 500.
	sampleCtx, cancelSample := context.WithTimeout(ctx, sampleTimeout)
	defer cancelSample()

	collector := &rowCollector{}
	if err := dataconn.SampleData(
		sampleCtx,
		collector,
		req.Msg.GetSchema(),
		req.Msg.GetTable(),
		uint(sampleSize),
	); err != nil {
		if errors.Is(sampleCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
			return nil, connect.NewError(connect.CodeDeadlineExceeded, fmt.Errorf(
				"l'échantillonnage de %s.%s a dépassé %s : table volumineuse ou base surchargée. "+
					"Décochez cette table ou relancez le scan hors période de charge",
				req.Msg.GetSchema(), req.Msg.GetTable(), sampleTimeout,
			))
		}
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
				MatchCount:                 sampleCount(values),
				SampledCount:               sampleCount(values),
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
					MatchCount:   sampleCount(values),
					SampledCount: sampleCount(values),
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

		// Colonnes hors de portée du NER : ni nom, ni lieu, ni texte libre ne peut
		// s'y cacher. Les identifiants à clé de contrôle (NIR, IBAN, carte) sont
		// déjà traités par l'étage 1, il ne reste ici que des entiers, des montants
		// et des booléens. Les écarter est décisif pour la tenue en charge : sur une
		// base métier réelle, l'essentiel des colonnes est de cette nature, et
		// chacune coûtait 20 appels HTTP à Presidio pour un résultat toujours vide.
		if !isAnalyzableText(values) {
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
		// Presidio ne distingue ni prénom/nom/nom complet (tout est PERSON), ni
		// ville/adresse (tout est LOCATION). On tranche sur la forme des valeurs,
		// sinon une colonne d'adresses se voyait suggérer Generate City.
		suggestion.Category, suggestion.Suggested = piidetect.RefineByValues(
			suggestion.Category, suggestion.Suggested, values,
		)
		detections = append(detections, &mgmtv1alpha1.ColumnPiiDetection{
			Schema:                     req.Msg.GetSchema(),
			Table:                      req.Msg.GetTable(),
			Column:                     col,
			EntityType:                 entity,
			Score:                      float32(avgScore),
			SuggestedTransformerSource: suggestion.Suggested,
			IsSensitive:                suggestion.Sensitive,
			MatchCount:                 clampUint32(matchCount),
			SampledCount:               sampleCount(values),
			DataCategory:               suggestion.Category,
			PiiConfidence:              mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_NEEDS_REVIEW,
			PiiDetectionMethod:         mgmtv1alpha1.PiiDetectionMethod_PII_DETECTION_METHOD_CONTENT,
			//nolint:misspell // message produit, rédigé en français
			PiiEvidence: fmt.Sprintf("%s reconnu par analyse de contenu sur %d/%d valeurs (score moyen %.2f)",
				entity, matchCount, len(values), avgScore),
		})
	}

	return connect.NewResponse(&mgmtv1alpha1.DetectPiiInConnectionDataResponse{
		Detections: detections,
	}), nil
}

// analyzeColumn examines each value on its own (NER recognizes an isolated
// name/place better than one buried in a list) and returns the dominant entity
// among the mappable ones, its mean score, and how many values carry it.
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

// sampleCount convertit une taille d'échantillon vers le type du proto. La borne
// est explicite : l'échantillon vaut quelques dizaines de valeurs, mais une
// conversion nue depuis un int laisserait un dépassement possible sans le dire.
func sampleCount(values []string) uint32 { return clampUint32(len(values)) }

// clampUint32 ramène un compteur positif dans les bornes du type du proto.
func clampUint32(n int) uint32 {
	if n < 0 {
		return 0
	}
	if n > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(n)
}

func truncateRunes(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit])
}

// nonTextualRe reconnaît une valeur dépourvue de contenu langagier : nombre
// (entier, décimal, signé), booléen, ou horodatage déjà écarté par l'étage 2.
var nonTextualRe = regexp.MustCompile(`^[+-]?\d+([.,]\d+)?$`)

// base64ishRe : suite continue de caractères de l'alphabet base64 / base64url.
// Le point et l'arobase en sont absents, ce qui laisse passer emails et URLs.
var base64ishRe = regexp.MustCompile(`^[A-Za-z0-9+/=_-]+$`)

// minBlobLen : en dessous, une chaîne compacte reste plausible comme mot réel
// (« Saint-Étienne », « Jean-Pierre »). Au-dessus, aucun mot de langue naturelle.
const minBlobLen = 32

// looksLikeOpaqueBlob reconnaît une donnée technique opaque : signature, jeton,
// hash, payload compressé, blob base64.
//
// Rencontré en base de production : une colonne de signatures zlib+base64
// (« eJztWPdXk1m0jc… ») que Presidio classait « ville » avec assez de valeurs
// concordantes pour franchir le seuil. Sans ce filtre, ces colonnes produisent un
// badge RGPD sur une donnée qui n'a rien de personnel — et chacune coûte un appel
// HTTP par valeur, sur du contenu tronqué qui ne veut rien dire.
// Le verdict se prend fragment par fragment, et non sur la chaîne entière : le
// base64 stocké en base est souvent replié en lignes de 76 caractères, si bien
// qu'un test sur la valeur complète échoue sur le premier saut de ligne. Découper
// protège aussi le texte libre — « Client Jean Dupont joignable au 0710203040 »
// forme, blancs retirés, une suite parfaitement conforme à l'alphabet base64,
// alors que ses MOTS sont courts. Exiger que CHAQUE fragment soit long sépare les
// deux sans ambiguïté.
func looksLikeOpaqueBlob(v string) bool {
	if len(v) < minBlobLen {
		return false
	}
	fields := strings.Fields(v)
	if len(fields) == 0 {
		return false
	}
	total := 0
	for _, f := range fields {
		if !base64ishRe.MatchString(f) {
			return false
		}
		total += len(f)
	}
	// C'est la LONGUEUR MOYENNE des fragments qui sépare les deux cas, et non
	// l'alphabet : les mots d'une phrase y sont eux aussi conformes. Un base64
	// replié donne des lignes de 76 caractères — la dernière est souvent courte,
	// d'où la moyenne plutôt qu'un minimum sur chaque fragment. Le mot français le
	// plus long reste très en dessous du seuil.
	return total/len(fields) >= minBlobLen
}

// isAnalyzableText indique s'il vaut la peine de soumettre la colonne au NER.
//
// Le verdict se prend sur la COLONNE, pas sur les valeurs une à une : une colonne
// est homogène, et une seule valeur textuelle parmi des nombres est plus
// probablement une anomalie de saisie qu'une PII. On exige donc qu'une part
// significative des valeurs porte du texte.
func isAnalyzableText(values []string) bool {
	textual := 0
	total := 0
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		total++
		switch strings.ToLower(v) {
		case "true", "false", "t", "f", "0", "1", "oui", "non", "y", "n":
			continue
		}
		if nonTextualRe.MatchString(v) || looksLikeOpaqueBlob(v) {
			continue
		}
		// Une valeur sans aucune lettre (référence « 12-AB »… non, celle-ci en a)
		// n'apporte rien au NER, qui raisonne sur des mots.
		if !strings.ContainsFunc(v, unicode.IsLetter) {
			continue
		}
		textual++
	}
	if total == 0 {
		return false
	}
	return textual*2 > total
}

// dateCategory retourne la catégorie affichée pour une colonne de dates.
func dateCategory(sensitive bool) string {
	if sensitive {
		return "birth_date"
	}
	return "date"
}
