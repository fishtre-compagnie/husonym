package piitext

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
	"github.com/stretchr/testify/require"
)

// The files of testdata/presidio are answers of a real Presidio (see their README). The
// analyzer's pin how it counts positions; the anonymizer's are the reference this package's own
// operators and overlap rule are compared with.

type analyzeCase struct {
	Name    string `json:"name"`
	Request struct {
		Text string `json:"text"`
	} `json:"request"`
	Status int             `json:"status"`
	Answer json.RawMessage `json:"answer"`
}

type anonymizeCase struct {
	Name    string `json:"name"`
	Request struct {
		Text        string                     `json:"text"`
		Findings    []presidio.Finding         `json:"analyzer_results"`
		Anonymizers map[string]json.RawMessage `json:"anonymizers"`
	} `json:"request"`
	Status int `json:"status"`
	Answer struct {
		Text string `json:"text"`
	} `json:"answer"`
}

func readCases[T any](t *testing.T, file string) []T {
	t.Helper()
	bits, err := os.ReadFile(filepath.Join("testdata", "presidio", file))
	require.NoError(t, err)
	var cases []T
	require.NoError(t, json.Unmarshal(bits, &cases))
	require.NotEmpty(t, cases)
	return cases
}

func Test_Reference_AnalyzerPositionsAreCharacters(t *testing.T) {
	// What each finding of the recorded answers designates, in the order of the answer.
	designated := map[string][]string{
		"french":                {"jörg.müller@example.com", "Jörg Müller", "example.com", "212-555-0188"},
		"emoji":                 {"ann@example.com", "example.com", "212-555-0188"},
		"combining":             {"jose@example.com", "example.com"},
		"cjk":                   {"taro@example.com", "example.com"},
		"arabic":                {"mohamed@example.com", "اتصل بمحمد", "example.com"},
		"hebrew":                {"david@example.com", "David", "example.com"},
		"replacement_character": {"bob@example.com", "example.com"},
	}
	seen := 0
	for _, tc := range readCases[analyzeCase](t, "analyze.json") {
		want, ok := designated[tc.Name]
		if !ok {
			continue
		}
		seen++
		t.Run(tc.Name, func(t *testing.T) {
			require.Equal(t, 200, tc.Status)
			var findings []presidio.Finding
			require.NoError(t, json.Unmarshal(tc.Answer, &findings))
			targets, err := locate(tc.Request.Text, findings)
			require.NoError(t, err)
			got := make([]string, 0, len(targets))
			for _, target := range targets {
				got = append(got, target.of(tc.Request.Text))
			}
			require.Equal(t, want, got)
		})
	}
	require.Equal(t, len(designated), seen)
}

// operatorOf reads an operator of the anonymizer's request as the configuration that asks
// this package for the same.
func operatorOf(t *testing.T, raw json.RawMessage) *mgmtv1alpha1.PiiAnonymizer {
	t.Helper()
	var operator struct {
		Type        string `json:"type"`
		NewValue    string `json:"new_value"`
		MaskingChar string `json:"masking_char"`
		CharsToMask int32  `json:"chars_to_mask"`
		FromEnd     bool   `json:"from_end"`
	}
	require.NoError(t, json.Unmarshal(raw, &operator))
	switch operator.Type {
	case "replace":
		return replaceWith(operator.NewValue)
	case "redact":
		return redact()
	case "mask":
		return mask(operator.MaskingChar, operator.CharsToMask, operator.FromEnd)
	default:
		t.Fatalf("no configuration for the operator %q", operator.Type)
		return nil
	}
}

func Test_Reference_Anonymizer(t *testing.T) {
	// Where this package answers something else than Presidio's anonymizer, on purpose.
	different := map[string]struct{ want, reason string }{
		"zero_length_finding": {
			"James Bond met Jörg Müller",
			"a finding of no character designates nothing, so nothing is written for it",
		},
		"equal_span_equal_scores_reversed": {
			"<LOCATION> Bond met Jörg Müller",
			"of two findings on the same characters with one score, the entity name that sorts first stays, whatever their order",
		},
	}
	// What the reference has no say on.
	notCompared := map[string]string{
		"hash_md5":            "the hash is this package's own: keyed, and of three lengths",
		"hash_sha256_first":   "the hash is this package's own: keyed, and repeatable",
		"hash_sha256_second":  "the hash is this package's own: keyed, and repeatable",
		"mask_two_characters": "refused when the transformer is built: see Test_Validate",
	}

	for _, tc := range readCases[anonymizeCase](t, "anonymize.json") {
		t.Run(tc.Name, func(t *testing.T) {
			if reason, ok := notCompared[tc.Name]; ok {
				t.Skip(reason)
			}
			config := &mgmtv1alpha1.TransformPiiText{EntityAnonymizers: map[string]*mgmtv1alpha1.PiiAnonymizer{}}
			for entity, raw := range tc.Request.Anonymizers {
				// The anonymizer reads the operator of every other entity type under this key.
				if entity == "DEFAULT" {
					config.DefaultAnonymizer = operatorOf(t, raw)
					continue
				}
				config.EntityAnonymizers[entity] = operatorOf(t, raw)
			}

			out, err := rewrite(t, config, Options{}, tc.Request.Text, tc.Request.Findings...)
			if tc.Status != 200 {
				require.Error(t, err, "what the anonymizer refuses fails the value")
				return
			}
			require.NoError(t, err)
			if deviation, ok := different[tc.Name]; ok {
				require.NotEqual(t, tc.Answer.Text, deviation.want, "the reference no longer differs: %s", deviation.reason)
				require.Equal(t, deviation.want, out, deviation.reason)
				return
			}
			require.Equal(t, tc.Answer.Text, out)
		})
	}
}

func Test_Reference_Analyzer(t *testing.T) {
	answers := map[string][]presidio.Finding{}
	statuses := map[string]int{}
	for _, tc := range readCases[analyzeCase](t, "analyze.json") {
		statuses[tc.Name] = tc.Status
		if tc.Status == 200 {
			var findings []presidio.Finding
			require.NoError(t, json.Unmarshal(tc.Answer, &findings))
			answers[tc.Name] = findings
		}
	}
	entities := func(name string) []string {
		found := []string{}
		for _, finding := range answers[name] {
			found = append(found, finding.EntityType)
		}
		return found
	}

	t.Run("a threshold of zero finds what no threshold finds", func(t *testing.T) {
		require.Equal(t, answers["threshold_omitted"], answers["threshold_zero"])
	})

	t.Run("a finding scored at the threshold is found when the threshold is sent as its decimal", func(t *testing.T) {
		require.Contains(t, entities("threshold_0.85"), "PERSON")
		require.NotContains(t, entities("threshold_0.85_as_float32"), "PERSON")
	})

	t.Run("a deny list is used only when declared in the language of the request", func(t *testing.T) {
		require.Contains(t, entities("deny_declared_in_request_language"), "vip-list")
		require.NotContains(t, entities("deny_declared_in_another_language"), "vip-list")
	})

	t.Run("a deny list is used under a restricted list of entities only when its name is listed", func(t *testing.T) {
		require.Contains(t, entities("deny_name_listed_in_entities"), "vip-list")
		require.NotContains(t, entities("deny_name_not_listed_in_entities"), "vip-list")
	})

	t.Run("a language the analyzer has no recognizer for is refused", func(t *testing.T) {
		require.Equal(t, 500, statuses["deny_request_in_unsupported_language"])
	})
}
