package presidio

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// The text the recorded analyzer answer is about. It holds characters of several bytes, before
// and inside what the analyzer finds.
const recordedText = "Très cher Jörg Müller, écrivez à jörg.müller@example.com ou appelez le 212-555-0188."

func TestAnalyze_SendsOnlyWhatIsSet(t *testing.T) {
	t.Run("a text and its language", func(t *testing.T) {
		srv, got := answeringJSON(t, http.StatusOK, `[]`)
		_, err := analyzerAt(t, srv.URL).Analyze(context.Background(), &AnalyzeRequest{Text: "hello", Language: "fr"})
		require.NoError(t, err)
		require.Equal(t, http.MethodPost, got.method)
		require.Equal(t, "/analyze", got.path)
		require.Equal(t, "application/json", got.header.Get("Content-Type"))
		require.JSONEq(t, `{"text":"hello","language":"fr"}`, got.body)
	})

	t.Run("everything a request can carry", func(t *testing.T) {
		srv, got := answeringJSON(t, http.StatusOK, `[]`)
		threshold := 0.0
		_, err := analyzerAt(t, srv.URL).Analyze(context.Background(), &AnalyzeRequest{
			Text:           "hello",
			Language:       "en",
			ScoreThreshold: &threshold,
			Entities:       []string{"PERSON", "EMAIL_ADDRESS"},
			AdHocRecognizers: []AdHocRecognizer{{
				Name:              "TITLE",
				SupportedEntity:   "TITLE",
				SupportedLanguage: "en",
				DenyList:          []string{"Mr", "Mrs"},
			}},
		})
		require.NoError(t, err)
		require.JSONEq(t, `{
			"text": "hello",
			"language": "en",
			"score_threshold": 0,
			"entities": ["PERSON", "EMAIL_ADDRESS"],
			"ad_hoc_recognizers": [{
				"name": "TITLE",
				"supported_entity": "TITLE",
				"supported_language": "en",
				"deny_list": ["Mr", "Mrs"]
			}]
		}`, got.body)
	})

	t.Run("empty lists are not sent", func(t *testing.T) {
		srv, got := answeringJSON(t, http.StatusOK, `[]`)
		_, err := analyzerAt(t, srv.URL).Analyze(context.Background(), &AnalyzeRequest{
			Text:             "hello",
			Language:         "en",
			Entities:         []string{},
			AdHocRecognizers: []AdHocRecognizer{},
		})
		require.NoError(t, err)
		require.JSONEq(t, `{"text":"hello","language":"en"}`, got.body)
	})
}

func TestAnalyze_ReadsRecordedAnswer(t *testing.T) {
	srv, _ := answeringJSON(t, http.StatusOK, recorded(t, "analyze_ok.json"))
	findings, err := analyzerAt(t, srv.URL).Analyze(context.Background(), &AnalyzeRequest{Text: recordedText, Language: "en"})
	require.NoError(t, err)
	require.NotEmpty(t, findings)

	text := []rune(recordedText)
	found := map[string]string{}
	for _, finding := range findings {
		require.NotEmpty(t, finding.EntityType)
		require.Positive(t, finding.Score)
		found[finding.EntityType] = string(text[finding.Start:finding.End])
	}
	// The positions count characters: read as bytes, they would fall beside what was found.
	require.Equal(t, "jörg.müller@example.com", found["EMAIL_ADDRESS"])
	require.Equal(t, "212-555-0188", found["PHONE_NUMBER"])
}

func TestAnalyze_NothingFoundIsAnEmptyList(t *testing.T) {
	srv, _ := answeringJSON(t, http.StatusOK, `[]`)
	findings, err := analyzerAt(t, srv.URL).Analyze(context.Background(), &AnalyzeRequest{Text: "x", Language: "en"})
	require.NoError(t, err)
	require.NotNil(t, findings)
	require.Empty(t, findings)
}

func TestAnalyze_FindingMissingAFieldIsInvalid(t *testing.T) {
	for name, body := range map[string]string{
		"no entity type":  `[{"start":0,"end":1,"score":1}]`,
		"no start":        `[{"entity_type":"PERSON","end":1,"score":1}]`,
		"no end":          `[{"entity_type":"PERSON","start":0,"score":1}]`,
		"null":            `null`,
		"not a list":      `{"entity_type":"PERSON","start":0,"end":1}`,
		"a list of lists": `[[{"entity_type":"PERSON","start":0,"end":1}]]`,
	} {
		t.Run(name, func(t *testing.T) {
			srv, _ := answeringJSON(t, http.StatusOK, body)
			_, err := analyzerAt(t, srv.URL).Analyze(context.Background(), &AnalyzeRequest{Text: "x", Language: "en"})
			require.ErrorIs(t, err, ErrInvalidResponse)
		})
	}

	t.Run("a finding without a score has none", func(t *testing.T) {
		srv, _ := answeringJSON(t, http.StatusOK, `[{"entity_type":"PERSON","start":0,"end":1}]`)
		findings, err := analyzerAt(t, srv.URL).Analyze(context.Background(), &AnalyzeRequest{Text: "x", Language: "en"})
		require.NoError(t, err)
		require.Equal(t, []Finding{{EntityType: "PERSON", Start: 0, End: 1}}, findings)
	})
}

// A finding lies inside the text that was sent, counted in characters: the text "éé" is two
// characters and four bytes long.
func TestAnalyze_OffsetsAreRunes(t *testing.T) {
	analyze := func(t *testing.T, body string) ([]Finding, error) {
		t.Helper()
		srv, _ := answeringJSON(t, http.StatusOK, body)
		return analyzerAt(t, srv.URL).Analyze(context.Background(), &AnalyzeRequest{Text: "éé", Language: "en"})
	}

	t.Run("up to the last character", func(t *testing.T) {
		findings, err := analyze(t, `[{"entity_type":"PERSON","start":0,"end":2,"score":0.85}]`)
		require.NoError(t, err)
		require.Equal(t, []Finding{{EntityType: "PERSON", Start: 0, End: 2, Score: 0.85}}, findings)
	})

	for name, body := range map[string]string{
		"past the last character": `[{"entity_type":"PERSON","start":0,"end":3,"score":1}]`,
		"before the first":        `[{"entity_type":"PERSON","start":-1,"end":1,"score":1}]`,
		"ending before it starts": `[{"entity_type":"PERSON","start":2,"end":1,"score":1}]`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := analyze(t, body)
			require.ErrorIs(t, err, ErrInvalidResponse)
		})
	}
}

func TestSupportedEntities_ListsForLanguage(t *testing.T) {
	t.Run("the recorded list", func(t *testing.T) {
		srv, got := answeringJSON(t, http.StatusOK, recorded(t, "supportedentities_en.json"))
		entities, err := analyzerAt(t, srv.URL).SupportedEntities(context.Background(), "en")
		require.NoError(t, err)
		require.Equal(t, http.MethodGet, got.method)
		require.Equal(t, "/supportedentities", got.path)
		require.Equal(t, "language=en", got.query)
		require.Empty(t, got.body)
		require.Contains(t, entities, "PERSON")
		require.Contains(t, entities, "EMAIL_ADDRESS")
	})

	t.Run("an empty list", func(t *testing.T) {
		srv, got := answeringJSON(t, http.StatusOK, `[]`)
		entities, err := analyzerAt(t, srv.URL).SupportedEntities(context.Background(), "fr")
		require.NoError(t, err)
		require.Equal(t, "language=fr", got.query)
		require.NotNil(t, entities)
		require.Empty(t, entities)
	})

	for name, body := range map[string]string{
		"null":       `null`,
		"not a list": `{"entities":["PERSON"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			srv, _ := answeringJSON(t, http.StatusOK, body)
			_, err := analyzerAt(t, srv.URL).SupportedEntities(context.Background(), "en")
			require.ErrorIs(t, err, ErrInvalidResponse)
		})
	}
}
