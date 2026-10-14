//go:build windows && (amd64 || arm64)

package platform

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	keyboardHookLowLevel = 13
	messageKeyDown       = 0x0100
	messageKeyUp         = 0x0101
	messageSystemDown    = 0x0104
	messageSystemUp      = 0x0105
	messageCopyTimer     = 0x0113
	messageCloseCopy     = 0x8000 + 0x52
	virtualKeyCopy       = 0x43
	keyboardInjected     = 0x10 | 0x02
)

var (
	setCopyHook      = user32.NewProc("SetWindowsHookExW")
	unhookCopyHook   = user32.NewProc("UnhookWindowsHookEx")
	callNextCopyHook = user32.NewProc("CallNextHookEx")
	getCopyMessage   = user32.NewProc("GetMessageW")
	peekCopyMessage  = user32.NewProc("PeekMessageW")
	postCopyMessage  = user32.NewProc("PostThreadMessageW")
	setCopyTimer     = user32.NewProc("SetTimer")
	killCopyTimer    = user32.NewProc("KillTimer")
	getCopyKeyState  = user32.NewProc("GetAsyncKeyState")
	copyForeground   = user32.NewProc("GetForegroundWindow")
	copyHookThreads  sync.Map
	copyHookCallback = windows.NewCallback(forwardCopyKeyboardEvent)
)

type copyKeyboardEvent struct {
	VirtualKey uint32
	ScanCode   uint32
	Flags      uint32
	Time       uint32
	ExtraInfo  uintptr
}

type copyThreadMessage struct {
	Window    uintptr
	Message   uint32
	Parameter uintptr
	Data      uintptr
	Time      uint32
	PositionX int32
	PositionY int32
	Private   uint32
}

type copyListenerFailure struct{ err error }

type windowsCopySequence struct {
	mutex         sync.Mutex
	closed        bool
	threadID      uint32
	callback      func(copyTrigger, uint64)
	generation    atomic.Uint64
	terminalError atomic.Pointer[copyListenerFailure]
	closeResults  chan error
	done          chan struct{}
	// These fields are accessed only on the hook's message thread.
	sequence         copySequence
	activeGeneration uint64
}

func registerNativeCopySequence(callback func(copyTrigger, uint64)) (copySequenceBinding, error) {
	listener := &windowsCopySequence{
		callback: callback, closeResults: make(chan error, 1), done: make(chan struct{}),
	}
	ready := make(chan error, 1)
	go listener.run(ready)
	if err := <-ready; err != nil {
		<-listener.done
		return nil, err
	}
	return listener, nil
}

func (listener *windowsCopySequence) activate(generation uint64) {
	listener.generation.Store(generation)
}

func (listener *windowsCopySequence) failure() error {
	if failure := listener.terminalError.Load(); failure != nil {
		return failure.err
	}
	return nil
}

func (listener *windowsCopySequence) fail(err error) {
	listener.generation.Store(0)
	listener.terminalError.Store(&copyListenerFailure{err: err})
}

func (listener *windowsCopySequence) synchronizeGeneration() bool {
	generation := listener.generation.Load()
	if generation != listener.activeGeneration {
		listener.sequence.cancel()
		listener.activeGeneration = generation
	}
	return generation != 0
}

func (listener *windowsCopySequence) run(ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(listener.done)
	listener.threadID = windows.GetCurrentThreadId()
	var message copyThreadMessage
	// Create the queue before exposing the thread ID to the closing caller.
	peekCopyMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0, 0)
	timer, _, timerError := setCopyTimer.Call(0, 0, 25, 0)
	if timer == 0 {
		ready <- fmt.Errorf("create repeated-copy timer: %w", timerError)
		return
	}
	defer killCopyTimer.Call(0, timer)
	module, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
	listener.sequence.copyHeld = isCopyKeyHeld(virtualKeyCopy)
	copyHookThreads.Store(listener.threadID, listener)
	defer copyHookThreads.Delete(listener.threadID)
	hook, _, hookError := setCopyHook.Call(keyboardHookLowLevel, copyHookCallback, module, 0)
	if hook == 0 {
		ready <- fmt.Errorf("install repeated-copy listener: %w", hookError)
		return
	}
	defer func() {
		listener.generation.Store(0)
		if hook != 0 {
			if removed, _, removeError := unhookCopyHook.Call(hook); removed == 0 {
				listener.fail(fmt.Errorf("remove repeated-copy listener: %w", removeError))
			}
		}
	}()
	ready <- nil
	for {
		result, _, messageError := getCopyMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(result) == -1 {
			listener.fail(fmt.Errorf("receive repeated-copy events: %w", messageError))
			return
		}
		if result == 0 {
			listener.fail(errors.New("repeated-copy message thread stopped unexpectedly"))
			return
		}
		switch message.Message {
		case messageCloseCopy:
			removed, _, removeError := unhookCopyHook.Call(hook)
			if removed == 0 {
				listener.closeResults <- fmt.Errorf("remove repeated-copy listener: %w", removeError)
				continue
			}
			hook = 0
			listener.generation.Store(0)
			listener.closeResults <- nil
			return
		case messageCopyTimer:
			if message.Parameter == timer {
				listener.completeSequence()
			}
		}
	}
}

func isCopyKeyHeld(virtualKey uintptr) bool {
	state, _, _ := getCopyKeyState.Call(virtualKey)
	return state&0x8000 != 0
}

func isControlOnlyHeld() bool {
	return isCopyKeyHeld(0x11) && !isCopyKeyHeld(0x10) && !isCopyKeyHeld(0x12) && !isCopyKeyHeld(0x5B) && !isCopyKeyHeld(0x5C)
}

func forwardCopyKeyboardEvent(code, message uintptr, data *copyKeyboardEvent) uintptr {
	if code == 0 && data != nil {
		if stored, exists := copyHookThreads.Load(windows.GetCurrentThreadId()); exists {
			listener := stored.(*windowsCopySequence)
			listener.observeKey(message, *data)
		}
	}
	// Never consume or synthesize Ctrl+C: the foreground application must copy.
	result, _, _ := callNextCopyHook.Call(0, code, message, uintptr(unsafe.Pointer(data)))
	return result
}

func (listener *windowsCopySequence) observeKey(message uintptr, event copyKeyboardEvent) {
	pressed := message == messageKeyDown || message == messageSystemDown
	released := message == messageKeyUp || message == messageSystemUp
	if !pressed && !released {
		return
	}
	if event.Flags&keyboardInjected != 0 {
		listener.sequence.cancel()
		return
	}
	active := listener.synchronizeGeneration()
	if event.VirtualKey == virtualKeyCopy && released {
		listener.sequence.release()
		return
	}
	if !pressed {
		return
	}
	if !active {
		if event.VirtualKey == virtualKeyCopy {
			listener.sequence.copyHeld = true
		}
		return
	}
	if event.VirtualKey == 0x11 || event.VirtualKey == 0xA2 || event.VirtualKey == 0xA3 {
		return // Allow releasing/repressing Ctrl between individual copy presses.
	}
	if event.VirtualKey != virtualKeyCopy || !isControlOnlyHeld() {
		listener.sequence.cancel()
		if event.VirtualKey == virtualKeyCopy {
			listener.sequence.copyHeld = true
		}
		return
	}
	foreground, _, _ := copyForeground.Call()
	listener.dispatchTrigger(listener.sequence.press(time.Now(), foreground, clipboardSequence()))
}

func (listener *windowsCopySequence) completeSequence() {
	if !listener.synchronizeGeneration() || listener.sequence.pressCount == 0 {
		return
	}
	foreground, _, _ := copyForeground.Call()
	trigger := listener.sequence.complete(time.Now(), foreground, clipboardSequence())
	listener.dispatchTrigger(trigger)
}

func (listener *windowsCopySequence) dispatchTrigger(trigger copyTrigger) {
	if trigger.repetitions == 0 {
		return
	}
	// No network, clipboard-content reads or application callback runs on the hook
	// thread. The desktop rejects queued callbacks from an older configuration.
	generation := listener.activeGeneration
	go listener.callback(trigger, generation)
}

func (native *windowsDesktop) captureCopy(trigger copyTrigger) (ClipboardCapture, error) {
	return captureClipboardCopyAtSnapshot(trigger, func() uintptr {
		foreground, _, _ := copyForeground.Call()
		return foreground
	}, native.capture)
}

func (listener *windowsCopySequence) close() error {
	listener.mutex.Lock()
	defer listener.mutex.Unlock()
	if listener.closed {
		return nil
	}
	select {
	case <-listener.done:
		listener.closed = true
		return listener.failure()
	default:
	}
	posted, _, postError := postCopyMessage.Call(uintptr(listener.threadID), messageCloseCopy, 0, 0)
	if posted == 0 {
		return fmt.Errorf("stop repeated-copy listener: %w", postError)
	}
	select {
	case err := <-listener.closeResults:
		if err != nil {
			return err
		}
		<-listener.done
		listener.closed = true
		return listener.failure()
	case <-listener.done:
		listener.closed = true
		return listener.failure()
	}
}
