package main

import (
	"testing"
	"time"
)

func TestCancelOneJobLeavesTheOtherRunning(t *testing.T) {
	t.Parallel()

	var a, b cancellable
	a.begin()
	b.begin()
	defer a.end()
	defer b.end()

	if a.cancelled() || b.cancelled() {
		t.Fatal("fresh jobs should not be cancelled")
	}

	a.request()

	if !a.cancelled() {
		t.Fatal("requested job should be cancelled")
	}
	if b.cancelled() {
		t.Fatal("sibling job should still be running")
	}
}

func TestSleepOrCancelCtxUsesProvidedContext(t *testing.T) {
	t.Parallel()

	var run cancellable
	run.begin()
	defer run.end()

	run.request()
	started := time.Now()
	if sleepOrCancelCtx(run.context(), time.Second) {
		t.Fatal("cancelled context should not wait the full duration")
	}
	if time.Since(started) > 200*time.Millisecond {
		t.Fatal("cancelled sleep should return promptly")
	}

	if !sleepOrCancelCtx(t.Context(), 10*time.Millisecond) {
		t.Fatal("live context should wait the duration")
	}
}
