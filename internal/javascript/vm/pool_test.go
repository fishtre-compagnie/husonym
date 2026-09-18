package javascript_vm

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

// An item goes back to the pool for the next Get, and stays there across garbage
// collections.
func TestPool(t *testing.T) {
	built := 0
	pool := NewPool(func() (*int, error) {
		built++
		item := built
		return &item, nil
	})

	first, err := pool.Get()
	require.NoError(t, err)
	second, err := pool.Get()
	require.NoError(t, err)
	require.Equal(t, 2, built, "two items in use at once")

	pool.Put(first)
	pool.Put(second)
	runtime.GC()
	runtime.GC()
	for range 3 {
		item, err := pool.Get()
		require.NoError(t, err)
		pool.Put(item)
	}
	require.Equal(t, 2, built)
}
