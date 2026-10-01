package benthosstream

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/redpanda-data/benthos/v4/public/service"
)

type BenthosStreamClient interface {
	Run(ctx context.Context) error
	Stop(ctx context.Context) error
	StopWithin(d time.Duration) error
}

type BenthosStreamManagerClient interface {
	NewBenthosStreamFromBuilder(streambldr *service.StreamBuilder) (BenthosStreamClient, error)
}

type BenthosStreamManager struct{}

func NewBenthosStreamManager() *BenthosStreamManager {
	return &BenthosStreamManager{}
}

func (b *BenthosStreamManager) NewBenthosStreamFromBuilder(
	streambldr *service.StreamBuilder,
) (BenthosStreamClient, error) {
	stream, err := streambldr.Build()
	if err != nil {
		return nil, err
	}
	return NewBenthosStreamAdapter(stream), nil
}

// BenthosStreamAdapter runs one stream, and stops it for good: a stream asked to stop before it
// ran never starts, and one whose stop came too early to be heard is stopped by the next.
type BenthosStreamAdapter struct {
	mu     sync.Mutex
	Stream *service.Stream

	// running says Run was let through, and stopAsked that a stop was asked, heard or not.
	running   bool
	stopAsked bool
}

func NewBenthosStreamAdapter(stream *service.Stream) *BenthosStreamAdapter {
	return &BenthosStreamAdapter{Stream: stream}
}

// ErrStoppedBeforeRun is returned by Run for a stream that was asked to stop before it ran.
var ErrStoppedBeforeRun = errors.New("the benthos stream was stopped before it ran")

func (b *BenthosStreamAdapter) Run(ctx context.Context) error {
	b.mu.Lock()
	stream, stopAsked := b.Stream, b.stopAsked
	if stream != nil && !stopAsked {
		b.running = true
	}
	b.mu.Unlock()

	if stream == nil {
		return fmt.Errorf("benthos stream is nil during Run")
	}
	if stopAsked {
		return ErrStoppedBeforeRun
	}
	return stream.Run(ctx)
}

func (b *BenthosStreamAdapter) Stop(ctx context.Context) error {
	return b.stop(func(stream *service.Stream) error { return stream.Stop(ctx) })
}

func (b *BenthosStreamAdapter) StopWithin(d time.Duration) error {
	return b.stop(func(stream *service.Stream) error { return stream.StopWithin(d) })
}

// stop stops the stream. One that never ran has nothing to stop, and will not run. One that is
// starting may not hear the stop yet: the stop asked again once it runs reaches it, a stream
// being stopped as often as it is asked to.
func (b *BenthosStreamAdapter) stop(stop func(*service.Stream) error) error {
	b.mu.Lock()
	b.stopAsked = true
	stream, running := b.Stream, b.running
	b.mu.Unlock()

	// The lock is not held while the stream stops: a stop that takes its time holds no other.
	if stream == nil || !running {
		return nil
	}
	return stop(stream)
}
