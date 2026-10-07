package licenserefusal

import (
	"context"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/stretchr/testify/require"
)

type blockingCounter struct{ called int }

func (c *blockingCounter) CountRefusal(ctx context.Context, _ string, _ []license.Gate, _ time.Time) error {
	c.called++
	<-ctx.Done()
	return ctx.Err()
}

func Test_Count_IsBounded(t *testing.T) {
	previous := countTimeout
	countTimeout = 50 * time.Millisecond
	t.Cleanup(func() { countTimeout = previous })
	counter := &blockingCounter{}

	started := time.Now()
	Count(context.Background(), counter, "00000000-0000-0000-0000-000000000001", []license.Gate{license.GateNotInForce})

	require.Equal(t, 1, counter.called)
	require.Less(t, time.Since(started), 2*time.Second)
}

func Test_Count_SkipsWhatNamesNoAccountOrNoGate(t *testing.T) {
	counter := &blockingCounter{}

	Count(context.Background(), counter, "", []license.Gate{license.GateNotInForce})
	Count(context.Background(), counter, "00000000-0000-0000-0000-000000000001", nil)

	require.Zero(t, counter.called)
}

func Test_Count_AServiceWithoutACounterStillAnswers(t *testing.T) {
	require.NotPanics(t, func() {
		Count(context.Background(), nil, "00000000-0000-0000-0000-000000000001", []license.Gate{license.GateNotInForce})
	})
}

func Test_Count_SkipsAnAccountThatIsNotAUuid(t *testing.T) {
	counter := &blockingCounter{}

	Count(context.Background(), counter, "not-a-uuid", []license.Gate{license.GateNotInForce})

	require.Zero(t, counter.called)
}
