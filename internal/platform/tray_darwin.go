//go:build darwin && cgo

package platform

/*
#include <stddef.h>
int cv_tray_install(const void *imageBytes, size_t imageLength);
int cv_tray_consume(void);
void cv_tray_remove(void);
*/
import "C"

import (
	_ "embed"
	"errors"
	"sync"
	"time"
	"unsafe"
)

//go:embed icons/tray-macos.png
var macTrayImage []byte

var macTrayLifecycle struct {
	sync.Mutex
	active bool
	tray   *Tray
}

func enableMacTray() {
	macTrayLifecycle.Lock()
	defer macTrayLifecycle.Unlock()
	macTrayLifecycle.active = true
}

func disableMacTray() {
	macTrayLifecycle.Lock()
	macTrayLifecycle.active = false
	tray := macTrayLifecycle.tray
	macTrayLifecycle.Unlock()
	if tray != nil {
		_ = tray.Close()
	}
}

// StartTray must run from RunMain's worker, never from the Cocoa main thread.
// Native menu targets only update atomics; a Go worker forwards those actions
// to the same event channel used by Windows and the application lifecycle.
func StartTray() (*Tray, error) {
	macTrayLifecycle.Lock()
	defer macTrayLifecycle.Unlock()
	if !macTrayLifecycle.active {
		return nil, errors.New("macOS menu bar requires the active RunMain event loop")
	}
	if macTrayLifecycle.tray != nil {
		return nil, errors.New("macOS menu bar is already initialized for this run")
	}
	if C.cv_tray_install(unsafe.Pointer(&macTrayImage[0]), C.size_t(len(macTrayImage))) != 0 {
		return nil, errors.New("could not create the macOS menu bar entry")
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	tray := startTrayWorker(ticker.C, func() TrayEvent {
		switch C.cv_tray_consume() {
		case 1:
			return TrayOpenUI
		case 2:
			return TrayQuit
		default:
			return 0
		}
	}, func() error {
		ticker.Stop()
		C.cv_tray_remove()
		return nil
	})
	macTrayLifecycle.tray = tray
	return tray, nil
}
