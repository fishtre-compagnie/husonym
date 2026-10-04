package profile

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func mask(value string) string {
	layout, _ := layoutOf(value)
	return layout
}

// A mask that gathers no character gives the class of each one.
func Test_LayoutOf_Gathers(t *testing.T) {
	for value, want := range map[string]bool{
		"aB3$": false, "M.": false, "a@b.c": false, "x": false, "": false, "-": false,
		"ab": true, "a  b": true, "A1b22": true, "M..": false, "$$": true,
	} {
		_, gathers := layoutOf(value)
		require.Equal(t, want, gathers, value)
	}
}

// A mask keeps the layout of a value and none of its characters.
func Test_Mask(t *testing.T) {
	for value, want := range map[string]string{
		"jean.dupont@example.org":           "a+.a+@a+.a+",
		"FR76 3000 6000 0112 3456 7890 189": "A+9+ 9+ 9+ 9+ 9+ 9+ 9+",
		"1985-03-12":                        "9+-9+-9+",
		"+33 (0)6 12-34":                    "+9+ (9+)9+ 9+-9+",
		"M.":                                "A+.",
		"a   b":                             "a+ a+",
		"Éloïse":                            "A+a+",
		"50%!":                              "9+?+",
		"a_b/c:d,e#f":                       "a+_a+/a+:a+,a+#a+",
		"":                                  "",
	} {
		require.Equal(t, want, mask(value), value)
	}
}

func Test_Mask_IsCutAt32Characters(t *testing.T) {
	long := mask(strings.Repeat("a1", 40))
	require.Len(t, long, 32)
	require.Equal(t, strings.Repeat("a+9+", 8), long)
}

// A value made of punctuation only has no letter and no digit to stand for: its mask
// would be the value itself. It is written as one run of characters that are not shown.
func Test_Mask_NeverSpellsAValue(t *testing.T) {
	for _, value := range []string{":-)", "-", "(+)", "#", "..", "@", "+ -", "§", "?", "?!"} {
		require.Equal(t, "?+", mask(value), value)
	}
}
