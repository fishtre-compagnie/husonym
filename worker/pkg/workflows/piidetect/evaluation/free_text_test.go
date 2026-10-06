package evaluation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	content "github.com/fishtre-compagnie/husonym/backend/pkg/piidetect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
	connectiondataservice "github.com/fishtre-compagnie/husonym/backend/services/mgmt/v1alpha1/connection-data-service"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/profile"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/report"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// What a job asks of the API for its free-text columns: the columns of a call, and the
// values of a column. They are the worker's own, which it does not export.
const (
	contentColumnsPerCall = 20
	contentSampleSize     = 50
)

func loadFreeText(t *testing.T) []Dataset {
	t.Helper()
	datasets, err := LoadFreeText("testdata")
	require.NoError(t, err)
	return datasets
}

func texts(column Column) []string {
	values := make([]string, 0, len(column.Values))
	for _, value := range column.Values {
		if text, ok := value.(string); ok {
			values = append(values, text)
		}
	}
	return values
}

// The data sets of free text hold, for French and for English, twelve columns of fifty
// sentences: six that name a person in 4 to 20 % of their values, six that name none. A
// job sends every one of them to the content analysis: each is text of
// content.FreeTextMinWords words or more, for the profile of the worker and for the rule
// of the API, and the rules find nothing in it.
func Test_FreeTextDataset(t *testing.T) {
	datasets := loadFreeText(t)
	languages := make([]string, 0, len(datasets))
	scanner := newScanner(t, datasets, nil)
	for _, dataset := range datasets {
		languages = append(languages, dataset.Language)
		require.Len(t, dataset.Tables, 1, dataset.Language)
		table := dataset.Tables[0]
		require.Len(t, table.Columns, 12, dataset.Language)

		columns := run[piidetect.GetColumnDataResponse](scanner, "GetColumnData", &piidetect.GetColumnDataRequest{
			ConnectionId: "evaluation", TableSchema: dataset.Language, TableName: table.Name, Sample: true,
		})
		byRules := run[piidetect.DetectPiiRegexResponse](scanner, "DetectPiiRegex", &piidetect.DetectPiiRegexRequest{
			ColumnData: columns.ColumnData,
		}).PiiColumns
		require.Empty(t, byRules, "%s: the rules find nothing in free text", dataset.Language)
		profiles := map[string]*profile.Profile{}
		for _, column := range columns.ColumnData {
			profiles[column.Column] = column.Profile
		}

		withPersons := 0
		for _, column := range table.Columns {
			name := dataset.Language + "." + column.Name
			require.Equal(t, "text", column.Type, name)
			require.Len(t, texts(column), contentSampleSize, name)
			require.True(t, content.IsFreeText(texts(column)), name)
			require.Equal(t, profile.KindText, profiles[column.Name].Kind, name)
			require.GreaterOrEqual(t, profiles[column.Name].Words, content.FreeTextMinWords, name)

			if column.Expected == None {
				require.Empty(t, column.Persons, name)
				continue
			}
			withPersons++
			require.Equal(t, string(report.Personal), column.Expected, name)
			require.GreaterOrEqual(t, len(column.Persons), 2, name)
			require.LessOrEqual(t, len(column.Persons), 10, name)
			require.True(t, sort.IntsAreSorted(column.Persons), name)
			require.Len(t, slices.Compact(slices.Clone(column.Persons)), len(column.Persons), name)
			require.GreaterOrEqual(t, column.Persons[0], 0, name)
			require.Less(t, column.Persons[len(column.Persons)-1], contentSampleSize, name)
		}
		require.Equal(t, 6, withPersons, dataset.Language)
	}
	require.Equal(t, []string{"en", "fr"}, languages)
}

// content.FreeTextMinWords, over the tables of the eight languages: every column of
// sentences reaches it, and no column of names, of codes or of labels does. The columns
// of other kinds that reach it are telephone numbers written in groups and postal
// addresses: values of several words that are not sentences. A job sends to the content
// analysis those the rules find nothing in, which are logged.
func Test_FreeTextMinWords_OnTheDataset(t *testing.T) {
	datasets := loadDatasets(t)
	scanner := newScanner(t, datasets, nil)
	lowestSentences, highestOther := 0.0, 0.0
	var sent []string
	for _, dataset := range datasets {
		for _, table := range dataset.Tables {
			columns := run[piidetect.GetColumnDataResponse](scanner, "GetColumnData", &piidetect.GetColumnDataRequest{
				ConnectionId: "evaluation", TableSchema: dataset.Language, TableName: table.Name, Sample: true,
			})
			byRules := run[piidetect.DetectPiiRegexResponse](scanner, "DetectPiiRegex", &piidetect.DetectPiiRegexRequest{
				ColumnData: columns.ColumnData,
			}).PiiColumns
			words := map[string]float64{}
			for _, column := range columns.ColumnData {
				if column.Profile != nil && column.Profile.Kind == profile.KindText {
					words[column.Column] = column.Profile.Words
				}
			}
			for _, column := range table.Columns {
				mean, isText := words[column.Name]
				if !isText {
					continue
				}
				name := dataset.Language + "." + table.Name + "." + column.Name
				_, found := byRules[column.Name]
				switch {
				case column.Type == "text":
					// The columns of type text of the data set are its columns of sentences.
					require.GreaterOrEqual(t, mean, content.FreeTextMinWords, name)
					if lowestSentences == 0 || mean < lowestSentences {
						lowestSentences = mean
					}
				case mean >= content.FreeTextMinWords:
					require.Contains(t, []string{string(report.Contact), string(report.Location)}, column.Expected, name)
					if !found {
						sent = append(sent, fmt.Sprintf("%s (%s, %.1f words)", name, column.Expected, mean))
					}
				default:
					highestOther = max(highestOther, mean)
				}
			}
		}
	}
	t.Logf("words a value: %.1f at least in a column of sentences, %.1f at most in a column under the threshold of %.1f",
		lowestSentences, highestOther, content.FreeTextMinWords)
	t.Logf("columns that are not sentences, reach the threshold and are sent to the content analysis: %s", strings.Join(sent, "; "))
	require.Greater(t, lowestSentences, highestOther)
}

// recordingAnalyzer asks an analyzer, and keeps what it found in each text.
type recordingAnalyzer struct {
	analyzer presidio.Analyzer
	findings map[string][]presidio.Finding
}

func (a *recordingAnalyzer) Analyze(ctx context.Context, request *presidio.AnalyzeRequest) ([]presidio.Finding, error) {
	findings, err := a.analyzer.Analyze(ctx, request)
	if err == nil {
		a.findings[request.Text] = findings
	}
	return findings, err
}

// span is one sensitive entity the analyzer found in a value of a column. NamesPerson
// says the value is one of those that name a person.
type span struct {
	Value       int     `json:"value"`
	NamesPerson bool    `json:"names_person,omitempty"`
	Entity      string  `json:"entity"`
	Text        string  `json:"text"`
	Score       float64 `json:"score"`
}

// freeTextOutcome is what the content analysis of the API said of one column of free
// text, beside what the analyzer found in its values.
type freeTextOutcome struct {
	Language string `json:"language"`
	Column   string `json:"column"`
	// Persons is how many values of the column name a person.
	Persons int `json:"persons"`

	// What the API reports: nothing, or a category with the entity found in the most
	// values, the number of values it counted and its evidence.
	Reported bool   `json:"reported"`
	Category string `json:"category,omitempty"`
	Entity   string `json:"entity,omitempty"`
	Matches  int    `json:"matches,omitempty"`
	Evidence string `json:"evidence,omitempty"`
	// ByThird says the column is reported by the rule of the third, as it was before the
	// rule of free text.
	ByThird bool `json:"by_third"`

	// Sensitive counts, for each entity the API marks as personal data, the values the
	// analyzer found it in; Other does the same for the entities it does not count.
	Sensitive map[string]int `json:"sensitive"`
	Other     map[string]int `json:"other,omitempty"`
	// SensitiveValues is how many values hold a sensitive entity, whichever it is.
	SensitiveValues int `json:"sensitive_values"`
	// PersonsFound is how many of the values that name a person the analyzer found a
	// person in.
	PersonsFound int `json:"persons_found"`
	// Found lists every sensitive entity the analyzer found, value by value: what any
	// other way of counting them would give can be worked out from it.
	Found []span `json:"found,omitempty"`
}

func isSensitive(entity string) bool {
	suggestion, ok := content.SuggestionForEntity(entity, "")
	return ok && suggestion.Sensitive
}

// contentScan asks the content analysis of the API, served by a real analyzer, about
// the columns of the data sets, as the activity of a job does.
type contentScan struct {
	t        *testing.T
	service  *connectiondataservice.Service
	recorder *recordingAnalyzer
}

func newContentScan(t *testing.T, analyzer presidio.Analyzer, language string, datasets []Dataset) *contentScan {
	t.Helper()
	db := newDatabase(datasets)
	connections := mgmtv1alpha1connect.NewMockConnectionServiceClient(t)
	connections.EXPECT().GetConnection(mock.Anything, mock.Anything).RunAndReturn(db.GetConnection).Maybe()
	recorder := &recordingAnalyzer{analyzer: analyzer, findings: map[string][]presidio.Finding{}}
	// A job names no language: the texts are analyzed in the language the API is set to.
	service := connectiondataservice.New(
		&connectiondataservice.Config{IsPresidioEnabled: true, PresidioDefaultLanguage: &language},
		connections, db.builder(t), recorder, connectiondataservice.Transformers{},
	)
	return &contentScan{t: t, service: service, recorder: recorder}
}

// detections asks about the columns of a table, contentColumnsPerCall per call and
// contentSampleSize values of each. Every column must have been analyzed.
func (s *contentScan) detections(schema string, table Table) map[string]*mgmtv1alpha1.ColumnPiiDetection {
	s.t.Helper()
	names := make([]string, 0, len(table.Columns))
	for _, column := range table.Columns {
		names = append(names, column.Name)
	}
	found := map[string]*mgmtv1alpha1.ColumnPiiDetection{}
	for columns := range slices.Chunk(names, contentColumnsPerCall) {
		answer, err := s.service.DetectPiiInConnectionData(s.t.Context(), connect.NewRequest(
			&mgmtv1alpha1.DetectPiiInConnectionDataRequest{
				ConnectionId: "evaluation", Schema: schema, Table: table.Name, Columns: columns, SampleSize: contentSampleSize,
			},
		))
		require.NoError(s.t, err)
		for _, detection := range answer.Msg.GetDetections() {
			found[detection.GetColumn()] = detection
		}
		for _, verdict := range answer.Msg.GetVerdicts() {
			require.False(s.t, verdict.GetContentNotAnalyzed(), "%s was not analyzed", verdict.GetColumn())
		}
	}
	return found
}

func (s *contentScan) outcome(language string, column Column, detection *mgmtv1alpha1.ColumnPiiDetection) freeTextOutcome {
	outcome := freeTextOutcome{
		Language: language, Column: column.Name, Persons: len(column.Persons),
		Sensitive: map[string]int{}, Other: map[string]int{},
	}
	if detection != nil {
		outcome.Reported = true
		outcome.Category, outcome.Entity = detection.GetDataCategory(), detection.GetEntityType()
		outcome.Matches, outcome.Evidence = int(detection.GetMatchCount()), detection.GetPiiEvidence()
		outcome.ByThird = outcome.Category != content.FreeTextCategory
	}
	for i, text := range texts(column) {
		namesPerson := slices.Contains(column.Persons, i)
		entities := map[string]bool{}
		for _, finding := range s.recorder.findings[text] {
			entities[finding.EntityType] = true
			if isSensitive(finding.EntityType) {
				outcome.Found = append(outcome.Found, span{
					Value: i, NamesPerson: namesPerson, Entity: finding.EntityType, Score: finding.Score,
					Text: string([]rune(text)[finding.Start:finding.End]),
				})
			}
		}
		holdsSensitive := false
		for entity := range entities {
			if isSensitive(entity) {
				outcome.Sensitive[entity]++
				holdsSensitive = true
			} else {
				outcome.Other[entity]++
			}
		}
		if holdsSensitive {
			outcome.SensitiveValues++
		}
		if namesPerson && entities["PERSON"] {
			outcome.PersonsFound++
		}
	}
	return outcome
}

func counted(counts map[string]int) string {
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s %d", name, counts[name]))
	}
	return strings.Join(parts, ", ")
}

// The rule of free text, on demand, against a real analyzer:
// PII_DETECT_EVAL_ANALYZER_URL names a Presidio analyzer built from docker/presidio-fr,
// which loads its language models for some seconds before it answers:
//
//	docker compose -f compose.dev.yml up -d presidio-analyzer
//	PII_DETECT_EVAL_ANALYZER_URL=http://localhost:5002 \
//	    go test ./worker/pkg/workflows/piidetect/evaluation -run Test_FreeText_AgainstAnAnalyzer -v
//
// No model of language is asked. The columns of the data sets of free text are sent
// to the content analysis of the API as a job sends them, each data set in its own
// language, or all of them in PII_DETECT_EVAL_ANALYZER_LANGUAGE, as an API set to one
// language analyzes them. What is measured, column by column, is logged and written to a
// file whose path is logged: whether the column is reported, by the rule of the third
// alone and with the rule of free text, and what the analyzer found in its values.
//
// A call for 20 columns of 50 values is timed, once: it is an order of magnitude beside
// the time the activity of a job has, not a measure of throughput.
//
// Nothing about the analyzer is asserted: the result is read by whoever changes the
// rule, its thresholds or the recognizers.
func Test_FreeText_AgainstAnAnalyzer(t *testing.T) {
	url := os.Getenv("PII_DETECT_EVAL_ANALYZER_URL")
	if url == "" {
		t.Skip("set PII_DETECT_EVAL_ANALYZER_URL to the URL of a Presidio analyzer to measure the rule of free text")
	}
	analyzer, err := presidio.NewAnalyzerClient(url, &http.Client{})
	require.NoError(t, err)

	type measure struct {
		Language string `json:"language"`
		Analyzed string `json:"analyzed_as"`
		// Whether a column is reported as holding personal data, before the rule of free
		// text and with it.
		Before  Counts            `json:"before"`
		After   Counts            `json:"after"`
		Columns []freeTextOutcome `json:"columns"`
		// CallColumns columns of contentSampleSize values were analyzed in one call that
		// took CallSeconds.
		CallColumns int     `json:"call_columns"`
		CallSeconds float64 `json:"call_seconds"`
	}
	var measures []measure

	for _, dataset := range loadFreeText(t) {
		language := dataset.Language
		if forced := os.Getenv("PII_DETECT_EVAL_ANALYZER_LANGUAGE"); forced != "" {
			language = forced
		}
		entities, err := analyzer.SupportedEntities(t.Context(), language)
		require.NoError(t, err, "the analyzer does not answer")
		require.NotEmpty(t, entities, language)

		table := dataset.Tables[0]
		scan := newContentScan(t, analyzer, language, []Dataset{dataset})
		detections := scan.detections(dataset.Language, table)

		m := measure{Language: dataset.Language, Analyzed: language}
		var before, after []Outcome
		for _, column := range table.Columns {
			outcome := scan.outcome(dataset.Language, column, detections[column.Name])
			m.Columns = append(m.Columns, outcome)

			scored := Outcome{
				Language: dataset.Language, Table: table.Name, Column: column.Name,
				Expected: column.Expected, Predicted: None, Answered: true,
			}
			if outcome.ByThird {
				scored.Predicted = outcome.Category
			}
			before = append(before, scored)
			if outcome.Reported {
				scored.Predicted = outcome.Category
			}
			after = append(after, scored)

			verdict := "nothing"
			if outcome.Reported {
				verdict = fmt.Sprintf("%s, %s in %d values", outcome.Category, outcome.Entity, outcome.Matches)
			}
			t.Logf("%s %-20s persons %2d (found %2d) | %-40s | sensitive values %2d: %s | other: %s",
				dataset.Language, column.Name, outcome.Persons, outcome.PersonsFound, verdict,
				outcome.SensitiveValues, counted(outcome.Sensitive), counted(outcome.Other))
			if column.Expected == None && outcome.Reported {
				for _, found := range outcome.Found {
					t.Logf("    value %2d: %s %q (%.2f)", found.Value, found.Entity, found.Text, found.Score)
				}
			}
		}
		m.Before, m.After = Detection(before), Detection(after)
		t.Logf("%s analyzed as %s, rule of the third alone: precision %.4f, recall %.4f (%+v)",
			dataset.Language, language, m.Before.Precision(), m.Before.Recall(), m.Before)
		t.Logf("%s analyzed as %s, with the rule of free text: precision %.4f, recall %.4f (%+v)",
			dataset.Language, language, m.After.Precision(), m.After.Recall(), m.After)

		// One call of a job at its largest: the columns of the table, and the first ones
		// again under another name.
		wide := Table{Name: table.Name + "_wide", Columns: slices.Clone(table.Columns)}
		for _, column := range table.Columns[:contentColumnsPerCall-len(table.Columns)] {
			column.Name += "_again"
			wide.Columns = append(wide.Columns, column)
		}
		timed := newContentScan(t, analyzer, language, []Dataset{{Language: dataset.Language, Tables: []Table{wide}}})
		started := time.Now()
		timed.detections(dataset.Language, wide)
		m.CallColumns, m.CallSeconds = len(wide.Columns), time.Since(started).Seconds()
		t.Logf("%s analyzed as %s: one call for %d columns of %d values took %.1f s",
			dataset.Language, language, m.CallColumns, contentSampleSize, m.CallSeconds)

		measures = append(measures, m)
	}

	file, err := os.CreateTemp("", "pii-detect-free-text-*.json")
	require.NoError(t, err)
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	require.NoError(t, encoder.Encode(measures))
	t.Logf("the measures are in %s", file.Name())
}
