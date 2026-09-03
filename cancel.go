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

var (
	cancelMu   sync.RWMutex
	cancelCtx  context.Context = context.Background()
	cancelStop context.CancelFunc
)

// beginCancellable installs a fresh context for one download run.
func beginCancellable() {
	cancelMu.Lock()
	defer cancelMu.Unlock()

	if cancelStop != nil {
		cancelStop()
	}
	cancelCtx, cancelStop = context.WithCancel(context.Background())
}

// endCancellable tears down the run context and releases its resources.
func endCancellable() {
	cancelMu.Lock()
	defer cancelMu.Unlock()

	if cancelStop != nil {
		cancelStop()
		cancelStop = nil
	}
	cancelCtx = context.Background()
}

// requestCancel asks the in-flight download run to stop.
func requestCancel() {
	cancelMu.RLock()
	stop := cancelStop
	cancelMu.RUnlock()

	if stop != nil {
		stop()
	}
}

// downloadContext returns the context for the current run. Outside a run this
// is a background context, so the CLI is never cancelled.
func downloadContext() context.Context {
	cancelMu.RLock()
	defer cancelMu.RUnlock()
	return cancelCtx
}

func isCancelled() bool {
	return downloadContext().Err() != nil
}

// sleepOrCancel waits for the given duration, returning false if the run was
// cancelled before it elapsed.
func sleepOrCancel(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-timer.C:
		return true
	case <-downloadContext().Done():
		return false
	}
}
