package perf

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// workerContainer is the container each run is measured on. A run starts on a worker that
// has just been restarted: a pool of connections already open, or a heap already grown,
// would measure the run before rather than this one.
func workerContainer() string {
	if name := os.Getenv("BENCH_WORKER_CONTAINER"); name != "" {
		return name
	}
	return "husonym-worker"
}

// RestartWorker restarts the worker and waits until it is listening again.
func RestartWorker(ctx context.Context) error {
	name := workerContainer()
	if out, err := exec.CommandContext(ctx, "docker", "restart", name).CombinedOutput(); err != nil {
		return fmt.Errorf("perf: restart %s: %w: %s", name, err, out)
	}
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		out, err := exec.CommandContext(ctx, "docker", "logs", "--since", "30s", name).CombinedOutput()
		if err == nil && strings.Contains(string(out), "Started Worker") {
			// The worker announces itself before it polls its first task.
			time.Sleep(2 * time.Second)
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("perf: %s did not come back within five minutes", name)
}

// MemoryWatch follows the memory of the worker container while a run goes on.
//
// The container holds a Go toolchain and the reloader that rebuilds the worker, and their
// memory dwarfs the run: what is measured is therefore the difference between the highest
// value seen during the run and the one just before it started. The peak of the control
// group cannot be reset from inside the container, hence the sampling.
type MemoryWatch struct {
	baseline, peak int64
	stop           chan struct{}
	done           chan struct{}
}

// WatchMemory starts following the memory of the worker container.
func WatchMemory(ctx context.Context) *MemoryWatch {
	w := &MemoryWatch{stop: make(chan struct{}), done: make(chan struct{})}
	w.baseline = currentMemory(ctx)
	w.peak = w.baseline
	go func() {
		defer close(w.done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-w.stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if current := currentMemory(ctx); current > w.peak {
					w.peak = current
				}
			}
		}
	}()
	return w
}

// Stop ends the watch and returns the highest memory seen and what the run added to the
// memory the container already held.
func (w *MemoryWatch) Stop() (peak, added int64) {
	close(w.stop)
	<-w.done
	return w.peak, max(w.peak-w.baseline, 0)
}

// currentMemory reads the memory the worker container holds, in bytes, and zero when the
// host does not expose it.
func currentMemory(ctx context.Context) int64 {
	for _, path := range []string{"/sys/fs/cgroup/memory.current", "/sys/fs/cgroup/memory/memory.usage_in_bytes"} {
		//nolint:gosec // the container name comes from the bench environment
		out, err := exec.CommandContext(ctx, "docker", "exec", workerContainer(), "cat", path).Output()
		if err != nil {
			continue
		}
		if current, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64); err == nil {
			return current
		}
	}
	return 0
}
