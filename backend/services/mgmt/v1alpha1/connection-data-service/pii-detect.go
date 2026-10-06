package v1alpha1_connectiondataservice

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/backend/pkg/piidetect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
	"google.golang.org/protobuf/proto"
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

	colValues, err := s.sampledValues(
		ctx, dataconn, req.Msg.GetSchema(), req.Msg.GetTable(), req.Msg.GetColumns(), uint(sampleSize),
	)
	if err != nil {
		return nil, err
	}

	// Type SQL de chaque colonne : il décide de la variante du transformer suggéré
	// (un téléphone en BIGINT veut GENERATE_INT64_PHONE_NUMBER, pas la variante
	// chaîne, qui ferait échouer la synchronisation). Son absence ne justifie pas
	// de refuser le scan : la suggestion retombe alors sur la variante chaîne.
	columnTypes := map[string]string{}
	tableColumns, terr := dataconn.GetTableSchema(ctx, req.Msg.GetSchema(), req.Msg.GetTable())
	if terr != nil {
		logger.Warn(fmt.Sprintf("unable to read the column types of %s.%s, suggesting string transformers: %v",
			req.Msg.GetSchema(), req.Msg.GetTable(), terr))
	} else {
		for _, column := range tableColumns {
			columnTypes[column.GetColumn()] = column.GetDataType()
		}
	}

	// Filtre optionnel sur un sous-ensemble de colonnes.
	wanted := wantedColumns(req.Msg.GetColumns())

	// Ordre stable des colonnes.
	colOrder := slices.Sorted(maps.Keys(colValues))

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

	content := &contentAnalysis{
		analyze: s.analyze, threshold: threshold, language: language, logger: logger,
		notAnalyzed: map[string]struct{}{},
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
		if cc, ok := piidetect.ClassifyValues(values, columnTypes[col]); ok {
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
					Schema:                     req.Msg.GetSchema(),
					Table:                      req.Msg.GetTable(),
					Column:                     col,
					EntityType:                 "DATE",
					Score:                      1,
					IsSensitive:                sensitive,
					MatchCount:                 sampleCount(values),
					SampledCount:               sampleCount(values),
					DataCategory:               dateCategory(sensitive),
					SuggestedTransformerSource: dateSuggestion(sensitive, columnTypes[col]),
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
		// A caller that gave up is not answered a scan cut short as if it were whole.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		told, ok := content.detect(ctx, col, values)
		if !ok {
			continue
		}
		if told.freeText {
			detections = append(detections,
				freeTextDetection(req.Msg.GetSchema(), req.Msg.GetTable(), col, values, told))
			continue
		}
		entity, avgScore, matchCount := told.entity, told.avgScore, told.matchCount
		suggestion, ok := piidetect.SuggestionForEntity(entity, columnTypes[col])
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
		Verdicts: verdicts(
			req.Msg.GetSchema(), req.Msg.GetTable(), tableColumns, wanted, detections, content.notAnalyzed,
		),
	}), nil
}

// verdicts reconciles each column's name with what the scan found in it: every column of the
// table's schema, and every column the scan found something in. The two lists should agree, but
// the schema can come back empty or fail, and a sampled column can be missing from it; a
// detection with no verdict would vanish from the screen while the scan still counts it. Such a
// column's name is still read, only without its type.
//
// A column whose content could not be analyzed is told so: its verdict, from its name alone,
// says nothing of what it holds.
func verdicts(
	schema, table string,
	tableColumns []*mgmtv1alpha1.DatabaseColumn,
	wanted map[string]struct{},
	detections []*mgmtv1alpha1.ColumnPiiDetection,
	notAnalyzed map[string]struct{},
) []*mgmtv1alpha1.ColumnPiiVerdict {
	byColumn := make(map[string]*mgmtv1alpha1.ColumnPiiDetection, len(detections))
	for _, detection := range detections {
		byColumn[detection.GetColumn()] = detection
	}

	columns := slices.Clone(tableColumns)
	inSchema := make(map[string]bool, len(tableColumns))
	for _, column := range tableColumns {
		inSchema[column.GetColumn()] = true
	}
	for _, detection := range detections {
		if !inSchema[detection.GetColumn()] {
			columns = append(columns, &mgmtv1alpha1.DatabaseColumn{
				Schema: schema, Table: table, Column: detection.GetColumn(),
			})
		}
	}
	for _, column := range slices.Sorted(maps.Keys(notAnalyzed)) {
		if !inSchema[column] {
			columns = append(columns, &mgmtv1alpha1.DatabaseColumn{Schema: schema, Table: table, Column: column})
		}
	}

	out := make([]*mgmtv1alpha1.ColumnPiiVerdict, 0, len(columns))
	for _, column := range columns {
		if wanted != nil {
			if _, ok := wanted[column.GetColumn()]; !ok {
				continue
			}
		}
		// A copy: the name-based detection is written onto the column, which is not ours.
		named := proto.CloneOf(column)
		piidetect.Enrich([]*mgmtv1alpha1.DatabaseColumn{named})
		verdict := piidetect.Reconcile(named, byColumn[column.GetColumn()])
		_, verdict.ContentNotAnalyzed = notAnalyzed[column.GetColumn()]
		out = append(out, verdict)
	}
	return out
}

// contentAnalysis analyzes the content of the columns of one table, and keeps which of them
// it could not analyze: a column the analyzer failed on is not one without personal data.
type contentAnalysis struct {
	analyze   presidio.Analyzer
	threshold float64
	language  string
	logger    interface{ Warn(string, ...any) }

	// silent says the analyzer did not answer: it is not asked again for this table, whose
	// other columns it would each be waited for.
	silent bool
	// notAnalyzed holds the columns whose content could not be analyzed.
	notAnalyzed map[string]struct{}
}

// detect tells what the analyzer finds in the values of a column, by the rule of free text for a
// free-text column and by the rule of the third for any other (see column and freeText).
func (a *contentAnalysis) detect(ctx context.Context, name string, values []string) (columnVerdict, bool) {
	if piidetect.IsFreeText(values) {
		return a.freeText(ctx, name, values)
	}
	entity, avgScore, matchCount, ok := a.column(ctx, name, values)
	return columnVerdict{entity: entity, avgScore: avgScore, matchCount: matchCount}, ok
}

// examine analyzes the values of a column, once. ok is false when the column could not be
// analyzed, which is kept.
func (a *contentAnalysis) examine(
	ctx context.Context,
	name string,
	values []string,
) (found columnEntity, refused error, ok bool) {
	if a.silent {
		a.notAnalyzed[name] = struct{}{}
		return columnEntity{}, nil, false
	}
	found, refused, err := analyzeColumn(ctx, a.analyze, values, a.threshold, a.language)
	if err != nil {
		// Why is logged, and not told: the error of the analyzer can quote where it is
		// reached, and the value it read.
		a.logger.Warn(fmt.Sprintf("presidio did not answer on column %q: %v", name, err))
		a.notAnalyzed[name] = struct{}{}
		a.silent = true
		return columnEntity{}, nil, false
	}
	return found, refused, true
}

// keepRefused keeps a column the analyzer refused values of, and told nothing, as not analyzed.
func (a *contentAnalysis) keepRefused(name string, refused error) {
	if refused != nil {
		a.logger.Warn(fmt.Sprintf("presidio refused values of column %q: %v", name, refused))
		a.notAnalyzed[name] = struct{}{}
	}
}

// column tells what the analyzer finds in the values of a column: the dominant entity, its
// mean score and how many values carry it. ok is false when no entity covers enough of the
// values, and when the column could not be analyzed, which is kept.
//
// A column some values of which the analyzer refused is told by the values it took, when they
// are enough to find an entity: what was found stands. When they are not, the column is not
// told empty of personal data: the values refused may be the ones that hold some.
func (a *contentAnalysis) column(
	ctx context.Context,
	name string,
	values []string,
) (entity string, avgScore float64, matchCount int, ok bool) {
	found, refused, ok := a.examine(ctx, name, values)
	if !ok {
		return "", 0, 0, false
	}
	// Une entité doit couvrir une fraction suffisante des valeurs.
	if found.entity != "" && found.matchCount >= minMatches(len(values)) {
		return found.entity, found.avgScore, found.matchCount, true
	}
	a.keepRefused(name, refused)
	return "", 0, 0, false
}

// freeText is column for a free-text column, which names and places hide in a few values of: it
// is told from piidetect.FreeTextMinMatches values holding a sensitive entity, whatever the
// entities are and the size of the sample. The entity of the verdict is the sensitive one found
// in the most values.
func (a *contentAnalysis) freeText(ctx context.Context, name string, values []string) (columnVerdict, bool) {
	found, refused, ok := a.examine(ctx, name, values)
	if !ok {
		return columnVerdict{}, false
	}
	if found.sensitive.values >= piidetect.FreeTextMinMatches {
		return columnVerdict{
			entity:     found.sensitive.entity,
			avgScore:   found.sensitive.avgScore,
			matchCount: found.sensitive.values,
			names:      found.sensitive.names,
			freeText:   true,
		}, true
	}
	a.keepRefused(name, refused)
	return columnVerdict{}, false
}

// columnVerdict is what the analyzer tells of a column: an entity, its mean score and how many
// values carry it. For a free-text column, the entity is the sensitive one found in the most
// values, matchCount the number of values holding any sensitive entity, and names those entities,
// sorted.
type columnVerdict struct {
	entity     string
	avgScore   float64
	matchCount int
	names      []string
	freeText   bool
}

// freeTextDetection is the detection of a free-text column found to hold personal data.
func freeTextDetection(
	schema, table, column string,
	values []string,
	told columnVerdict,
) *mgmtv1alpha1.ColumnPiiDetection {
	return &mgmtv1alpha1.ColumnPiiDetection{
		Schema:                     schema,
		Table:                      table,
		Column:                     column,
		EntityType:                 told.entity,
		Score:                      float32(told.avgScore),
		SuggestedTransformerSource: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_PII_TEXT,
		IsSensitive:                true,
		MatchCount:                 clampUint32(told.matchCount),
		SampledCount:               sampleCount(values),
		DataCategory:               piidetect.FreeTextCategory,
		PiiConfidence:              mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_NEEDS_REVIEW,
		PiiDetectionMethod:         mgmtv1alpha1.PiiDetectionMethod_PII_DETECTION_METHOD_CONTENT,
		PiiEvidence: fmt.Sprintf("texte libre : %d/%d valeurs contiennent %s",
			told.matchCount, len(values), strings.Join(told.names, ", ")),
	}
}

// minMatches is how many of the values sampled an entity must cover to be told of a column.
func minMatches(sampled int) int {
	return max(sampled*matchRatioNumerator/matchRatioDenominato, minMatchesFloor)
}

// refusedValuesLimit is how many values of a column the analyzer may refuse before the
// column is given up: past it, the analyzer fails on the column, not on a value.
const refusedValuesLimit = 2

// columnEntity is the dominant entity of a column, among the mappable ones: its mean score,
// and how many values carry it. Its entity is empty when the analyzer found none.
type columnEntity struct {
	entity     string
	avgScore   float64
	matchCount int
	// sensitive is what the values hold of personal data, for the rule of free text.
	sensitive sensitiveValues
}

// sensitiveValues is what a column holds of the entities known as personal data: how many
// values hold at least one of them, the one found in the most values with its mean score, and
// the names of all of them, sorted.
type sensitiveValues struct {
	values   int
	entity   string
	avgScore float64
	names    []string
}

// analyzeTimeout is how long the analyzer is waited for, for one sampled value. A value is
// short, and analyzed in milliseconds: past this, the analyzer is not answering, and the scan
// that asks it would wait without end.
const analyzeTimeout = 15 * time.Second

// analyzeValue asks the analyzer about one sampled value, for no longer than analyzeTimeout.
func analyzeValue(
	ctx context.Context,
	analyzer presidio.Analyzer,
	request *presidio.AnalyzeRequest,
) ([]presidio.Finding, error) {
	ctx, cancel := context.WithTimeout(ctx, analyzeTimeout)
	defer cancel()
	return analyzer.Analyze(ctx, request)
}

// analyzeColumn examines each value on its own (NER recognizes an isolated
// name/place better than one buried in a list) and returns the dominant entity
// among the mappable ones.
//
// A value the analyzer refuses is asked once more, a refusal that passes being no reason to
// leave it out. One it refuses again is left out, and its refusal returned with what the other
// values showed: past refusedValuesLimit of them, the column is given up. An analyzer that
// does not answer is not asked again, and is the error returned.
func analyzeColumn(
	ctx context.Context,
	analyzer presidio.Analyzer,
	values []string,
	threshold float64,
	language string,
) (found columnEntity, refused, err error) {
	type agg struct {
		count    int
		scoreSum float64
	}
	byEntity := map[string]*agg{}
	refusedValues := 0
	sensitiveValueCount := 0
	for _, v := range values {
		text := truncateRunes(v, maxValueRunes)
		if strings.TrimSpace(text) == "" {
			continue
		}
		request := &presidio.AnalyzeRequest{Text: text, Language: language}
		if threshold > 0 {
			request.ScoreThreshold = &threshold
		}
		results, err := analyzeValue(ctx, analyzer, request)
		if err != nil && !errors.Is(err, presidio.ErrNoAnswer) {
			results, err = analyzeValue(ctx, analyzer, request)
		}
		if errors.Is(err, presidio.ErrNoAnswer) {
			return columnEntity{}, nil, err
		}
		if err != nil {
			refused = err
			refusedValues++
			if refusedValues >= refusedValuesLimit {
				break
			}
			continue
		}
		// Meilleur score par entité DANS cette valeur (on compte des VALEURS, pas des spans).
		bestPerEntity := map[string]float64{}
		for _, r := range results {
			if sc, seen := bestPerEntity[r.EntityType]; !seen || r.Score > sc {
				bestPerEntity[r.EntityType] = r.Score
			}
		}
		holdsSensitive := false
		for e, sc := range bestPerEntity {
			a := byEntity[e]
			if a == nil {
				a = &agg{}
				byEntity[e] = a
			}
			a.count++
			a.scoreSum += sc
			holdsSensitive = holdsSensitive || isSensitiveEntity(e)
		}
		if holdsSensitive {
			sensitiveValueCount++
		}
	}

	sensitive := sensitiveValues{values: sensitiveValueCount}
	for e, a := range byEntity {
		if !isSensitiveEntity(e) {
			continue
		}
		sensitive.names = append(sensitive.names, e)
		if sensitive.entity == "" ||
			a.count > byEntity[sensitive.entity].count ||
			(a.count == byEntity[sensitive.entity].count && e < sensitive.entity) {
			sensitive.entity = e
		}
	}
	slices.Sort(sensitive.names)
	if sensitive.entity != "" {
		best := byEntity[sensitive.entity]
		sensitive.avgScore = best.scoreSum / float64(best.count)
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
		return columnEntity{sensitive: sensitive}, refused, nil
	}
	a := byEntity[best]
	return columnEntity{
		entity: best, avgScore: a.scoreSum / float64(a.count), matchCount: a.count, sensitive: sensitive,
	}, refused, nil
}

// isSensitiveEntity tells whether an entity is one the scan knows, and marks as personal data.
func isSensitiveEntity(entity string) bool {
	suggestion, ok := piidetect.SuggestionForEntity(entity, "")
	return ok && suggestion.Sensitive
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
// dateSuggestion is the transformer suggested for a column of dates: none for a date
// that is not personal data, the one of a birth date for a birth date.
func dateSuggestion(sensitive bool, dataType string) mgmtv1alpha1.TransformerSource {
	if !sensitive {
		return mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED
	}
	return piidetect.SuggestionForBirthDate(dataType)
}

func dateCategory(sensitive bool) string {
	if sensitive {
		return "birth_date"
	}
	return "date"
}
