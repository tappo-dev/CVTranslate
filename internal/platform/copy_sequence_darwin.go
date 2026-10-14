//go:build darwin && cgo

package platform

/*
#include "native_darwin.h"
*/
import "C"

import (
	"context"
	"errors"
	"runtime"
	"runtime/cgo"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

type macCopyFailure struct{ err error }

type macCopyReady struct {
	native unsafe.Pointer
	err    error
}

type macCopySequence struct {
	mutex      sync.Mutex
	native     unsafe.Pointer
	handle     cgo.Handle
	generation atomic.Uint64
	lastError  atomic.Pointer[macCopyFailure]
	callback   func(copyTrigger, uint64)
	ready      chan macCopyReady
	done       chan struct{}
	// Only the dedicated event-tap thread accesses the observer.
	observer macCopyObserver
}

func registerNativeCopySequence(callback func(copyTrigger, uint64)) (copySequenceBinding, error) {
	listener := &macCopySequence{callback: callback, ready: make(chan macCopyReady, 1), done: make(chan struct{})}
	listener.handle = cgo.NewHandle(listener)
	go listener.run()
	ready := <-listener.ready
	if ready.err != nil {
		<-listener.done
		listener.handle.Delete()
		return nil, ready.err
	}
	listener.native = ready.native
	return listener, nil
}

func (listener *macCopySequence) run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(listener.done)
	if err := macError(C.cv_copy_run(C.uintptr_t(listener.handle))); err != nil {
		listener.lastError.Store(&macCopyFailure{err: err})
	}
	listener.generation.Store(0)
}

//export cv_go_copy_ready
func cv_go_copy_ready(handle C.uintptr_t, native unsafe.Pointer, result C.int) {
	listener := cgo.Handle(handle).Value().(*macCopySequence)
	listener.ready <- macCopyReady{native: native, err: macError(result)}
}

func (listener *macCopySequence) activate(generation uint64) { listener.generation.Store(generation) }

func (listener *macCopySequence) failure() error {
	if failure := listener.lastError.Load(); failure != nil {
		return failure.err
	}
	return nil
}

func (listener *macCopySequence) close() error {
	listener.mutex.Lock()
	defer listener.mutex.Unlock()
	if listener.native == nil {
		return nil
	}
	listener.generation.Store(0)
	// The run loop retains its native state until callbacks have fully stopped.
	C.cv_copy_stop(listener.native)
	<-listener.done
	C.cv_copy_dispose(listener.native)
	listener.native = nil
	listener.handle.Delete()
	return nil
}

func macCopySnapshot() (uintptr, uint64) {
	var foreground C.uintptr_t
	var version C.uint64_t
	C.cv_copy_snapshot(&foreground, &version)
	return uintptr(foreground), uint64(version)
}

//export cv_go_copy_event
func cv_go_copy_event(handle C.uintptr_t, kind C.int, key C.int, commandOnly C.int, autoRepeat C.int, injected C.int) {
	listener := cgo.Handle(handle).Value().(*macCopySequence)
	if int(kind) == macCopyListenerFailed {
		listener.lastError.Store(&macCopyFailure{err: errors.New("macOS repeated-copy listener stopped; check Accessibility and Input Monitoring permissions and restart")})
		listener.generation.Store(0)
	}
	generation := listener.generation.Load()
	event := macCopyEvent{kind: int(kind), key: int(key), commandOnly: commandOnly != 0, autoRepeat: autoRepeat != 0, injected: injected != 0}
	trigger := listener.observer.observe(event, time.Now(), generation, macCopySnapshot)
	if trigger.repetitions != 0 && listener.failure() == nil {
		// Never acquire the Desktop lock, read text or run translation on the tap.
		go listener.callback(trigger, generation)
	}
}

func (desktop *macDesktop) captureCopy(trigger copyTrigger) (ClipboardCapture, error) {
	if C.cv_trusted() == 0 || C.cv_copy_permitted() == 0 {
		return ClipboardCapture{}, describeNativeError(errors.New("macOS shortcut permissions unavailable; capture refused"))
	}
	return captureClipboardCopyAtSnapshot(trigger, func() uintptr {
		foreground, _ := macCopySnapshot()
		return foreground
	}, desktop.capture)
}

func (*macDesktop) validateClipboard(version uint64) error {
	return macError(C.cv_validate_clipboard(C.uint64_t(version)))
}

func (desktop *macDesktop) dispatchDirect(ctx context.Context, version uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if desktop.state == nil {
		return errors.New("native desktop is unavailable")
	}
	handle := cgo.NewHandle(ctx)
	defer handle.Delete()
	result := C.cv_dispatch_direct(C.uint64_t(version), C.uintptr_t(handle))
	if result == 8 {
		return ctx.Err()
	}
	return macError(result)
}

//export cv_go_paste_cancelled
func cv_go_paste_cancelled(handle C.uintptr_t) C.int {
	if cgo.Handle(handle).Value().(context.Context).Err() != nil {
		return 1
	}
	return 0
}
