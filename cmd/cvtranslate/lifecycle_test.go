package main

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"cvtranslate/internal/platform"
)

func TestTrayOpenFailureStillAllowsRetryAndGracefulQuit(test *testing.T) {
	applicationContext, stop := context.WithCancel(context.Background())
	defer stop()
	events := make(chan platform.TrayEvent, 1)
	events <- platform.TrayOpenUI
	var openRequests atomic.Int32
	err := waitForShutdown(applicationContext, stop, nil, events, false, func() error {
		if openRequests.Add(1) == 1 {
			events <- platform.TrayOpenUI
			return errors.New("browser unavailable")
		}
		events <- platform.TrayQuit
		return nil
	})
	if err != nil || openRequests.Load() != 2 || !errors.Is(applicationContext.Err(), context.Canceled) {
		test.Fatalf("quit did not cancel the application after browser retry: opens=%d context=%v error=%v", openRequests.Load(), applicationContext.Err(), err)
	}
}

func TestTrayQuitDoesNotWaitForBlockedBrowserLaunch(test *testing.T) {
	for _, openOnStartup := range []bool{false, true} {
		applicationContext, stop := context.WithCancel(context.Background())
		defer stop()
		events := make(chan platform.TrayEvent, 3)
		if !openOnStartup {
			events <- platform.TrayOpenUI
		}
		started := make(chan struct{})
		release := make(chan struct{})
		finished := make(chan struct{})
		result := make(chan error, 1)
		var openRequests atomic.Int32
		go func() {
			result <- waitForShutdown(applicationContext, stop, nil, events, openOnStartup, func() error {
				if openRequests.Add(1) == 1 {
					close(started)
					<-release
					close(finished)
				}
				return nil
			})
		}()
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			stop()
			close(release)
			test.Fatal("browser launch did not start")
		}
		events <- platform.TrayOpenUI
		events <- platform.TrayOpenUI
		events <- platform.TrayQuit
		select {
		case err := <-result:
			if err != nil || applicationContext.Err() == nil || openRequests.Load() != 1 {
				test.Errorf("blocked browser affected quit/coalescing: requests=%d context=%v error=%v", openRequests.Load(), applicationContext.Err(), err)
			}
		case <-time.After(2 * time.Second):
			stop()
			test.Error("quit waited for the blocked browser")
		}
		close(release)
		<-finished
	}
}

func TestLostTrayCancelsApplicationInsteadOfLeavingBackgroundProcess(test *testing.T) {
	applicationContext, stop := context.WithCancel(context.Background())
	defer stop()
	events := make(chan platform.TrayEvent)
	close(events)
	err := waitForShutdown(applicationContext, stop, nil, events, false, func() error {
		test.Error("unexpected browser request")
		return nil
	})
	if err == nil || !errors.Is(applicationContext.Err(), context.Canceled) {
		test.Fatalf("lost tray did not trigger failed shutdown: context=%v error=%v", applicationContext.Err(), err)
	}
}

func TestCancelledApplicationDiscardsQueuedTrayOpen(test *testing.T) {
	applicationContext, stop := context.WithCancel(context.Background())
	stop()
	events := make(chan platform.TrayEvent, 1)
	events <- platform.TrayOpenUI
	err := waitForShutdown(applicationContext, stop, nil, events, false, func() error {
		test.Error("queued browser request survived application cancellation")
		return nil
	})
	if err != nil {
		test.Fatal(err)
	}
}

func TestHeadlessServerTerminationCancelsApplication(test *testing.T) {
	for _, scenario := range []struct {
		name       string
		serveError error
		wantError  bool
	}{
		{name: "normal close", serveError: http.ErrServerClosed},
		{name: "listener failure", serveError: errors.New("listener failed"), wantError: true},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			applicationContext, stop := context.WithCancel(context.Background())
			defer stop()
			serverErrors := make(chan error, 1)
			serverErrors <- scenario.serveError
			err := waitForShutdown(applicationContext, stop, serverErrors, nil, false, func() error {
				test.Error("headless shutdown requested a browser")
				return nil
			})
			if (err != nil) != scenario.wantError || applicationContext.Err() == nil {
				test.Fatalf("unexpected server shutdown: context=%v error=%v", applicationContext.Err(), err)
			}
		})
	}
}
