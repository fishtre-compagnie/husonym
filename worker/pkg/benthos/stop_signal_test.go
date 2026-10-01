package husonym_benthos

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The first signal is kept, and one that finds the channel full is dropped: its sender goes on.
func Test_SignalStop(t *testing.T) {
	first, later := errors.New("first"), errors.New("later")
	stop := make(chan error, 1)

	sent := make(chan struct{})
	go func() {
		defer close(sent)
		SignalStop(stop, first)
		SignalStop(stop, later)
	}()
	select {
	case <-sent:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "the signal waits for a listener")
	}

	select {
	case got := <-stop:
		require.Equal(t, first, got)
	default:
		require.FailNow(t, "the first signal was dropped")
	}
	require.Empty(t, stop)
}
