//go:build darwin && cgo

package platform

/*
void cv_reopen_install(void);
void cv_reopen_uninstall(void);
void cv_reopen_disable(void);
int cv_reopen_consume(void);
*/
import "C"

import (
	"sync"
	"time"
)

var reopenLifecycle struct {
	sync.Mutex
	active      bool
	subscribed  bool
	unsubscribe func()
}

func installReopen() {
	C.cv_reopen_install()
	reopenLifecycle.Lock()
	reopenLifecycle.active = true
	reopenLifecycle.Unlock()
}

func uninstallReopen() {
	reopenLifecycle.Lock()
	reopenLifecycle.active = false
	unsubscribe := reopenLifecycle.unsubscribe
	reopenLifecycle.Unlock()
	if unsubscribe != nil {
		unsubscribe()
	}
	C.cv_reopen_uninstall()
}

// OnReopen subscribes once per RunMain lifetime, after the HTTP server is ready.
// Call unsubscribe before tearing down that server. It joins any running callback;
// do not call unsubscribe from the callback itself or from the Cocoa main thread.
// Calls outside RunMain, duplicate subscriptions, and nil callbacks are no-ops.
func OnReopen(callback func()) func() {
	reopenLifecycle.Lock()
	defer reopenLifecycle.Unlock()
	if !reopenLifecycle.active || reopenLifecycle.subscribed || callback == nil {
		return func() {}
	}
	reopenLifecycle.subscribed = true
	ticker := time.NewTicker(100 * time.Millisecond)
	stopWorker := startReopenWorker(ticker.C, func() bool { return C.cv_reopen_consume() != 0 }, callback)
	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			C.cv_reopen_disable()
			ticker.Stop()
			stopWorker()
		})
	}
	reopenLifecycle.unsubscribe = unsubscribe
	return unsubscribe
}
