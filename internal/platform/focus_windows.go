//go:build windows && (amd64 || arm64)

package platform

import (
	"errors"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var errUnsupportedWindowsTextControl = errors.New("focused control requires Windows UI Automation inspection")

type windowsTextFocus struct {
	foreground  uintptr
	processID   uint32
	threadID    uintptr
	information guiThreadInfo
	style       uintptr
	class       string
}

func inspectWindowsTextFocus() (windowsTextFocus, error) {
	var focus windowsTextFocus
	focus.foreground, _, _ = user32.NewProc("GetForegroundWindow").Call()
	if focus.foreground == 0 {
		return focus, errors.New("no foreground text target")
	}
	focus.threadID, _, _ = user32.NewProc("GetWindowThreadProcessId").Call(focus.foreground, uintptr(unsafe.Pointer(&focus.processID)))
	if focus.threadID == 0 || focus.processID == windows.GetCurrentProcessId() {
		return focus, errors.New("text target cannot be inspected safely")
	}
	focus.information.Size = uint32(unsafe.Sizeof(guiThreadInfo{}))
	valid, _, _ := user32.NewProc("GetGUIThreadInfo").Call(focus.threadID, uintptr(unsafe.Pointer(&focus.information)))
	if valid == 0 || focus.information.Focus == 0 || focus.information.Active != focus.foreground {
		return focus, errors.New("text focus is unavailable")
	}
	var focusedProcessID uint32
	focusedThreadID, _, _ := user32.NewProc("GetWindowThreadProcessId").Call(focus.information.Focus, uintptr(unsafe.Pointer(&focusedProcessID)))
	if focusedThreadID != focus.threadID || focusedProcessID != focus.processID {
		return focus, errors.New("text focus identity is unavailable")
	}
	var className [256]uint16
	length, _, _ := user32.NewProc("GetClassNameW").Call(focus.information.Focus, uintptr(unsafe.Pointer(&className[0])), uintptr(len(className)))
	focus.class = strings.ToLower(windows.UTF16ToString(className[:]))
	if length == 0 {
		return focus, errors.New("focused control class is unavailable")
	}
	if !supportedEditClass(focus.class) {
		return focus, errUnsupportedWindowsTextControl
	}
	focus.style, _, _ = user32.NewProc("GetWindowLongPtrW").Call(focus.information.Focus, ^uintptr(15))
	if focus.style&0x20 != 0 {
		return focus, errPasswordTarget
	}
	if focus.style == 0 {
		return focus, errors.New("text target style is unavailable")
	}
	enabled, _, _ := user32.NewProc("IsWindowEnabled").Call(focus.information.Focus)
	visible, _, _ := user32.NewProc("IsWindowVisible").Call(focus.information.Focus)
	unicodeWindow, _, _ := user32.NewProc("IsWindowUnicode").Call(focus.information.Focus)
	if enabled == 0 || visible == 0 || unicodeWindow == 0 {
		return focus, errors.New("text target is not a visible enabled Unicode control")
	}
	return focus, nil
}
