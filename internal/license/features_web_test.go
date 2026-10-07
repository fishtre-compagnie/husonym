package license

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// webFeaturesFile is where the web app lists the features, from this package's directory.
var webFeaturesFile = filepath.Join("..", "..", "frontend", "apps", "web", "libs", "license", "license.ts")

var (
	// webFeaturesList is the array literal the web app declares its features with.
	webFeaturesList = regexp.MustCompile(`(?s)export const LICENSE_FEATURES\s*=\s*\[(.*?)\]`)
	// webFeatureName is one quoted name of that literal.
	webFeatureName = regexp.MustCompile(`'([^']*)'`)
)

// The License page of the web app shows the features by a list of its own, which nothing else
// holds to this one: a feature added here and forgotten there would be missing from the page,
// with no test of either side failing. The list of the web app is read from its source and
// must be the one this package declares, in the same order.
func Test_AllFeatures_AreTheOnesTheWebAppLists(t *testing.T) {
	source, err := os.ReadFile(webFeaturesFile)
	require.NoError(t, err, "the web app no longer lists its features in %s", webFeaturesFile)

	list := webFeaturesList.FindSubmatch(source)
	require.NotNil(t, list, "no LICENSE_FEATURES array literal in %s", webFeaturesFile)

	var listed []Feature
	for _, name := range webFeatureName.FindAllSubmatch(list[1], -1) {
		listed = append(listed, Feature(name[1]))
	}
	require.Equal(t, AllFeatures(), listed)
}
