// Package presidiotest stands in for Presidio in the tests of the packages that call it.
package presidiotest

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
)

var (
	_ presidio.Analyzer     = (*Fake)(nil)
	_ presidio.EntityLister = (*Fake)(nil)
)

// Fake is a Presidio that answers what the test tells it to, and counts what it is asked. An
// operation the test gave no answer for fails the test when it is called: a test that sets
// none proves Presidio is not called.
type Fake struct {
	t testing.TB

	mu                sync.Mutex
	analyze           func(context.Context, *presidio.AnalyzeRequest) ([]presidio.Finding, error)
	supportedEntities func(context.Context, string) ([]string, error)
	calls             Calls
}

// Calls is how many times each operation was called.
type Calls struct {
	Analyze           int
	SupportedEntities int
}

// New returns a Presidio no call is expected of.
func New(t testing.TB) *Fake {
	return &Fake{t: t}
}

// Finding returns a Presidio that finds part, as an entity of the given type, in every text
// that carries it, and nothing in the others. Like Presidio it counts positions in characters.
func Finding(t testing.TB, entityType, part string) *Fake {
	fake := New(t)
	fake.OnAnalyze(func(_ context.Context, req *presidio.AnalyzeRequest) ([]presidio.Finding, error) {
		at := strings.Index(req.Text, part)
		if at < 0 {
			return []presidio.Finding{}, nil
		}
		start := utf8.RuneCountInString(req.Text[:at])
		return []presidio.Finding{{
			EntityType: entityType,
			Start:      start,
			End:        start + utf8.RuneCountInString(part),
			Score:      0.85,
		}}, nil
	})
	return fake
}

// OnAnalyze sets what Analyze answers from now on.
func (f *Fake) OnAnalyze(answer func(context.Context, *presidio.AnalyzeRequest) ([]presidio.Finding, error)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.analyze = answer
}

// OnSupportedEntities sets what SupportedEntities answers from now on.
func (f *Fake) OnSupportedEntities(answer func(context.Context, string) ([]string, error)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.supportedEntities = answer
}

// Calls returns how many times each operation was called so far.
func (f *Fake) Calls() Calls {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *Fake) Analyze(ctx context.Context, req *presidio.AnalyzeRequest) ([]presidio.Finding, error) {
	f.mu.Lock()
	answer := f.analyze
	f.calls.Analyze++
	f.mu.Unlock()
	if answer == nil {
		return nil, f.unexpected("Analyze")
	}
	return answer(ctx, req)
}

func (f *Fake) SupportedEntities(ctx context.Context, language string) ([]string, error) {
	f.mu.Lock()
	answer := f.supportedEntities
	f.calls.SupportedEntities++
	f.mu.Unlock()
	if answer == nil {
		return nil, f.unexpected("SupportedEntities")
	}
	return answer(ctx, language)
}

func (f *Fake) unexpected(operation string) error {
	f.t.Helper()
	f.t.Errorf("presidiotest: unexpected call to %s", operation)
	return errors.New("presidiotest: unexpected call to " + operation)
}
