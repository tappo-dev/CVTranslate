package main

import (
	"context"
	"errors"
	"log"
	"net/http"

	"cvtranslate/internal/platform"
)

// Native callbacks only enqueue actions. Application work and cancellation
// remain on the composition side of the platform boundary.
func waitForShutdown(applicationContext context.Context, stop context.CancelFunc, serverErrors <-chan error, trayEvents <-chan platform.TrayEvent, openOnStartup bool, openUI func() error) error {
	defer stop()
	var browserResult <-chan error
	openPending := false
	startBrowserOpen := func() {
		result := make(chan error, 1)
		browserResult = result
		// Shell activation can block. It owns no application resources and must
		// never prevent quit, including during the initial browser launch.
		go func() {
			if applicationContext.Err() != nil {
				result <- applicationContext.Err()
				return
			}
			result <- openUI()
		}()
	}
	if openOnStartup && applicationContext.Err() == nil {
		startBrowserOpen()
	}
	for {
		select {
		case <-applicationContext.Done():
			return nil
		case browserError := <-browserResult:
			browserResult = nil
			if applicationContext.Err() != nil {
				return nil
			}
			if browserError != nil {
				log.Print("Could not open the browser; use the tray menu or reopen the application to retry.")
			}
			if openPending {
				openPending = false
				startBrowserOpen()
			}
		case serveError := <-serverErrors:
			if serveError != nil && !errors.Is(serveError, http.ErrServerClosed) {
				return errors.New("local HTTP service stopped unexpectedly")
			}
			return nil
		case event, available := <-trayEvents:
			if !available {
				return errors.New("system tray stopped unexpectedly; shutting down")
			}
			if applicationContext.Err() != nil {
				return nil
			}
			switch event {
			case platform.TrayOpenUI:
				if browserResult == nil {
					startBrowserOpen()
				} else {
					openPending = true
				}
			case platform.TrayQuit:
				return nil
			}
		}
	}
}
