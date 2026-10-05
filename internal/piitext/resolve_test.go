package piitext

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
	"github.com/stretchr/testify/require"
)

func Test_Resolve(t *testing.T) {
	// James 0..5, Bond 6..10, Jörg 15..19, Müller 20..26, in characters.
	text := "James Bond met Jörg Müller"
	at := func(entity string, start, end int, score float64) presidio.Finding {
		return presidio.Finding{EntityType: entity, Start: start, End: end, Score: score}
	}

	for _, tc := range []struct {
		name     string
		text     string
		findings []presidio.Finding
		want     string
	}{
		{
			"findings of one type that overlap are one finding",
			text, []presidio.Finding{at("PERSON", 0, 8, 0.8), at("PERSON", 6, 10, 0.8)},
			"<PERSON> met Jörg Müller",
		},
		{
			"findings of one type that overlap in a chain are one finding",
			text, []presidio.Finding{at("PERSON", 7, 10, 0.8), at("PERSON", 0, 4, 0.8), at("PERSON", 3, 8, 0.8)},
			"<PERSON> met Jörg Müller",
		},
		{
			"a finding inside another of its type is rewritten with all of it",
			text, []presidio.Finding{at("PERSON", 0, 10, 0.8), at("PERSON", 6, 8, 0.8)},
			"<PERSON> met Jörg Müller",
		},
		{
			"a finding inside another of its type that starts with it is rewritten with all of it",
			text, []presidio.Finding{at("PERSON", 0, 3, 0.9), at("PERSON", 0, 10, 0.4)},
			"<PERSON> met Jörg Müller",
		},
		{
			"findings of one type that touch stay two",
			"JamesBond", []presidio.Finding{at("PERSON", 0, 5, 0.8), at("PERSON", 5, 9, 0.8)},
			"<PERSON><PERSON>",
		},
		{
			"a finding inside another is dropped, whatever its score",
			text, []presidio.Finding{at("PERSON", 0, 10, 0.4), at("LOCATION", 6, 10, 1)},
			"<PERSON> met Jörg Müller",
		},
		{
			"a finding inside another that starts with it is dropped",
			text, []presidio.Finding{at("LOCATION", 0, 5, 1), at("PERSON", 0, 10, 0.4)},
			"<PERSON> met Jörg Müller",
		},
		{
			"of two findings on the same characters the higher score stays",
			text, []presidio.Finding{at("PERSON", 0, 5, 0.4), at("LOCATION", 0, 5, 0.9)},
			"<LOCATION> Bond met Jörg Müller",
		},
		{
			"of two findings on the same characters with one score the first entity name stays",
			text, []presidio.Finding{at("PERSON", 0, 5, 0.8), at("LOCATION", 0, 5, 0.8)},
			"<LOCATION> Bond met Jörg Müller",
		},
		{
			"the order of the findings does not change which one stays",
			text, []presidio.Finding{at("LOCATION", 0, 5, 0.8), at("PERSON", 0, 5, 0.8)},
			"<LOCATION> Bond met Jörg Müller",
		},
		{
			"findings of two types that overlap are both rewritten, side by side",
			text, []presidio.Finding{at("PERSON", 0, 8, 0.9), at("LOCATION", 6, 10, 0.4)},
			"<PERSON><LOCATION> met Jörg Müller",
		},
		{
			"findings of three types that overlap in a chain are all rewritten",
			text, []presidio.Finding{at("PERSON", 0, 8, 0.5), at("LOCATION", 6, 12, 0.9), at("ORGANIZATION", 11, 19, 0.7)},
			"<PERSON><LOCATION><ORGANIZATION> Müller",
		},
		{
			"a finding whose characters all belong to its neighbors is dropped",
			text, []presidio.Finding{at("PERSON", 0, 8, 0.9), at("LOCATION", 6, 11, 0.4), at("ORGANIZATION", 8, 14, 0.9)},
			"<PERSON><ORGANIZATION> Jörg Müller",
		},
		{
			"findings of one type separated by a space are one finding",
			text, []presidio.Finding{at("PERSON", 0, 5, 0.8), at("PERSON", 6, 10, 0.8)},
			"<PERSON> met Jörg Müller",
		},
		{
			"findings of one type separated by a space after accented letters are one finding",
			text, []presidio.Finding{at("PERSON", 20, 26, 0.8), at("PERSON", 15, 19, 0.8)},
			"James Bond met <PERSON>",
		},
		{
			"findings of one type separated by several spaces are one finding",
			"James  Bond", []presidio.Finding{at("PERSON", 0, 5, 0.8), at("PERSON", 7, 11, 0.8)},
			"<PERSON>",
		},
		{
			"three findings of one type separated by spaces are one finding",
			"Ian Lancaster Fleming wrote",
			[]presidio.Finding{at("PERSON", 0, 3, 0.8), at("PERSON", 4, 13, 0.8), at("PERSON", 14, 21, 0.8)},
			"<PERSON> wrote",
		},
		{
			"findings of one type separated by a tab stay two",
			"James\tBond", []presidio.Finding{at("PERSON", 0, 5, 0.8), at("PERSON", 6, 10, 0.8)},
			"<PERSON>\t<PERSON>",
		},
		{
			"findings of one type separated by a no-break space stay two",
			"James Bond", []presidio.Finding{at("PERSON", 0, 5, 0.8), at("PERSON", 6, 10, 0.8)},
			"<PERSON> <PERSON>",
		},
		{
			"findings of two types separated by a space stay two",
			text, []presidio.Finding{at("PERSON", 0, 5, 0.8), at("LOCATION", 6, 10, 0.8)},
			"<PERSON> <LOCATION> met Jörg Müller",
		},
		{
			"a finding of no character is dropped",
			text, []presidio.Finding{at("PERSON", 5, 5, 0.8)},
			text,
		},
		{
			"the same finding twice is one finding",
			text, []presidio.Finding{at("PERSON", 0, 5, 0.8), at("PERSON", 0, 5, 0.8)},
			"<PERSON> Bond met Jörg Müller",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, mustRewrite(t, nil, Options{}, tc.text, tc.findings...))
		})
	}

	t.Run("an overlap leaves each operator the characters its finding owns", func(t *testing.T) {
		config := withDefault(replaceByEntity())
		config.EntityAnonymizers = map[string]*mgmtv1alpha1.PiiAnonymizer{"PERSON": mask("*", 3, false)}
		out := mustRewrite(t, config, Options{}, text, at("PERSON", 0, 8, 0.4), at("LOCATION", 6, 10, 0.9))
		require.Equal(t, "***es <LOCATION> met Jörg Müller", out)
	})

	t.Run("findings merged over a space are handed to a transformer as one text", func(t *testing.T) {
		builder := &bracketing{}
		out := mustRewrite(t, withDefault(transformWith(passthrough)), Options{Build: builder.build}, text,
			at("PERSON", 15, 19, 0.8), at("PERSON", 20, 26, 0.8))
		require.Equal(t, "James Bond met [Jörg Müller]", out)
	})
}
