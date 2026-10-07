package hooks_test

import (
	"slices"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/stretchr/testify/require"
)

// kindOf gives the feature of the kind of hook an operation is about, the feature of the
// other kind, and what is answered to whoever lacks the first.
func kindOf(op *operation) (own, other license.Feature, refusal string) {
	if strings.Contains(op.name, "JobHook") {
		return license.FeatureJobHooks, license.FeatureAccountHooks, "this license does not include job_hooks"
	}
	return license.FeatureAccountHooks, license.FeatureJobHooks, "this license does not include account_hooks"
}

// Each kind of hook is a feature of its own: creating, changing and turning on a hook ask the
// license for the feature of its kind, and for that one only. Reading, turning off and
// deleting ask for none, so that an account whose license no longer includes its hooks can
// still see them and take them out of its runs.
func TestAHookAsksForTheFeatureOfItsKind(t *testing.T) {
	for i := range operations {
		op := &operations[i]
		if op.cells[allGood].code != 0 {
			continue // retired: answered the same to everyone
		}
		own, other, refusal := kindOf(op)
		gated := slices.Contains(op.asks, "license")

		t.Run(op.name+"/the license lacks the feature of its kind", func(t *testing.T) {
			w := newWorld(t)
			w.papers.lacks = []license.Feature{own}

			_, err := op.call(t.Context(), w, w.own, false)

			if !gated {
				require.NoError(t, err)
				require.Equal(t, op.cells[allGood].wrote, w.store.writes > 0, "writes: %d", w.store.writes)
				return
			}
			requireAnswer(t, err, connect.CodePermissionDenied, refusal)
			require.Zero(t, w.store.writes)
			// The feature is asked last, once the license as a whole was.
			require.Equal(t, op.asks, append([]string{}, w.trail...), "what was asked before the refusal")
		})

		t.Run(op.name+"/the license lacks the feature of the other kind", func(t *testing.T) {
			w := newWorld(t)
			w.papers.lacks = []license.Feature{other}

			_, err := op.call(t.Context(), w, w.own, false)

			require.NoError(t, err)
			require.Equal(t, op.cells[allGood].wrote, w.store.writes > 0, "writes: %d", w.store.writes)
		})
	}
}
