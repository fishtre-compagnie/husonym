package javascript_draft

import (
	"strings"
	"testing"

	pseudo_functions "github.com/fishtre-compagnie/husonym/internal/javascript/functions/pseudo"
	"github.com/stretchr/testify/require"
)

func column() *ColumnFacts {
	return &ColumnFacts{
		Schema: "public",
		Table:  "users",
		Column: "email",
		Mode:   ModeTransform,
		Engine: EngineAthanor,
	}
}

func Test_BuildPrompt_Contract(t *testing.T) {
	t.Run("transform hands over the row as well as the value", func(t *testing.T) {
		prompt := BuildPrompt(column())
		require.Contains(t, prompt, "function fn(value, input)")
		require.Contains(t, prompt, "the whole source row")
	})

	t.Run("generate takes no arguments", func(t *testing.T) {
		facts := column()
		facts.Mode = ModeGenerate
		prompt := BuildPrompt(facts)
		require.Contains(t, prompt, "function fn(){ ... }")
		require.NotContains(t, prompt, "function fn(value, input)")
	})

	t.Run("names what the sandbox does not have", func(t *testing.T) {
		prompt := BuildPrompt(column())
		// The failures worth pre-empting are the ones a model cannot guess: it reaches for
		// crypto to hash an identifier, and for require to import a faker.
		require.Contains(t, prompt, "no `crypto`")
		require.Contains(t, prompt, "no `require`")
	})

	t.Run("same facts, same prompt", func(t *testing.T) {
		require.Equal(t, BuildPrompt(column()), BuildPrompt(column()))
	})
}

func Test_BuildPrompt_Rules(t *testing.T) {
	t.Run("a unique column forbids a constant and a random draw", func(t *testing.T) {
		facts := column()
		facts.IsUnique = true
		prompt := BuildPrompt(facts)
		// This is the rule that would have caught the constant login that broke a job on a
		// unique index: stating the constraint is not enough, the consequence has to be spelt.
		require.Contains(t, prompt, "never a constant")
		require.Contains(t, prompt, "which collides")
	})

	t.Run("a foreign key demands the parent's own output", func(t *testing.T) {
		facts := column()
		facts.ForeignKey = &ForeignKey{Schema: "public", Table: "companies", Column: "id"}
		prompt := BuildPrompt(facts)
		require.Contains(t, prompt, "public.companies.id")
		require.Contains(t, prompt, "the row loses its parent")
	})

	t.Run("a non nullable column may not return null", func(t *testing.T) {
		facts := column()
		facts.IsNullable = false
		require.Contains(t, BuildPrompt(facts), "Never return null")
	})

	t.Run("a nullable column keeps its nulls", func(t *testing.T) {
		facts := column()
		facts.IsNullable = true
		require.Contains(t, BuildPrompt(facts), "A null input stays null")
	})

	t.Run("a bounded type bounds the output", func(t *testing.T) {
		facts := column()
		length := int32(20)
		facts.MaxLength = &length
		require.Contains(t, BuildPrompt(facts), "at most 20 characters")
	})

	t.Run("a generated column is not drafted for", func(t *testing.T) {
		facts := column()
		facts.IsGenerated = true
		require.Contains(t, BuildPrompt(facts), "Do not draft a rule for it")
	})
}

func Test_BuildPrompt_Functions(t *testing.T) {
	t.Run("athanor offers every kind the namespace defines", func(t *testing.T) {
		prompt := BuildPrompt(column())
		// Derived, not copied: a kind added to the namespace has to show up here without anyone
		// remembering this file exists.
		for _, kind := range pseudo_functions.Kinds {
			require.Contains(t, prompt, kind)
		}
		require.Contains(t, prompt, "pseudo.hash(value, domain)")
		require.Contains(t, prompt, "pseudo.int(value, domain, min, max)")
		require.Contains(t, prompt, "pseudo.pick(list, value, domain)")
	})

	t.Run("benthos says the namespace is out of reach, and why", func(t *testing.T) {
		facts := column()
		facts.Engine = EngineBenthos
		prompt := BuildPrompt(facts)
		require.Contains(t, prompt, "not available on this engine")
		require.Contains(t, prompt, "no consistency scope")
		require.NotContains(t, prompt, "pseudo.hash(value, domain)")
	})
}

func Test_BuildPrompt_CarriesNoValues(t *testing.T) {
	// The prompt is the one place where a model asked to anonymize data could be handed the
	// data instead. Nothing in ColumnFacts holds a value, and the prompt says so, so that adding
	// such a field has to be a deliberate act against a red bar.
	facts := column()
	facts.PiiCategory = "contact"
	prompt := BuildPrompt(facts)

	require.Contains(t, prompt, "You are not shown any of the column's values")
	require.False(t, strings.Contains(prompt, "@example.com"))
}

func Test_BuildPrompt_GenerateRefusesWhatItCannotSatisfy(t *testing.T) {
	// Generate mode receives no input, so "derive the output from the input" is not a rule it
	// can follow. Restating it would get back a draft that says `value` and throws on row one.
	for _, tc := range []struct {
		name  string
		apply func(*ColumnFacts)
	}{
		{"unique", func(f *ColumnFacts) { f.IsUnique = true }},
		{"foreign key", func(f *ColumnFacts) {
			f.ForeignKey = &ForeignKey{Schema: "public", Table: "companies", Column: "id"}
		}},
		{"referenced by others", func(f *ColumnFacts) { f.ReferencedBy = 3 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			facts := column()
			facts.Mode = ModeGenerate
			tc.apply(facts)
			prompt := BuildPrompt(facts)

			require.Contains(t, prompt, "needs a transform rule instead")
			// None of the rules that assume an input may survive alongside the refusal.
			require.NotContains(t, prompt, "Derive it from the input")
			require.NotContains(t, prompt, "the same input must always give the same output")
			require.NotContains(t, prompt, "the row loses its parent")
		})
	}

	t.Run("transform mode still states them", func(t *testing.T) {
		facts := column()
		facts.Mode = ModeTransform
		facts.IsUnique = true
		prompt := BuildPrompt(facts)
		require.Contains(t, prompt, "Derive it from the input")
		require.NotContains(t, prompt, "needs a transform rule instead")
	})
}
