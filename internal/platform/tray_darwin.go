//go:build darwin && cgo

package platform

/*
#include <stddef.h>
int cv_tray_install(const void *imageBytes, size_t imageLength, int launchItem);
void cv_tray_set_launch_state(int state);
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
func StartTray(options TrayOptions) (*Tray, error) {
	macTrayLifecycle.Lock()
	defer macTrayLifecycle.Unlock()
	if !macTrayLifecycle.active {
		return nil, errors.New("macOS menu bar requires the active RunMain event loop")
	}
	if macTrayLifecycle.tray != nil {
		return nil, errors.New("macOS menu bar is already initialized for this run")
	}
	launch := options.LaunchAtLogin
	launchItem := C.int(0)
	if launch != nil {
		launchItem = 1
	}
	if C.cv_tray_install(unsafe.Pointer(&macTrayImage[0]), C.size_t(len(macTrayImage)), launchItem) != 0 {
		return nil, errors.New("could not create the macOS menu bar entry")
	}
	publishLaunchState := func(enabled bool, err error) error {
		state := C.int(0)
		if err != nil {
			state = 2
		} else if enabled {
			state = 1
		}
		C.cv_tray_set_launch_state(state)
		return err
	}
	if launch != nil {
		options.report(publishLaunchState(launch.Enabled()))
	}
	polls := 0
	ticker := time.NewTicker(50 * time.Millisecond)
	tray := startTrayWorker(ticker.C, func() TrayEvent {
		action := C.cv_tray_consume()
		if launch != nil {
			// Login item file I/O stays on this worker; Cocoa reads only the atomic.
			if action == 3 {
				options.report(publishLaunchState(launch.toggle()))
			} else if polls++; polls%20 == 0 {
				// The WebUI may change the registration while the menu is closed;
				// a persistent read failure was already reported and grays the item.
				_ = publishLaunchState(launch.Enabled())
			}
		}
		switch action {
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
