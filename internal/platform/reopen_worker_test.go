package platform

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestReopenWorkerCoalescesPendingAndStops(t *testing.T) {
	ticks := make(chan time.Time)
	var pending atomic.Bool
	var callbacks atomic.Int32
	consumed := make(chan struct{}, 4)
	called := make(chan struct{}, 4)
	pending.Store(true) // An event arrived before the HTTP-ready subscription.
	unsubscribe := startReopenWorker(ticks, func() bool {
		result := pending.Swap(false)
		consumed <- struct{}{}
		return result
	}, func() { callbacks.Add(1); called <- struct{}{} })
	defer unsubscribe()
	ticks <- time.Now()
	<-consumed
	<-called
	ticks <- time.Now()
	<-consumed // Empty polls must not open another browser.
	pending.Store(true)
	pending.Store(true)
	ticks <- time.Now()
	<-consumed
	<-called
	unsubscribe()
	unsubscribe() // Idempotent and no callback after it returns.
	pending.Store(true)
	if callbacks.Load() != 2 {
		t.Fatalf("callbacks = %d, want 2", callbacks.Load())
	}
}

func TestReopenUnsubscribeJoinsRunningCallback(t *testing.T) {
	ticks := make(chan time.Time, 2)
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	var callbacks atomic.Int32
	unsubscribe := startReopenWorker(ticks, func() bool { return true }, func() {
		callbacks.Add(1)
		close(started)
		<-release
		close(finished)
	})
	ticks <- time.Now()
	<-started
	joined := make(chan struct{})
	go func() { unsubscribe(); close(joined) }()
	select {
	case <-joined:
		t.Fatal("unsubscribe returned during callback")
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	select {
	case <-joined:
	case <-time.After(time.Second):
		t.Fatal("unsubscribe did not join callback")
	}
	<-finished
	ticks <- time.Now()
	if callbacks.Load() != 1 {
		t.Fatal("callback ran after shutdown")
	}
}
