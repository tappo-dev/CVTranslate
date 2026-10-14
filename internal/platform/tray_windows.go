//go:build windows && (amd64 || arm64)

package platform

import (
	_ "embed"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	trayCallbackMessage = 0x8000 + 0x61
	trayCloseMessage    = 0x8000 + 0x62
	trayContextMenu     = 0x007B
	traySelect          = 0x0400
	trayKeySelect       = 0x0401
	trayOpenCommand     = 1
	trayQuitCommand     = 2
	trayIconAdd         = 0
	trayIconDelete      = 2
	trayIconSetVersion  = 4
)

var (
	trayThreads        sync.Map
	trayWindowCallback = windows.NewCallback(dispatchTrayWindowMessage)
	notifyTrayIcon     = shell32.NewProc("Shell_NotifyIconW")
)

//go:embed icons/tray-windows.png
var windowsTrayImage []byte

type trayWindowClass struct {
	Size            uint32
	Style           uint32
	WindowProcedure uintptr
	ClassExtra      int32
	WindowExtra     int32
	Instance        uintptr
	Icon            uintptr
	Cursor          uintptr
	Background      uintptr
	MenuName        *uint16
	ClassName       *uint16
	SmallIcon       uintptr
}

type trayIconData struct {
	Size             uint32
	Window           uintptr
	Identifier       uint32
	Flags            uint32
	CallbackMessage  uint32
	Icon             uintptr
	Tooltip          [128]uint16
	State            uint32
	StateMask        uint32
	Information      [256]uint16
	TimeoutOrVersion uint32
	InformationTitle [64]uint16
	InformationFlags uint32
	GUID             windows.GUID
	BalloonIcon      uintptr
}

type trayWindowMessage struct {
	Window    uintptr
	Message   uint32
	Parameter uintptr
	Data      uintptr
	Time      uint32
	PositionX int32
	PositionY int32
	Private   uint32
}

type windowsTray struct {
	mutex      sync.Mutex
	window     uintptr
	done       chan struct{}
	queue      *trayEventQueue
	notifyIcon func(uintptr, *trayIconData) error
	// These fields belong to the locked message thread until done closes.
	menu           uintptr
	icon           trayIconData
	iconAdded      bool
	taskbarCreated uint32
	stopping       bool
	runError       error
}

// StartTray creates a hidden top-level window on a dedicated OS thread. Unlike
// a message-only window, it receives Explorer's TaskbarCreated broadcast.
func StartTray() (*Tray, error) {
	native := &windowsTray{done: make(chan struct{}), queue: newTrayEventQueue(), notifyIcon: updateWindowsTrayIcon}
	ready := make(chan error, 1)
	go native.run(ready)
	if err := <-ready; err != nil {
		<-native.done
		return nil, errors.Join(err, native.runError)
	}
	return &Tray{Events: native.queue.events, close: native.close}, nil
}

func (native *windowsTray) run(ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(native.done)
	defer close(native.queue.events)
	threadID := windows.GetCurrentThreadId()
	trayThreads.Store(threadID, native)
	defer trayThreads.Delete(threadID)
	module, _, moduleError := kernel32.NewProc("GetModuleHandleW").Call(0)
	if module == 0 {
		ready <- fmt.Errorf("load tray module: %w", moduleError)
		return
	}
	className, _ := windows.UTF16PtrFromString("CVTranslate.Tray")
	windowClass := trayWindowClass{
		Size: uint32(unsafe.Sizeof(trayWindowClass{})), WindowProcedure: trayWindowCallback,
		Instance: module, ClassName: className,
	}
	registered, _, registerError := user32.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&windowClass)))
	if registered == 0 {
		ready <- fmt.Errorf("register tray window: %w", registerError)
		return
	}
	defer func() {
		removed, _, removeError := user32.NewProc("UnregisterClassW").Call(uintptr(unsafe.Pointer(className)), module)
		if removed == 0 {
			native.runError = errors.Join(native.runError, fmt.Errorf("unregister tray window: %w", removeError))
		}
	}()
	window, _, createError := user32.NewProc("CreateWindowExW").Call(
		0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(className)), 0,
		0, 0, 0, 0, 0, 0, module, 0,
	)
	if window == 0 {
		ready <- fmt.Errorf("create tray window: %w", createError)
		return
	}
	native.window = window
	defer func() {
		destroyed, _, destroyError := user32.NewProc("DestroyWindow").Call(window)
		if destroyed == 0 {
			native.runError = errors.Join(native.runError, fmt.Errorf("destroy tray window: %w", destroyError))
		}
	}()
	if err := native.createMenu(); err != nil {
		ready <- err
		return
	}
	defer func() {
		destroyed, _, destroyError := user32.NewProc("DestroyMenu").Call(native.menu)
		if destroyed == 0 {
			native.runError = errors.Join(native.runError, fmt.Errorf("destroy tray menu: %w", destroyError))
		}
	}()
	broadcastName, _ := windows.UTF16PtrFromString("TaskbarCreated")
	broadcast, _, broadcastError := user32.NewProc("RegisterWindowMessageW").Call(uintptr(unsafe.Pointer(broadcastName)))
	if broadcast == 0 {
		ready <- fmt.Errorf("register taskbar recovery: %w", broadcastError)
		return
	}
	native.taskbarCreated = uint32(broadcast)
	icon, iconError := createWindowsTrayIcon()
	if iconError != nil {
		ready <- iconError
		return
	}
	defer func() {
		destroyed, _, destroyError := user32.NewProc("DestroyIcon").Call(icon)
		if destroyed == 0 {
			native.runError = errors.Join(native.runError, fmt.Errorf("destroy tray image: %w", destroyError))
		}
	}()
	native.icon = trayIconData{
		Size: uint32(unsafe.Sizeof(trayIconData{})), Window: window, Identifier: 1,
		Flags:           0x01 | 0x02 | 0x04 | 0x80, // NIF_MESSAGE | NIF_ICON | NIF_TIP | NIF_SHOWTIP
		CallbackMessage: trayCallbackMessage, Icon: icon,
	}
	tooltip, _ := windows.UTF16FromString("CVTranslate")
	copy(native.icon.Tooltip[:], tooltip)
	defer func() {
		if native.iconAdded {
			if err := native.notifyIcon(trayIconDelete, &native.icon); err != nil {
				native.runError = errors.Join(native.runError, fmt.Errorf("remove tray icon: %w", err))
			}
		}
	}()
	if err := native.addIcon(); err != nil {
		ready <- err
		return
	}
	ready <- nil
	var message trayWindowMessage
	for {
		result, _, messageError := user32.NewProc("GetMessageW").Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(result) == -1 {
			native.runError = errors.Join(native.runError, fmt.Errorf("receive tray events: %w", messageError))
			return
		}
		if result == 0 {
			if !native.stopping && native.runError == nil {
				native.runError = errors.New("tray message loop stopped unexpectedly")
			}
			return
		}
		user32.NewProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&message)))
		user32.NewProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&message)))
	}
}

func createWindowsTrayIcon() (uintptr, error) {
	width, _, _ := user32.NewProc("GetSystemMetrics").Call(49)  // SM_CXSMICON
	height, _, _ := user32.NewProc("GetSystemMetrics").Call(50) // SM_CYSMICON
	// Windows accepts a PNG icon resource directly, without an ICO wrapper or
	// temporary file. This is an owned HICON, released after notification removal.
	icon, _, iconError := user32.NewProc("CreateIconFromResourceEx").Call(
		uintptr(unsafe.Pointer(&windowsTrayImage[0])), uintptr(len(windowsTrayImage)),
		1, 0x00030000, width, height, 0,
	)
	if icon == 0 {
		return 0, fmt.Errorf("create tray image: %w", iconError)
	}
	return icon, nil
}

func (native *windowsTray) createMenu() error {
	menu, _, menuError := user32.NewProc("CreatePopupMenu").Call()
	if menu == 0 {
		return fmt.Errorf("create tray menu: %w", menuError)
	}
	native.menu = menu
	for _, item := range []struct {
		flags   uintptr
		command uintptr
		label   string
	}{
		{command: trayOpenCommand, label: "\u6253\u5f00\u7ba1\u7406\u754c\u9762"},
		{flags: 0x0800}, // MF_SEPARATOR
		{command: trayQuitCommand, label: "\u9000\u51fa"},
	} {
		label, _ := windows.UTF16PtrFromString(item.label)
		added, _, appendError := user32.NewProc("AppendMenuW").Call(menu, item.flags, item.command, uintptr(unsafe.Pointer(label)))
		if added == 0 {
			user32.NewProc("DestroyMenu").Call(menu)
			return fmt.Errorf("populate tray menu: %w", appendError)
		}
	}
	return nil
}

func updateWindowsTrayIcon(operation uintptr, icon *trayIconData) error {
	updated, _, _ := notifyTrayIcon.Call(operation, uintptr(unsafe.Pointer(icon)))
	if updated == 0 {
		// Shell_NotifyIcon does not promise a meaningful GetLastError value.
		return errors.New("Windows shell rejected the notification icon operation")
	}
	return nil
}

func (native *windowsTray) addIcon() error {
	native.icon.TimeoutOrVersion = 0
	if err := native.notifyIcon(trayIconAdd, &native.icon); err != nil {
		return fmt.Errorf("add tray icon: %w", err)
	}
	native.iconAdded = true
	native.icon.TimeoutOrVersion = 4 // NOTIFYICON_VERSION_4 includes keyboard activation.
	if err := native.notifyIcon(trayIconSetVersion, &native.icon); err != nil {
		return fmt.Errorf("configure tray interaction: %w", err)
	}
	return nil
}

func dispatchTrayWindowMessage(window uintptr, message uint32, parameter, data uintptr) uintptr {
	stored, exists := trayThreads.Load(windows.GetCurrentThreadId())
	if exists {
		native := stored.(*windowsTray)
		if native.window == window {
			switch message {
			case trayCloseMessage:
				native.stopping = true
				user32.NewProc("EndMenu").Call()
				user32.NewProc("PostQuitMessage").Call(0)
				return 0
			case 0x0011: // WM_QUERYENDSESSION
				return 1
			case 0x0010: // WM_CLOSE: never destroy the exit entry before application cleanup.
				native.queue.requestQuit()
				return 0
			case 0x0016: // WM_ENDSESSION
				if parameter != 0 {
					native.queue.requestQuit()
				}
				return 0
			case trayCallbackMessage:
				if native.stopping || native.queue.quitting {
					return 0
				}
				switch uint16(data) {
				case traySelect, trayKeySelect:
					native.queue.requestOpen()
				case trayContextMenu:
					native.showMenu(parameter)
				}
				return 0
			case native.taskbarCreated:
				if !native.stopping && !native.queue.quitting {
					native.iconAdded = false
					if err := native.addIcon(); err != nil {
						native.runError = errors.Join(native.runError, err)
						user32.NewProc("PostQuitMessage").Call(0)
					}
				}
				return 0
			}
		}
	}
	result, _, _ := user32.NewProc("DefWindowProcW").Call(window, uintptr(message), parameter, data)
	return result
}

func (native *windowsTray) showMenu(position uintptr) {
	point := struct{ Horizontal, Vertical int32 }{int32(int16(position)), int32(int16(position >> 16))}
	if point.Horizontal == -1 && point.Vertical == -1 {
		located, _, _ := user32.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&point)))
		if located == 0 {
			return
		}
	}
	// Foreground ownership is needed only for this explicit user-invoked menu;
	// WM_NULL after dismissal lets Windows dismiss later menus normally.
	user32.NewProc("SetForegroundWindow").Call(native.window)
	command, _, _ := user32.NewProc("TrackPopupMenuEx").Call(
		native.menu, 0x0002|0x0080|0x0100, // TPM_RIGHTBUTTON | TPM_NONOTIFY | TPM_RETURNCMD
		uintptr(point.Horizontal), uintptr(point.Vertical), native.window, 0,
	)
	user32.NewProc("PostMessageW").Call(native.window, 0, 0, 0)
	if native.stopping {
		return
	}
	switch command {
	case trayOpenCommand:
		native.queue.requestOpen()
	case trayQuitCommand:
		native.queue.requestQuit()
	}
}

func (native *windowsTray) close() error {
	native.mutex.Lock()
	defer native.mutex.Unlock()
	select {
	case <-native.done:
		return native.runError
	default:
	}
	posted, _, postError := user32.NewProc("PostMessageW").Call(native.window, trayCloseMessage, 0, 0)
	if posted == 0 {
		return fmt.Errorf("stop tray message loop: %w", postError)
	}
	<-native.done
	return native.runError
}
