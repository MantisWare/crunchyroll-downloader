package main

import (
	"context"
	"errors"
	"sync"
	"time"
)

// errCancelled marks work that stopped because the user asked it to, rather
// than because something failed.
var errCancelled = errors.New("stopped by user")

// cancellable is one stoppable job. GUI sessions each own one so Stop on a
// tab cannot cancel a sibling download or conversion.
type cancellable struct {
	mu   sync.RWMutex
	ctx  context.Context
	stop context.CancelFunc
}

func (c *cancellable) begin() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.stop != nil {
		c.stop()
	}
	c.ctx, c.stop = context.WithCancel(context.Background())
}

func (c *cancellable) end() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.stop != nil {
		c.stop()
		c.stop = nil
	}
	c.ctx = context.Background()
}

func (c *cancellable) request() {
	c.mu.RLock()
	stop := c.stop
	c.mu.RUnlock()

	if stop != nil {
		stop()
	}
}

func (c *cancellable) context() context.Context {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.ctx == nil {
		return context.Background()
	}
	return c.ctx
}

func (c *cancellable) cancelled() bool {
	return c.context().Err() != nil
}

// cliRun is the single job the command-line entry point can stop. The GUI
// never uses it; each tab holds its own cancellable.
var cliRun cancellable

// beginCancellable installs a fresh context for one CLI download run.
func beginCancellable() {
	cliRun.begin()
}

// endCancellable tears down the CLI run context and releases its resources.
func endCancellable() {
	cliRun.end()
}

// requestCancel asks the in-flight CLI download run to stop.
func requestCancel() {
	cliRun.request()
}

// downloadContext returns the context for the current CLI run. Outside a run
// this is a background context, so the CLI is never cancelled.
func downloadContext() context.Context {
	return cliRun.context()
}

func isCancelled() bool {
	return cliRun.cancelled()
}

// sleepOrCancel waits for the given duration, returning false if the CLI run
// was cancelled before it elapsed.
func sleepOrCancel(d time.Duration) bool {
	return sleepOrCancelCtx(downloadContext(), d)
}

// sleepOrCancelCtx waits for the given duration, returning false if ctx is
// cancelled before it elapsed.
func sleepOrCancelCtx(ctx context.Context, d time.Duration) bool {
	if ctx == nil {
		ctx = context.Background()
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
