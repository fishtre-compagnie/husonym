package presidio

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAnonymize_SendsFindingsAndOperators(t *testing.T) {
	t.Run("findings in their order, one operator of each kind", func(t *testing.T) {
		srv, got := answeringJSON(t, http.StatusOK, `{"text":"x","items":[]}`)
		_, err := anonymizerAt(t, srv.URL).Anonymize(context.Background(), &AnonymizeRequest{
			Text: "hello Jane, jane@example.com",
			Findings: []Finding{
				{EntityType: "PERSON", Start: 6, End: 10, Score: 0.85},
				{EntityType: "EMAIL_ADDRESS", Start: 12, End: 28, Score: 1},
			},
			Operators: map[string]Operator{
				DefaultOperatorKey: Replace("<pii>"),
				"PERSON":           Redact(),
				"EMAIL_ADDRESS":    Hash(HashSHA512),
				"PHONE_NUMBER":     Mask("*", 4, true),
				"IBAN_CODE":        Mask("#", 0, false),
				"URL":              Replace(""),
			},
		})
		require.NoError(t, err)
		require.Equal(t, http.MethodPost, got.method)
		require.Equal(t, "/anonymize", got.path)
		require.Equal(t, "application/json", got.header.Get("Content-Type"))
		require.JSONEq(t, `{
			"text": "hello Jane, jane@example.com",
			"analyzer_results": [
				{"entity_type": "PERSON", "start": 6, "end": 10, "score": 0.85},
				{"entity_type": "EMAIL_ADDRESS", "start": 12, "end": 28, "score": 1}
			],
			"anonymizers": {
				"DEFAULT": {"type": "replace", "new_value": "<pii>"},
				"PERSON": {"type": "redact"},
				"EMAIL_ADDRESS": {"type": "hash", "hash_type": "sha512"},
				"PHONE_NUMBER": {"type": "mask", "masking_char": "*", "chars_to_mask": 4, "from_end": true},
				"IBAN_CODE": {"type": "mask", "masking_char": "#", "chars_to_mask": 0, "from_end": false},
				"URL": {"type": "replace", "new_value": ""}
			}
		}`, got.body)
	})

	t.Run("no finding is an empty list, no operator is not sent", func(t *testing.T) {
		srv, got := answeringJSON(t, http.StatusOK, `{"text":"hello","items":[]}`)
		_, err := anonymizerAt(t, srv.URL).Anonymize(context.Background(), &AnonymizeRequest{Text: "hello"})
		require.NoError(t, err)
		require.JSONEq(t, `{"text":"hello","analyzer_results":[]}`, got.body)
	})

	t.Run("an operator that was never built is refused before the call", func(t *testing.T) {
		srv, got := answeringJSON(t, http.StatusOK, `{"text":"hello","items":[]}`)
		_, err := anonymizerAt(t, srv.URL).Anonymize(context.Background(), &AnonymizeRequest{
			Text:      "hello",
			Operators: map[string]Operator{"PERSON": {}},
		})
		require.Error(t, err)
		require.Empty(t, got.path)
	})
}

func TestOperators_AreComparable(t *testing.T) {
	require.Equal(t, Hash(HashSHA256), Hash(HashSHA256))
	require.NotEqual(t, Hash(HashSHA256), Hash(HashMD5))
	require.NotEqual(t, Replace("a"), Replace("b"))
	require.NotEqual(t, Redact(), Replace(""))
	require.Equal(t, Mask("*", 4, true), Mask("*", 4, true))
}

func TestAnonymize_ReadsRecordedAnswer(t *testing.T) {
	srv, _ := answeringJSON(t, http.StatusOK, recorded(t, "anonymize_ok.json"))
	result, err := anonymizerAt(t, srv.URL).Anonymize(context.Background(), &AnonymizeRequest{Text: recordedText})
	require.NoError(t, err)

	require.Equal(t, "Très cher Jörg Müller, écrivez à <EMAIL> ou appelez le ********0188.", result.Text)
	text := []rune(result.Text)
	replaced := map[string]AnonymizedItem{}
	for _, item := range result.Items {
		// An item is placed in the anonymized text, in characters.
		require.Equal(t, item.Text, string(text[item.Start:item.End]))
		replaced[item.EntityType] = item
	}
	require.Len(t, replaced, 2)
	require.Equal(t, "<EMAIL>", replaced["EMAIL_ADDRESS"].Text)
	require.Equal(t, "replace", replaced["EMAIL_ADDRESS"].Operator)
	require.Equal(t, "********0188", replaced["PHONE_NUMBER"].Text)
	require.Equal(t, "mask", replaced["PHONE_NUMBER"].Operator)
}

// A success that lacks what the caller is about to read is an error, never a missing value the
// caller finds out by reading it.
func TestAnonymize_AnswerWithoutTextIsInvalid(t *testing.T) {
	for name, body := range map[string]string{
		"no text":                    `{"items":[]}`,
		"a null text":                `{"text":null,"items":[]}`,
		"null":                       `null`,
		"a list":                     `[]`,
		"an item without text":       `{"text":"x","items":[{"entity_type":"PERSON","start":0,"end":1,"operator":"replace"}]}`,
		"an item with a null text":   `{"text":"x","items":[{"entity_type":"PERSON","start":0,"end":1,"text":null}]}`,
		"an item without its entity": `{"text":"x","items":[{"start":0,"end":1,"text":"x"}]}`,
		"an item without its start":  `{"text":"x","items":[{"entity_type":"PERSON","end":1,"text":"x"}]}`,
		"an item without its end":    `{"text":"x","items":[{"entity_type":"PERSON","start":0,"text":"x"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			srv, _ := answeringJSON(t, http.StatusOK, body)
			_, err := anonymizerAt(t, srv.URL).Anonymize(context.Background(), &AnonymizeRequest{Text: "x"})
			require.ErrorIs(t, err, ErrInvalidResponse)
		})
	}

	t.Run("an empty text is a text", func(t *testing.T) {
		srv, _ := answeringJSON(t, http.StatusOK, `{"text":"","items":[{"entity_type":"PERSON","start":0,"end":0,"text":"","operator":"redact"}]}`)
		result, err := anonymizerAt(t, srv.URL).Anonymize(context.Background(), &AnonymizeRequest{Text: "x"})
		require.NoError(t, err)
		require.Empty(t, result.Text)
		require.Equal(t, []AnonymizedItem{{EntityType: "PERSON", Operator: "redact"}}, result.Items)
	})

	t.Run("no items is none", func(t *testing.T) {
		srv, _ := answeringJSON(t, http.StatusOK, `{"text":"x"}`)
		result, err := anonymizerAt(t, srv.URL).Anonymize(context.Background(), &AnonymizeRequest{Text: "x"})
		require.NoError(t, err)
		require.Equal(t, "x", result.Text)
		require.Empty(t, result.Items)
	})
}
