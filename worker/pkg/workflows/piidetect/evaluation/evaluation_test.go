package evaluation

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/connectiondata"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/model"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/profile"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/report"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/rules"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
)

// The ways a table is scanned. They are the ways a job can be set, on the code a run goes
// through: the activities of the table workflow, fed by the rows of the data set.
const (
	modeRules         = "rules"
	modeModelNames    = "model, names and types"
	modeModelProfiles = "model, profiles"
	modeModelValues   = "model, profiles and values"
	anyReporter       = " + rules"
)

var modelModes = []string{modeModelNames, modeModelProfiles, modeModelValues}

func loadDatasets(t *testing.T) []Dataset {
	t.Helper()
	datasets, err := Load("testdata")
	require.NoError(t, err)
	return datasets
}

// The data set holds eight languages, each with the three kinds of tables and every
// category, and the cases that are hard: personal data under a neutral name, a name that
// sounds personal over data that is not, columns that are empty.
func Test_Dataset(t *testing.T) {
	datasets := loadDatasets(t)
	languages := make([]string, 0, len(datasets))
	for _, dataset := range datasets {
		languages = append(languages, dataset.Language)
		require.Len(t, dataset.Tables, 3, dataset.Language)

		expected := map[string]int{}
		for _, table := range dataset.Tables {
			require.NotEmpty(t, table.Rows(), table.Name)
			for _, column := range table.Columns {
				require.True(t, column.Expected == None || report.Category(column.Expected).Valid(), "%s.%s", table.Name, column.Name)
				expected[column.Expected]++
			}
		}
		for _, category := range report.Categories {
			require.Positive(t, expected[string(category)], "%s: %s", dataset.Language, category)
		}
		require.Greater(t, expected[None], 15, dataset.Language)
	}
	require.Equal(t, []string{"de", "en", "es", "fr", "it", "nl", "pl", "pt"}, languages)
}

func Test_Table_Rows(t *testing.T) {
	table := Table{Columns: []Column{
		{Name: "id", Type: "integer", Values: []any{float64(7), nil}},
		{Name: "born", Type: "date", Values: []any{"1985-03-12", nil}},
		{Name: "at", Type: "timestamp", Values: []any{"2026-10-04T08:30:00Z", "not a moment"}},
		{Name: "price", Type: "numeric", Values: []any{12.5, float64(3)}},
		{Name: "note", Type: "text", Values: []any{"text", ""}},
	}}
	rows := table.Rows()
	require.Len(t, rows, 2)
	require.Equal(t, int64(7), rows[0]["id"])
	require.Equal(t, "1985-03-12", rows[0]["born"].(interface{ Format(string) string }).Format("2006-01-02"))
	require.Equal(t, 8, rows[0]["at"].(interface{ Hour() int }).Hour())
	require.Equal(t, 12.5, rows[0]["price"])
	require.Equal(t, float64(3), rows[1]["price"])
	require.Nil(t, rows[1]["id"])
	require.Equal(t, "not a moment", rows[1]["at"])
}

// database serves the tables of the data sets as the reader of a source does: the
// columns of a table from its catalogue, its rows through the sampling stream.
type database struct {
	tables map[string]Table
}

func newDatabase(datasets []Dataset) *database {
	db := &database{tables: map[string]Table{}}
	for _, dataset := range datasets {
		for _, table := range dataset.Tables {
			db.tables[table.Name+"@"+dataset.Language] = table
		}
	}
	return db
}

func (db *database) GetConnection(
	context.Context,
	*connect.Request[mgmtv1alpha1.GetConnectionRequest],
) (*connect.Response[mgmtv1alpha1.GetConnectionResponse], error) {
	return connect.NewResponse(&mgmtv1alpha1.GetConnectionResponse{Connection: &mgmtv1alpha1.Connection{
		Id: "evaluation",
		ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{
			Config: &mgmtv1alpha1.ConnectionConfig_PgConfig{PgConfig: &mgmtv1alpha1.PostgresConnectionConfig{}},
		},
	}}), nil
}

// builder returns the connection data builder of the database. The schema of a table is
// the language of its data set.
func (db *database) builder(t *testing.T) connectiondata.ConnectionDataBuilder {
	t.Helper()
	data := connectiondata.NewMockConnectionDataService(t)
	data.EXPECT().GetTableSchema(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, schema, name string) ([]*mgmtv1alpha1.DatabaseColumn, error) {
			table, ok := db.tables[name+"@"+schema]
			if !ok {
				return nil, errors.New("no such table")
			}
			columns := make([]*mgmtv1alpha1.DatabaseColumn, 0, len(table.Columns))
			for _, column := range table.Columns {
				columns = append(columns, &mgmtv1alpha1.DatabaseColumn{
					Schema: schema, Table: name, Column: column.Name, DataType: column.Type, IsNullable: "YES",
				})
			}
			return columns, nil
		}).Maybe()
	data.EXPECT().SampleData(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, stream connectiondata.SampleDataStream, schema, name string, limit uint) error {
			for i, row := range db.tables[name+"@"+schema].Rows() {
				if uint(i) >= limit {
					break
				}
				var encoded bytes.Buffer
				if err := gob.NewEncoder(&encoded).Encode(row); err != nil {
					return err
				}
				if err := stream.Send(&mgmtv1alpha1.GetConnectionDataStreamResponse{RowBytes: encoded.Bytes()}); err != nil {
					return err
				}
			}
			return nil
		}).Maybe()
	builder := connectiondata.NewMockConnectionDataBuilder(t)
	builder.EXPECT().NewDataConnection(mock.Anything, mock.Anything).Return(data, nil).Maybe()
	return builder
}

type silent struct{}

func (silent) Debug(string, ...any) {}
func (silent) Info(string, ...any)  {}
func (silent) Warn(string, ...any)  {}
func (silent) Error(string, ...any) {}

type registry struct {
	env *testsuite.TestActivityEnvironment
}

func (r registry) RegisterWorkflow(any)   {}
func (r registry) RegisterActivity(a any) { r.env.RegisterActivity(a) }

// scanner runs the activities of the table workflow on the tables of the data set.
type scanner struct {
	t   *testing.T
	env *testsuite.TestActivityEnvironment
}

func newScanner(t *testing.T, datasets []Dataset, classifier *model.Classifier) *scanner {
	t.Helper()
	db := newDatabase(datasets)
	var ts testsuite.WorkflowTestSuite
	ts.SetLogger(silent{})
	env := ts.NewTestActivityEnvironment()
	activities := piidetect.NewActivities(nil, db, db.builder(t), nil, classifier, &piidetect.Config{})
	piidetect.Register(registry{env}, nil, activities, &piidetect.Config{})
	return &scanner{t: t, env: env}
}

func run[T any](s *scanner, activity string, request any) *T {
	s.t.Helper()
	value, err := s.env.ExecuteActivity(activity, request)
	require.NoError(s.t, err)
	var response T
	require.NoError(s.t, value.Get(&response))
	return &response
}

// scan scans one table in a mode and returns an outcome per column: what the rules
// report in the rules mode, what the model reports in a model mode. byRules is what the
// rules found on the same columns.
func (s *scanner) scan(language string, table Table, mode string) (outcomes []Outcome, byRules map[string]report.Category) {
	s.t.Helper()
	columns := run[piidetect.GetColumnDataResponse](s, "GetColumnData", &piidetect.GetColumnDataRequest{
		ConnectionId: "evaluation", TableSchema: language, TableName: table.Name, Sample: mode != modeModelNames,
	})
	byRules = run[piidetect.DetectPiiRegexResponse](s, "DetectPiiRegex", &piidetect.DetectPiiRegexRequest{
		ColumnData: columns.ColumnData,
	}).PiiColumns

	var byModel *piidetect.DetectPiiLLMResponse
	if mode != modeRules {
		request := &piidetect.DetectPiiLLMRequest{TableSchema: language, TableName: table.Name, ColumnData: columns.ColumnData}
		if mode == modeModelValues {
			request.Input, request.ConnectionId = report.InputValues, "evaluation"
		}
		byModel = run[piidetect.DetectPiiLLMResponse](s, "DetectPiiLLM", request)
	}

	for _, column := range table.Columns {
		outcome := Outcome{
			Language: language, Table: table.Name, Column: column.Name,
			Expected: column.Expected, Predicted: None, Answered: true,
		}
		if byModel == nil {
			if category, found := byRules[column.Name]; found {
				outcome.Predicted = string(category)
			}
			outcomes = append(outcomes, outcome)
			continue
		}
		outcome.Category = None
		for _, unanswered := range byModel.Unanswered {
			if unanswered == column.Name {
				outcome.Answered, outcome.Category = false, ""
			}
		}
		if finding, found := byModel.PiiColumns[column.Name]; found {
			outcome.Category, outcome.Confidence = string(finding.Category), float64(finding.Confidence)
			outcome.Predicted = outcome.Category
		}
		for _, dismissed := range byModel.BelowThreshold {
			if dismissed.ColumnName == column.Name {
				outcome.Category, outcome.Confidence = string(dismissed.Category), float64(dismissed.Confidence)
			}
		}
		outcomes = append(outcomes, outcome)
	}
	return outcomes, byRules
}

// withRules is what a customer reads: a column is reported when the model or the rules
// report it, under the category of the rules when they have one.
func withRules(outcomes []Outcome, byRules map[string]map[string]report.Category) []Outcome {
	merged := make([]Outcome, 0, len(outcomes))
	for _, outcome := range outcomes {
		if category, found := byRules[outcome.Language+"."+outcome.Table][outcome.Column]; found {
			outcome.Predicted = string(category)
		}
		merged = append(merged, outcome)
	}
	return merged
}

// evaluate scans every table of the data sets in a mode. It returns the outcomes of the
// mode and, for a model mode, those of the mode joined with the rules.
func evaluate(s *scanner, datasets []Dataset, mode string) (own, joined []Outcome) {
	byRules := map[string]map[string]report.Category{}
	for _, dataset := range datasets {
		for _, table := range dataset.Tables {
			outcomes, rules := s.scan(dataset.Language, table, mode)
			own = append(own, outcomes...)
			byRules[dataset.Language+"."+table.Name] = rules
		}
	}
	return own, withRules(own, byRules)
}

// languageScore is the detection of a language, as the baseline holds it.
type languageScore struct {
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
}

func byLanguage(outcomes []Outcome) map[string][]Outcome {
	grouped := map[string][]Outcome{}
	for _, outcome := range outcomes {
		grouped[outcome.Language] = append(grouped[outcome.Language], outcome)
	}
	return grouped
}

func round(value float64) float64 {
	return float64(int(value*10000+0.5)) / 10000
}

// The detection by rules is deterministic. It is held, language by language, to the
// precision and the recall of testdata/baseline.json: a change that lowers one of them
// fails here. A change that raises one says so, with the baseline to commit.
func Test_Rules_HoldTheBaseline(t *testing.T) {
	datasets := loadDatasets(t)
	outcomes, _ := evaluate(newScanner(t, datasets, nil), datasets, modeRules)

	content, err := os.ReadFile(filepath.Join("testdata", "baseline.json"))
	require.NoError(t, err)
	var baseline map[string]languageScore
	require.NoError(t, json.Unmarshal(content, &baseline))

	measured := map[string]languageScore{}
	grouped := byLanguage(outcomes)
	languages := make([]string, 0, len(grouped))
	for language := range grouped {
		languages = append(languages, language)
	}
	sort.Strings(languages)
	for _, language := range languages {
		counts := Detection(grouped[language])
		measured[language] = languageScore{Precision: round(counts.Precision()), Recall: round(counts.Recall())}
		t.Logf("rules, %s: precision %.4f, recall %.4f, F2 %.4f (%+v)", language, counts.Precision(), counts.Recall(), counts.F2(), counts)
	}
	all := Detection(outcomes)
	t.Logf("rules, all languages: precision %.4f, recall %.4f, F2 %.4f (%+v)", all.Precision(), all.Recall(), all.F2(), all)

	raised := false
	for _, language := range languages {
		held, known := baseline[language]
		require.True(t, known, "no baseline for %s", language)
		require.GreaterOrEqual(t, measured[language].Precision, held.Precision, "the precision of the rules is lower for %s", language)
		require.GreaterOrEqual(t, measured[language].Recall, held.Recall, "the recall of the rules is lower for %s", language)
		raised = raised || measured[language] != held
	}
	require.Len(t, baseline, len(languages))
	if raised {
		encoded, err := json.MarshalIndent(measured, "", "  ")
		require.NoError(t, err)
		t.Logf("the rules do better than their baseline: testdata/baseline.json can be raised to\n%s", encoded)
	}
	for _, outcome := range outcomes {
		if (outcome.Expected != None) != (outcome.Predicted != None) {
			t.Logf("  %s %s.%s: expected %s, the rules say %s", outcome.Language, outcome.Table, outcome.Column, outcome.Expected, outcome.Predicted)
		}
	}
}

// modeReport is what the harness says of a mode.
type modeReport struct {
	Mode       string                    `json:"mode"`
	Detection  Counts                    `json:"detection"`
	Precision  float64                   `json:"precision"`
	Recall     float64                   `json:"recall"`
	F2         float64                   `json:"f2"`
	ByLanguage map[string]Counts         `json:"by_language"`
	ByCategory map[string]Counts         `json:"by_category"`
	Confusion  map[string]map[string]int `json:"confusion"`
	Unanswered float64                   `json:"unanswered"`
	// Of the model alone, for the modes that ask it.
	Reliability []Bin   `json:"reliability,omitempty"`
	Thresholds  []Point `json:"thresholds,omitempty"`
	// Wrong lists every column that was not reported as the data set expects.
	Wrong []Outcome `json:"wrong"`
}

func reportOf(mode string, outcomes []Outcome, ofModel bool) modeReport {
	detection := Detection(outcomes)
	r := modeReport{
		Mode: mode, Detection: detection,
		Precision: detection.Precision(), Recall: detection.Recall(), F2: detection.F2(),
		ByLanguage: map[string]Counts{}, ByCategory: ByCategory(outcomes), Confusion: Confusion(outcomes),
		Unanswered: Unanswered(outcomes), Wrong: []Outcome{},
	}
	for language, own := range byLanguage(outcomes) {
		r.ByLanguage[language] = Detection(own)
	}
	if ofModel {
		r.Reliability, r.Thresholds = Reliability(outcomes), Thresholds(outcomes)
	}
	for _, outcome := range outcomes {
		if outcome.Expected != outcome.Predicted {
			r.Wrong = append(r.Wrong, outcome)
		}
	}
	return r
}

// evaluateModel measures the three inputs of the model side by side, each alone and
// joined with the rules, for one model.
func evaluateModel(t *testing.T, datasets []Dataset, classifier *model.Classifier) []modeReport {
	t.Helper()
	scanner := newScanner(t, datasets, classifier)
	reports := make([]modeReport, 0, 2*len(modelModes))
	for _, mode := range modelModes {
		own, joined := evaluate(scanner, datasets, mode)
		reports = append(reports, reportOf(mode, own, true), reportOf(mode+anyReporter, joined, false))
	}
	return reports
}

// oracle is an endpoint that answers what the data set expects, with a confidence of
// 0.9, and keeps what each request carried.
type oracle struct {
	server   *httptest.Server
	expected map[string]string // by table and column
	requests []string
}

func newOracle(t *testing.T, datasets []Dataset) *oracle {
	t.Helper()
	o := &oracle{expected: map[string]string{}}
	for _, dataset := range datasets {
		for _, table := range dataset.Tables {
			for _, column := range table.Columns {
				o.expected[table.Name+"."+column.Name] = column.Expected
			}
		}
	}
	o.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		o.requests = append(o.requests, string(body))
		var request struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(body, &request)
		var document struct {
			Table   string `json:"table"`
			Columns map[string]struct {
				Name string `json:"name"`
			} `json:"columns"`
		}
		_ = json.NewDecoder(strings.NewReader(request.Messages[1].Content)).Decode(&document)

		answers := map[string]any{}
		for id, column := range document.Columns {
			answers[id] = map[string]any{"category": o.expected[document.Table+"."+column.Name], "confidence": 0.9}
		}
		content, _ := json.Marshal(answers)
		completion, _ := json.Marshal(map[string]any{
			"id": "chatcmpl-1", "object": "chat.completion", "model": "oracle",
			"choices": []any{map[string]any{
				"index": 0, "finish_reason": "stop",
				"message": map[string]any{"role": "assistant", "content": string(content)},
			}},
		})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(completion)
	}))
	t.Cleanup(o.server.Close)
	return o
}

// The model stage of the harness, run against an endpoint that answers what the data set
// expects: every mode is then perfect, and each input sends what it says it sends. It
// checks the harness, not a model.
func Test_ModelStage_AgainstAnEndpointThatKnowsTheAnswers(t *testing.T) {
	// One language is enough to check the harness.
	datasets := loadDatasets(t)[:1]
	endpoint := newOracle(t, datasets)
	classifier, err := model.NewClassifier(&model.Config{BaseURL: endpoint.server.URL + "/v1", Model: "oracle", MinConfidence: 0.5})
	require.NoError(t, err)

	reports := evaluateModel(t, datasets, classifier)

	require.Len(t, reports, 6)
	modes := make([]string, 0, len(reports))
	for i, r := range reports {
		modes = append(modes, r.Mode)
		require.InDelta(t, 1, r.Recall, 1e-9, r.Mode)
		require.Zero(t, r.Unanswered, r.Mode)
		if i%2 == 0 {
			// The model alone reports what is expected and nothing else. Joined with the
			// rules it also reports what the rules report.
			require.InDelta(t, 1, r.Precision, 1e-9, r.Mode)
			require.Empty(t, r.Wrong, r.Mode)
		}
	}
	require.Equal(t, []string{
		modeModelNames, modeModelNames + anyReporter,
		modeModelProfiles, modeModelProfiles + anyReporter,
		modeModelValues, modeModelValues + anyReporter,
	}, modes)
	require.Len(t, reports[0].Thresholds, 21)
	require.Len(t, reports[0].Reliability, 10)
	require.Nil(t, reports[1].Thresholds)

	// Three tables, one batch of columns each, per mode; values make smaller batches.
	var names, profiles, values int
	for _, request := range endpoint.requests {
		switch {
		case strings.Contains(request, `\"values\":`):
			values++
		case strings.Contains(request, `\"sample\":`):
			profiles++
		default:
			names++
		}
	}
	require.Equal(t, 3, names)
	require.Equal(t, 3, profiles)
	require.Greater(t, values, 3)
}

// The model stage, on demand: PII_DETECT_EVAL=1 measures the model that the
// PII_DETECT_LLM_* settings name, in its three inputs, and writes what it measured to a
// file whose path is logged. Nothing about a model is asserted: its result is compared
// with the previous one for the same model by whoever changes the prompt, the profile,
// the batches, the values or the threshold.
func Test_ModelStage_OnDemand(t *testing.T) {
	if os.Getenv("PII_DETECT_EVAL") != "1" {
		t.Skip("set PII_DETECT_EVAL=1, and the PII_DETECT_LLM_* settings, to measure a model")
	}
	cfg, err := model.NewConfig(&model.Settings{
		URL:           os.Getenv("PII_DETECT_LLM_URL"),
		APIKey:        os.Getenv("PII_DETECT_LLM_API_KEY"),
		Model:         os.Getenv("PII_DETECT_LLM_MODEL"),
		MinConfidence: os.Getenv("PII_DETECT_LLM_MIN_CONFIDENCE"),
		OpenAIBaseURL: os.Getenv("OPENAI_BASE_URL"),
		OpenAIAPIKey:  os.Getenv("OPENAI_API_KEY"),
	})
	require.NoError(t, err)
	require.True(t, cfg.Enabled(), "no model is configured: set PII_DETECT_LLM_MODEL")
	classifier, err := model.NewClassifier(&cfg)
	require.NoError(t, err)

	datasets := loadDatasets(t)
	rules, _ := evaluate(newScanner(t, datasets, nil), datasets, modeRules)
	result := struct {
		Model         string       `json:"model"`
		Host          string       `json:"host"`
		MinConfidence float64      `json:"min_confidence"`
		Modes         []modeReport `json:"modes"`
	}{
		Model: cfg.Model, Host: cfg.Host(), MinConfidence: cfg.MinConfidence,
		Modes: append([]modeReport{reportOf(modeRules, rules, false)}, evaluateModel(t, datasets, classifier)...),
	}

	file, err := os.CreateTemp("", "pii-detect-eval-*.json")
	require.NoError(t, err)
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	require.NoError(t, encoder.Encode(result))
	t.Logf("model %s at %s, threshold %.2f: the measures are in %s", cfg.Model, cfg.Host(), cfg.MinConfidence, file.Name())
	for _, r := range result.Modes {
		t.Logf("%-40s precision %.4f, recall %.4f, F2 %.4f, unanswered %.4f, wrong %d",
			r.Mode, r.Precision, r.Recall, r.F2, r.Unanswered, len(r.Wrong))
	}
}

// shadowed replaces every letter and every digit of the texts of a row by another one of
// the same class: the layout of the values is kept, their content is not.
func shadowed(row map[string]any) map[string]any {
	shadow := make(map[string]any, len(row))
	for name, value := range row {
		text, isText := value.(string)
		if !isText {
			shadow[name] = value
			continue
		}
		shadow[name] = strings.Map(func(r rune) rune {
			switch {
			case r >= 'a' && r <= 'z':
				return 'a' + (r-'a'+1)%26
			case r >= 'A' && r <= 'Z':
				return 'A' + (r-'A'+1)%26
			case r >= '0' && r <= '9':
				return '0' + (r-'0'+1)%10
			}
			return r
		}, text)
	}
	return shadow
}

// The words a profile is made of, beside its numbers and the layouts of its values.
var profileWords = map[string]bool{
	profile.KindText: true, profile.KindInteger: true, profile.KindDecimal: true, profile.KindBoolean: true,
	profile.KindDate: true, profile.KindDateTime: true, profile.KindBinary: true, profile.KindJSON: true,
	profile.KindArray: true, profile.KindOther: true,
	"future": true, "<1y": true, "1-5y": true, "5-20y": true, "20-60y": true, ">60y": true,
}

// Over the whole data set, a profile says nothing of the values it was computed from: it
// is made of numbers, of its own words and of layouts, and it is the same for values
// whose letters and digits are others. Only the share of the values that pass a format
// check depends on what the values are.
func Test_Profiles_HoldNoPartOfAValue(t *testing.T) {
	layout := regexp.MustCompile(`^[Aa9?+@.,\-_/:()# ]+$`)
	for _, detector := range rules.Detectors() {
		profileWords[detector.Name] = true
	}
	for _, dataset := range loadDatasets(t) {
		for _, table := range dataset.Tables {
			real, shadow := profile.NewTable(rules.Detectors()), profile.NewTable(rules.Detectors())
			for _, row := range table.Rows() {
				real.Add(row)
				shadow.Add(shadowed(row))
			}
			for _, column := range table.Columns {
				p, s := real.Profile(column.Name), shadow.Profile(column.Name)
				require.NotNil(t, p, "%s.%s", table.Name, column.Name)

				for _, share := range append(append([]profile.Share{}, p.Shapes...), p.Hits...) {
					require.True(t, profileWords[share.Name] || layout.MatchString(share.Name),
						"%s.%s holds %q", table.Name, column.Name, share.Name)
				}
				require.True(t, p.Kind == "" || profileWords[p.Kind], p.Kind)
				require.True(t, p.Age == "" || profileWords[p.Age], p.Age)

				p.Hits, s.Hits = nil, nil
				// The age of a moment does not depend on a text; a text that is a number
				// is the same number of characters.
				require.Equal(t, p, s, "%s.%s", table.Name, column.Name)

				encoded, err := json.Marshal(real.Profile(column.Name))
				require.NoError(t, err)
				require.LessOrEqual(t, len(encoded), profile.MaxSize, "%s.%s", table.Name, column.Name)
			}
		}
	}
}
