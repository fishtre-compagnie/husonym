// Package workerctl drives the worker container the bench runs its jobs on: the perf mode
// restarts it before each measured run, a robustness case kills it in the middle of a page.
package workerctl

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Container is the name of the worker container.
func Container() string {
	if name := os.Getenv("BENCH_WORKER_CONTAINER"); name != "" {
		return name
	}
	return "husonym-worker"
}

// Restart restarts the worker and waits until it is listening again.
func Restart(ctx context.Context) error {
	since := time.Now()
	if err := docker(ctx, "restart", Container()); err != nil {
		return err
	}
	return waitStarted(ctx, since)
}

// Kill kills the worker the way a machine lost or an out-of-memory kill ends it — no
// signal it could handle, nothing it could finish — then starts it again and waits until
// it is listening. What it was doing is left for Temporal to retry.
func Kill(ctx context.Context) error {
	since := time.Now()
	if err := docker(ctx, "kill", "--signal", "KILL", Container()); err != nil {
		return err
	}
	if err := docker(ctx, "start", Container()); err != nil {
		return err
	}
	return waitStarted(ctx, since)
}

func docker(ctx context.Context, args ...string) error {
	if out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("workerctl: docker %s: %w: %s", strings.Join(args, " "), err, out)
	}
	return nil
}

// waitStarted waits for the worker to announce itself after since.
func waitStarted(ctx context.Context, since time.Time) error {
	name := Container()
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		//nolint:gosec // the container name comes from the bench environment
		out, err := exec.CommandContext(ctx, "docker", "logs", "--since", since.UTC().Format(time.RFC3339), name).
			CombinedOutput()
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
	return fmt.Errorf("workerctl: %s did not come back within five minutes", name)
}
