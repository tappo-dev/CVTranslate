package platform

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTrayWorkerForwardsEarlyOpenThenQuitAndSuppressesLaterActions(test *testing.T) {
	ticks := make(chan time.Time)
	var pending atomic.Uint32
	pending.Store(uint32(TrayOpenUI))
	consumed := make(chan struct{})
	tray := startTrayWorker(ticks, func() TrayEvent {
		event := TrayEvent(pending.Swap(0))
		consumed <- struct{}{}
		return event
	}, func() error { return nil })
	defer tray.Close()
	poll := func() { ticks <- time.Now(); <-consumed }
	poll()
	if event := <-tray.Events; event != TrayOpenUI {
		test.Fatalf("early menu action was lost: %v", event)
	}
	pending.Store(uint32(TrayQuit))
	poll()
	if event := <-tray.Events; event != TrayQuit {
		test.Fatalf("quit was not forwarded: %v", event)
	}
	pending.Store(uint32(TrayOpenUI))
	poll()
	poll() // The next consume starts only after the previous event was handled.
	select {
	case event := <-tray.Events:
		test.Fatalf("menu event survived quit: %v", event)
	default:
	}
}

func TestTrayWorkerCloseJoinsPollBeforeRemovingNativeResources(test *testing.T) {
	ticks := make(chan time.Time, 1)
	started := make(chan struct{})
	release := make(chan struct{})
	pollFinished := make(chan struct{})
	var removals atomic.Int32
	removeError := errors.New("native removal failed")
	tray := startTrayWorker(ticks, func() TrayEvent {
		close(started)
		<-release
		close(pollFinished)
		return TrayOpenUI
	}, func() error {
		select {
		case <-pollFinished:
		default:
			test.Error("native resources removed while consume was active")
		}
		removals.Add(1)
		return removeError
	})
	ticks <- time.Now()
	<-started
	var callers sync.WaitGroup
	for range 4 {
		callers.Go(func() {
			if err := tray.Close(); !errors.Is(err, removeError) {
				test.Errorf("concurrent close lost native error: %v", err)
			}
		})
	}
	close(release)
	callers.Wait()
	if removals.Load() != 1 {
		test.Fatalf("native resources removed %d times", removals.Load())
	}
	if err := tray.Close(); !errors.Is(err, removeError) {
		test.Fatalf("repeated close lost native error: %v", err)
	}
	// A queued action may predate Close, but no producer survives it.
	for range tray.Events {
	}
}

func TestTrayWorkerLostEventSourceClosesEventsAndStillCleansUp(test *testing.T) {
	ticks := make(chan time.Time)
	var removed bool
	tray := startTrayWorker(ticks, func() TrayEvent {
		test.Error("closed event source was polled")
		return 0
	}, func() error { removed = true; return nil })
	close(ticks)
	select {
	case _, available := <-tray.Events:
		if available {
			test.Fatal("lost event source delivered an action")
		}
	case <-time.After(time.Second):
		test.Fatal("lost event source left the application waiting")
	}
	if err := tray.Close(); err != nil || !removed {
		test.Fatalf("lost event source skipped native cleanup: removed=%v error=%v", removed, err)
	}
}
