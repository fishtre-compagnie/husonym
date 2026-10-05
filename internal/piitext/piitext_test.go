package piitext

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio/presidiotest"
	"github.com/stretchr/testify/require"
)

// texts are values in several scripts, each with the personal data an analyzer finds in it
// and another part of it the analyzer finds too, which a user may allow.
var texts = []struct {
	name    string
	text    string
	found   string
	allowed string
}{
	{"ascii", "Call Bob now, not Alice", "Bob", "Alice"},
	{"french accents", "Très cher Jörg Müller, écrivez à Zoé", "Jörg Müller", "Zoé"},
	{"accents before the finding", "ééééé John Mary", "Mary", "John"},
	{"emoji", "👩‍💻 Ann 🎉 met Bob 🎉", "Bob", "Ann"},
	{"combining characters", "José écrit à Zoé et Bob", "Zoé", "José"},
	{"cjk", "山田太郎さんに電話、佐藤さんにも", "山田太郎", "佐藤"},
	{"arabic", "اتصل بمحمد غدا مع فاطمة", "محمد", "فاطمة"},
	{"hebrew and latin", "שלום David שלום דוד", "David", "דוד"},
}

func Test_Transform_Texts(t *testing.T) {
	for _, tc := range texts {
		before, after, _ := strings.Cut(tc.text, tc.found)
		person := found(t, tc.text, tc.found, "PERSON", 0.85)
		other := found(t, tc.text, tc.allowed, "PERSON", 0.85)
		characters := utf8.RuneCountInString(tc.found)

		t.Run(tc.name+"/a finding is replaced where it is, and nothing else changes", func(t *testing.T) {
			out := mustRewrite(t, withDefault(replaceWith("X")), Options{}, tc.text, person)
			require.Equal(t, before+"X"+after, out)
		})
		t.Run(tc.name+"/with nothing configured the entity type is written", func(t *testing.T) {
			out := mustRewrite(t, nil, Options{}, tc.text, person)
			require.Equal(t, before+"<PERSON>"+after, out)
		})
		t.Run(tc.name+"/a redacted finding is removed", func(t *testing.T) {
			out := mustRewrite(t, withDefault(redact()), Options{}, tc.text, person)
			require.Equal(t, before+after, out)
		})
		t.Run(tc.name+"/a mask without a count covers every character of the finding", func(t *testing.T) {
			out := mustRewrite(t, withDefault(maskAll("*")), Options{}, tc.text, person)
			require.Equal(t, before+strings.Repeat("*", characters)+after, out)
		})
		t.Run(tc.name+"/a hash stands where the finding was", func(t *testing.T) {
			out := mustRewrite(t, withDefault(hashOf(mgmtv1alpha1.PiiAnonymizer_Hash_HASH_TYPE_SHA256)), Options{}, tc.text, person)
			require.True(t, strings.HasPrefix(out, before) && strings.HasSuffix(out, after))
			require.Len(t, out, len(before)+64+len(after))
		})
		t.Run(tc.name+"/a transformer receives exactly the text of the finding", func(t *testing.T) {
			builder := &bracketing{}
			out := mustRewrite(t, withDefault(transformWith(passthrough)), Options{Build: builder.build}, tc.text, person)
			require.Equal(t, []string{tc.found}, builder.snippets)
			require.Equal(t, before+"["+tc.found+"]"+after, out)
		})
		t.Run(tc.name+"/an allowed phrase is kept and the other finding is rewritten", func(t *testing.T) {
			config := withDefault(replaceWith("X"))
			config.AllowedPhrases = []string{tc.allowed}
			out := mustRewrite(t, config, Options{}, tc.text, person, other)
			require.Equal(t, before+"X"+after, out)
			require.Contains(t, out, tc.allowed)
		})
	}
}

func Test_Transform_AllowedPhrases(t *testing.T) {
	allowing := func(phrases ...string) *mgmtv1alpha1.TransformPiiText {
		config := withDefault(replaceWith("X"))
		config.AllowedPhrases = phrases
		return config
	}

	t.Run("an allowed phrase after accented letters is kept", func(t *testing.T) {
		text := "Café: John called"
		out := mustRewrite(t, allowing("John"), Options{}, text, found(t, text, "John", "PERSON", 0.85))
		require.Equal(t, text, out)
	})

	t.Run("a finding after accented letters is rewritten when another word is allowed", func(t *testing.T) {
		text := "ééééé John Mary"
		out := mustRewrite(t, allowing("John"), Options{}, text, found(t, text, "Mary", "PERSON", 0.85))
		require.Equal(t, "ééééé John X", out)
	})

	t.Run("a phrase that is only part of a finding does not keep it", func(t *testing.T) {
		text := "Call John Smith"
		out := mustRewrite(t, allowing("John"), Options{}, text, found(t, text, "John Smith", "PERSON", 0.85))
		require.Equal(t, "Call X", out)
	})

	t.Run("a phrase in another case does not keep the finding", func(t *testing.T) {
		text := "Call John"
		out := mustRewrite(t, allowing("john"), Options{}, text, found(t, text, "John", "PERSON", 0.85))
		require.Equal(t, "Call X", out)
	})

	t.Run("a phrase with a space around it does not keep the finding", func(t *testing.T) {
		text := "Call John"
		out := mustRewrite(t, allowing("John "), Options{}, text, found(t, text, "John", "PERSON", 0.85))
		require.Equal(t, "Call X", out)
	})

	t.Run("the same phrase in another normalization form is kept", func(t *testing.T) {
		decomposed := "José called"
		out := mustRewrite(t, allowing("José"), Options{}, decomposed, found(t, decomposed, "José", "PERSON", 0.85))
		require.Equal(t, decomposed, out)

		composed := "José called"
		out = mustRewrite(t, allowing("José"), Options{}, composed, found(t, composed, "José", "PERSON", 0.85))
		require.Equal(t, composed, out)
	})

	t.Run("every finding of an allowed phrase is kept, whatever its entity type", func(t *testing.T) {
		text := "Call John"
		out := mustRewrite(t, allowing("John"), Options{}, text,
			found(t, text, "John", "PERSON", 0.85), found(t, text, "John", "LOCATION", 0.4))
		require.Equal(t, text, out)
	})
}

func Test_Transform_Calls(t *testing.T) {
	t.Run("an empty value makes no call", func(t *testing.T) {
		transformer, err := newEngine(t, presidiotest.New(t)).Transformer(nil, Options{})
		require.NoError(t, err)
		out, err := transformer.Transform(context.Background(), "")
		require.NoError(t, err)
		require.Empty(t, out)
	})

	t.Run("a value makes one analyzer call", func(t *testing.T) {
		text := "Call Bob now"
		fake := finding(t, found(t, text, "Bob", "PERSON", 0.85))
		transformer, err := newEngine(t, fake).Transformer(nil, Options{})
		require.NoError(t, err)
		_, err = transformer.Transform(context.Background(), text)
		require.NoError(t, err)
		require.Equal(t, 1, fake.Calls().Analyze)
	})

	t.Run("a value with no finding is returned as it is", func(t *testing.T) {
		text := "Très cher 👩‍💻, rien à signaler"
		require.Equal(t, text, mustRewrite(t, withDefault(redact()), Options{}, text))
	})

	t.Run("one transformer serves several values at once", func(t *testing.T) {
		text := "Call Bob now"
		transformer, err := newEngine(t, finding(t, found(t, text, "Bob", "PERSON", 0.85))).
			Transformer(withDefault(transformWith(nil)), Options{Build: (&constant{out: "N"}).build})
		require.NoError(t, err)
		done := make(chan string, 8)
		for range cap(done) {
			go func() {
				out, _ := transformer.Transform(context.Background(), text)
				done <- out
			}()
		}
		for range cap(done) {
			require.Equal(t, "Call N now", <-done)
		}
	})
}

func Test_Transform_Failures(t *testing.T) {
	text := "Call Bob and Zoé now"
	bob := found(t, text, "Bob", "PERSON", 0.85)
	zoe := found(t, text, "Zoé", "PERSON", 0.85)

	failing := func(analyze func(context.Context, *presidio.AnalyzeRequest) ([]presidio.Finding, error)) (string, error) {
		fake := presidiotest.New(t)
		fake.OnAnalyze(analyze)
		transformer, err := newEngine(t, fake).Transformer(nil, Options{})
		require.NoError(t, err)
		return transformer.Transform(context.Background(), text)
	}

	t.Run("an analyzer that did not answer fails the value, and stays the cause", func(t *testing.T) {
		out, err := failing(func(context.Context, *presidio.AnalyzeRequest) ([]presidio.Finding, error) {
			return nil, errors.Join(presidio.ErrNoAnswer, errors.New("connection refused"))
		})
		require.ErrorIs(t, err, presidio.ErrNoAnswer)
		require.ErrorContains(t, err, "unable to analyze input")
		require.Empty(t, out)
	})

	t.Run("an analyzer that refused fails the value, with its status and its words", func(t *testing.T) {
		out, err := failing(func(context.Context, *presidio.AnalyzeRequest) ([]presidio.Finding, error) {
			return nil, &presidio.RefusedError{Operation: "analyze", StatusCode: 500, Message: "No matching recognizers"}
		})
		var refused *presidio.RefusedError
		require.ErrorAs(t, err, &refused)
		require.Equal(t, 500, refused.StatusCode)
		require.Equal(t, "No matching recognizers", refused.Message)
		require.Empty(t, out)
	})

	for name, inconsistent := range map[string]presidio.Finding{
		"a finding that ends past the text":       {EntityType: "PERSON", Start: 5, End: 400, Score: 1},
		"a finding that starts before the text":   {EntityType: "PERSON", Start: -1, End: 3, Score: 1},
		"a finding that starts after its end":     {EntityType: "PERSON", Start: 8, End: 5, Score: 1},
		"a finding past the text in characters":   {EntityType: "PERSON", Start: 5, End: len(text), Score: 1},
		"a finding that starts past the text":     {EntityType: "PERSON", Start: 300, End: 300, Score: 1},
		"a finding with no entity type":           {EntityType: "", Start: 5, End: 8, Score: 1},
		"a finding next to one that is rewritten": {EntityType: "PERSON", Start: 5, End: 400, Score: 1},
	} {
		t.Run(name+" fails the value", func(t *testing.T) {
			out, err := rewrite(t, nil, Options{}, text, bob, inconsistent)
			require.ErrorIs(t, err, presidio.ErrInvalidResponse)
			require.Empty(t, out)
		})
	}

	t.Run("a transformer that fails on one finding fails the value", func(t *testing.T) {
		boom := errors.New("boom")
		calls := 0
		build := func(context.Context, *mgmtv1alpha1.TransformerConfig) (SnippetTransformer, error) {
			return func(_ context.Context, snippet string) (string, error) {
				calls++
				if snippet == "Zoé" {
					return "", boom
				}
				return "N", nil
			}, nil
		}
		out, err := rewrite(t, withDefault(transformWith(passthrough)), Options{Build: build}, text, bob, zoe)
		require.ErrorIs(t, err, boom)
		require.ErrorContains(t, err, "unable to transform PERSON entity")
		require.Empty(t, out, "no part of a value that failed is returned")
		require.Equal(t, 2, calls)
	})

	t.Run("a transformer that cannot be built fails the value", func(t *testing.T) {
		boom := errors.New("no such transformer")
		build := func(context.Context, *mgmtv1alpha1.TransformerConfig) (SnippetTransformer, error) {
			return nil, boom
		}
		out, err := rewrite(t, withDefault(transformWith(passthrough)), Options{Build: build}, text, bob)
		require.ErrorIs(t, err, boom)
		require.Empty(t, out)
	})

	t.Run("a transform operator with nothing to run transformers fails the value", func(t *testing.T) {
		out, err := rewrite(t, withDefault(transformWith(passthrough)), Options{}, text, bob)
		require.Error(t, err)
		require.Empty(t, out)
	})

	t.Run("an error never quotes the value", func(t *testing.T) {
		_, err := rewrite(t, nil, Options{}, text, presidio.Finding{EntityType: "PERSON", Start: 5, End: 400})
		require.NotContains(t, err.Error(), "Bob")
	})
}

func Test_Transform_InvalidBytes(t *testing.T) {
	t.Run("a byte that is no character counts as one, as the analyzer is sent it", func(t *testing.T) {
		text := "caf\xff Bob"
		out := mustRewrite(t, nil, Options{}, text, presidio.Finding{EntityType: "PERSON", Start: 5, End: 8, Score: 1})
		require.Equal(t, "caf\xff <PERSON>", out)
	})
}
