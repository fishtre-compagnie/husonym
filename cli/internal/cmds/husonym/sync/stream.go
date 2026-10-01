package sync_cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	benthosstream "github.com/fishtre-compagnie/husonym/internal/benthos-stream"
	"github.com/redpanda-data/benthos/v4/public/service"
)

// streamStopBudget is how long a stream is given to stop by itself before it is closed.
const streamStopBudget = 1 * time.Millisecond

// newStream builds the stream of one table from its config. Streams are built one at a time,
// and the lock is let go however the build ends: a table whose config is refused must not hold
// the tables that come after it.
func newStream(benv *service.Environment, configYAML string, logger *slog.Logger) (*service.Stream, error) {
	if benv == nil {
		return nil, errors.New("benthos env is nil")
	}
	streamBuilderMu.Lock()
	defer streamBuilderMu.Unlock()

	streambldr := benv.NewStreamBuilder()
	if streambldr == nil {
		return nil, errors.New("failed to create StreamBuilder")
	}
	if logger != nil {
		streambldr.SetLogger(logger)
	}
	if err := streambldr.SetYAML(configYAML); err != nil {
		return nil, fmt.Errorf("unable to convert benthos config to yaml for stream builder: %w", err)
	}
	return streambldr.Build()
}

// runStream runs the stream of one table until it ends, and stops it when the sync does. Run
// returns on a canceled context without stopping the stream it started: the stream is stopped
// here, or it would go on writing the destination of a sync that has failed. A sync that has
// ended runs no more stream.
func runStream(ctx context.Context, stream benthosstream.BenthosStreamClient, logger *slog.Logger) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("the sync ended before the stream started: %w", err)
	}
	if err := stream.Run(ctx); err != nil {
		// Whether it stopped is not told: a stream that failed to start has nothing to stop,
		// and one given no time to stop by itself is closed.
		if stopErr := stream.StopWithin(streamStopBudget); stopErr != nil {
			logger.Debug("the stream was asked to stop", "answer", stopErr)
		}
		return fmt.Errorf("unable to run benthos stream: %w", err)
	}
	return nil
}
