//go:build windows && (amd64 || arm64)

package platform

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"unsafe"

	"github.com/zalando/go-keyring"
	"golang.design/x/hotkey"
	"golang.org/x/sys/windows"
)

var user32 = windows.NewLazySystemDLL("user32.dll")
var kernel32 = windows.NewLazySystemDLL("kernel32.dll")
var shell32 = windows.NewLazySystemDLL("shell32.dll")

func RunMain(run func()) { run() }
func nativeModifiers() map[string]hotkey.Modifier {
	return map[string]hotkey.Modifier{"CTRL": hotkey.ModCtrl, "ALT": hotkey.ModAlt, "SHIFT": hotkey.ModShift, "WIN": hotkey.ModWin}
}

type windowsDesktop struct{}

func newNativeDesktop() nativeDesktop { return &windowsDesktop{} }
func (*windowsDesktop) status() Status {
	return Status{HotkeysAvailable: true, PasteAvailable: true, Message: "Native Edit/RichEdit and UI Automation-exposed text controls are inspected on shortcut invocation. Guarded paste requires a verifiable editable target; application accessibility support varies. Sensitive source capture is refused. Changed or unverifiable targets block guarded paste, except the explicit Ctrl+C*3 direct-paste experiment; no simulated copy or focus stealing"}
}
func (*windowsDesktop) close() error { return nil }

type guiThreadInfo struct {
	Size      uint32
	Flags     uint32
	Active    uintptr
	Focus     uintptr
	Capture   uintptr
	MenuOwner uintptr
	MoveSize  uintptr
	Caret     uintptr
	CaretRect [4]int32
}

func windowsTarget() (string, error) {
	focus, err := inspectWindowsTextFocus()
	return readWindowsPasteTarget(focus, err, windowsEditTarget, inspectWindowsAccessibilityTarget)
}

func inspectWindowsCapturedTarget(target string) (string, error) {
	return inspectWindowsTargetWithReader(target, windowsEditTarget, func() (string, error) {
		foreground, _, _ := user32.NewProc("GetForegroundWindow").Call()
		return inspectWindowsAccessibilityTarget(foreground)
	})
}

func inspectWindowsTargetWithReader(target string, readNative, readAccessible func() (string, error)) (string, error) {
	if strings.HasPrefix(target, "uia:") {
		return readAccessible()
	}
	return readNative()
}

// Unsupported control classes may use UI Automation, but a refused native
// password or inaccessible focus must never be retried through another reader.
func readWindowsPasteTarget(focus windowsTextFocus, focusError error, readNative func() (string, error), readAccessible func(uintptr) (string, error)) (string, error) {
	if errors.Is(focusError, errUnsupportedWindowsTextControl) {
		target, err := readAccessible(focus.foreground)
		if err != nil {
			return "", fmt.Errorf("UI Automation target inspection: %w", err)
		}
		return target, nil
	}
	if focusError != nil {
		return "", fmt.Errorf("Win32 focus inspection: %w", focusError)
	}
	return readNative()
}

func windowsEditTarget() (string, error) {
	foreground, _, _ := user32.NewProc("GetForegroundWindow").Call()
	if foreground == 0 {
		return "", errors.New("Win32 foreground window is unavailable")
	}
	var processID uint32
	threadID, _, _ := user32.NewProc("GetWindowThreadProcessId").Call(foreground, uintptr(unsafe.Pointer(&processID)))
	// Same-process window procedures can bypass SendMessageTimeout's timeout.
	// The application has no native editor of its own that needs automatic paste.
	if threadID == 0 || processID == windows.GetCurrentProcessId() {
		return "", errors.New("Win32 foreground thread is unavailable or belongs to this application")
	}
	information := guiThreadInfo{Size: uint32(unsafe.Sizeof(guiThreadInfo{}))}
	valid, _, _ := user32.NewProc("GetGUIThreadInfo").Call(threadID, uintptr(unsafe.Pointer(&information)))
	if valid == 0 || information.Focus == 0 || information.Active != foreground {
		return "", errors.New("Win32 focused input is unavailable")
	}
	var focusProcessID uint32
	focusThreadID, _, _ := user32.NewProc("GetWindowThreadProcessId").Call(information.Focus, uintptr(unsafe.Pointer(&focusProcessID)))
	if focusThreadID != threadID || focusProcessID != processID {
		return "", errors.New("Win32 focused input process or thread differs from the foreground")
	}
	var className [256]uint16
	length, _, _ := user32.NewProc("GetClassNameW").Call(information.Focus, uintptr(unsafe.Pointer(&className[0])), uintptr(len(className)))
	if length == 0 {
		return "", errors.New("Win32 input class is unavailable")
	}
	class := strings.ToLower(windows.UTF16ToString(className[:]))
	if !supportedEditClass(class) {
		return "", errors.New("Win32 input class changed or is unsupported by the captured reader")
	}
	style, _, _ := user32.NewProc("GetWindowLongPtrW").Call(information.Focus, ^uintptr(15)) // GWL_STYLE = -16
	if style&0x20 != 0 {
		return "", errPasswordTarget
	}
	if style&0x800 != 0 {
		return "", errors.New("Win32 input is read-only")
	} // ES_READONLY
	enabled, _, _ := user32.NewProc("IsWindowEnabled").Call(information.Focus)
	visible, _, _ := user32.NewProc("IsWindowVisible").Call(information.Focus)
	if enabled == 0 || visible == 0 {
		return "", errors.New("Win32 input is disabled or invisible")
	}
	unicodeWindow, _, _ := user32.NewProc("IsWindowUnicode").Call(information.Focus)
	if unicodeWindow == 0 {
		return "", errors.New("Win32 input is not a Unicode control")
	} // ANSI conversion can make text lengths ambiguous.
	fingerprint, fingerprintError := windowsEditFingerprint(information.Focus)
	if fingerprintError != nil {
		return "", fmt.Errorf("Win32 text/selection inspection: %w", fingerprintError)
	}
	current, _, _ := user32.NewProc("GetForegroundWindow").Call()
	if current != foreground {
		return "", errors.New("Win32 foreground changed during target inspection")
	}
	confirmed := guiThreadInfo{Size: uint32(unsafe.Sizeof(guiThreadInfo{}))}
	valid, _, _ = user32.NewProc("GetGUIThreadInfo").Call(threadID, uintptr(unsafe.Pointer(&confirmed)))
	if valid == 0 || confirmed.Active != foreground || confirmed.Focus != information.Focus || confirmed.Caret != information.Caret || confirmed.CaretRect != information.CaretRect {
		return "", errors.New("Win32 focused input or caret changed during target inspection")
	}
	return fmt.Sprintf("%x:%x:%x:%x:%x:%v:%x:%x:%x", foreground, information.Focus, processID, threadID, confirmed.Caret, confirmed.CaretRect, fingerprint.Length, fingerprint.Selection, fingerprint.Digest), nil
}

func clipboardSequence() uint64 {
	sequence, _, _ := user32.NewProc("GetClipboardSequenceNumber").Call()
	return uint64(sequence)
}

// withWindowsClipboard holds the OS clipboard lock for the entire generation
// check and read/write, so a concurrent copy cannot be silently overwritten.
func withWindowsClipboard(operation func() error) error {
	return withWindowsClipboardWindow(func(window uintptr) error {
		return withOpenWindowsClipboard(window, operation)
	})
}

// Keep the owner alive across publication and the subsequent locked receipt
// check. Both OpenClipboard and CloseClipboard must run on this same thread.
func withWindowsClipboardWindow(operation func(uintptr) error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	class, _ := windows.UTF16PtrFromString("STATIC")
	window, _, createError := user32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(class)), 0, 0, 0, 0, 0, 0, ^uintptr(2), 0, 0, 0) // HWND_MESSAGE
	if window == 0 {
		return fmt.Errorf("create clipboard owner: %w", createError)
	}
	defer user32.NewProc("DestroyWindow").Call(window)
	return operation(window)
}

func withOpenWindowsClipboard(window uintptr, operation func() error) (err error) {
	opened, _, openError := user32.NewProc("OpenClipboard").Call(window)
	if opened == 0 {
		return fmt.Errorf("clipboard is busy or unavailable: %w", openError)
	}
	defer func() {
		closed, _, closeError := user32.NewProc("CloseClipboard").Call()
		if closed == 0 {
			err = errors.Join(err, fmt.Errorf("close clipboard: %w", closeError))
		}
	}()
	return operation()
}

func clipboardSensitive() bool {
	for _, format := range []string{"ExcludeClipboardContentFromMonitorProcessing", "Clipboard Viewer Ignore", "application/x-keepassxc", "org.nspasteboard.ConcealedType"} {
		name, _ := windows.UTF16PtrFromString(format)
		identifier, _, _ := user32.NewProc("RegisterClipboardFormatW").Call(uintptr(unsafe.Pointer(name)))
		present, _, _ := user32.NewProc("IsClipboardFormatAvailable").Call(identifier)
		if present != 0 {
			return true
		}
	}
	return false
}

func (*windowsDesktop) capture() (capture ClipboardCapture, err error) {
	defer func() { err = describeNativeError(err) }()
	return captureWindowsClipboardWithTarget(windowsTarget, readWindowsClipboardText)
}

// A failed paste inspection does not discard an explicitly copied source, but
// its reason must travel with that exact capture. Password and clipboard read
// failures remain fatal and must never expose text to the translation workflow.
func captureWindowsClipboardWithTarget(inspectTarget func() (string, error), readClipboard func() (string, uint64, error)) (ClipboardCapture, error) {
	target, initialError := inspectTarget()
	if errors.Is(initialError, errPasswordTarget) {
		return ClipboardCapture{}, initialError
	}
	text, version, err := readClipboard()
	if err != nil {
		return ClipboardCapture{}, err
	}
	confirmedTarget, confirmationError := inspectTarget()
	if errors.Is(confirmationError, errPasswordTarget) {
		return ClipboardCapture{}, confirmationError
	}
	capture := ClipboardCapture{Text: text, version: version, target: target}
	switch {
	case initialError != nil:
		capture.pasteError = fmt.Errorf("paste target unavailable at capture (initial inspection): %w", initialError)
	case confirmationError != nil:
		capture.pasteError = fmt.Errorf("paste target unavailable at capture (confirmation): %w", confirmationError)
	case target != confirmedTarget:
		capture.pasteError = errors.New("paste target changed while the clipboard was being captured")
	case target == "":
		capture.pasteError = errors.New("target inspection returned no verified editable input")
	}
	if capture.pasteError != nil {
		capture.target = ""
		capture.pasteError = describeNativeError(capture.pasteError)
	}
	return capture, nil
}

func readWindowsClipboardText() (text string, version uint64, err error) {
	err = withWindowsClipboard(func() error {
		version = clipboardSequence()
		if version == 0 {
			return errors.New("clipboard generation is unavailable")
		}
		if clipboardSensitive() {
			return errors.New("clipboard is marked sensitive; capture refused")
		}
		handle, _, _ := user32.NewProc("GetClipboardData").Call(13) // CF_UNICODETEXT
		if handle == 0 {
			return errors.New("clipboard has no Unicode text")
		}
		size, _, _ := kernel32.NewProc("GlobalSize").Call(handle)
		if size == 0 || size > 4*1024*1024 || size%2 != 0 {
			return errors.New("clipboard text size is invalid or too large")
		}
		pointer, _, _ := kernel32.NewProc("GlobalLock").Call(handle)
		if pointer == 0 {
			return errors.New("clipboard data could not be locked")
		}
		defer kernel32.NewProc("GlobalUnlock").Call(handle)
		// Copy OS-owned memory through Win32 rather than converting a stored
		// uintptr into a Go pointer (which violates Go's pointer rules).
		encoded := make([]uint16, int(size/2))
		kernel32.NewProc("RtlMoveMemory").Call(uintptr(unsafe.Pointer(&encoded[0])), pointer, size)
		text = windows.UTF16ToString(encoded)
		if clipboardSequence() != version {
			return errors.New("clipboard changed during capture")
		}
		return nil
	})
	if err != nil {
		return "", 0, err
	}
	return
}

func (*windowsDesktop) validate(target string, version uint64) (err error) {
	defer func() { err = describeNativeError(err) }()
	if target == "" {
		return errors.New("no verified editable target")
	}
	current, err := inspectWindowsCapturedTarget(target)
	if err != nil {
		return err
	}
	if current != target {
		return errors.New("focused input, text, selection or caret changed or became unreadable; paste cancelled")
	}
	if clipboardSequence() != version {
		return errors.New("clipboard changed; paste cancelled")
	}
	return nil
}

func (*windowsDesktop) modifiersReleased() bool {
	for _, key := range []uintptr{0x10, 0x11, 0x12, 0x5b, 0x5c} {
		state, _, _ := user32.NewProc("GetAsyncKeyState").Call(key)
		if state&0x8000 != 0 {
			return false
		}
	}
	return true
}

func (*windowsDesktop) write(version uint64, text string) (writtenVersion uint64, err error) {
	defer func() { err = describeNativeError(err) }()
	encoded, err := windows.UTF16FromString(text)
	if err != nil {
		return 0, errors.New("translation contains a NUL character")
	}
	defer clear(encoded)
	err = withWindowsClipboardWindow(func(window uintptr) error {
		writtenVersion, err = publishWindowsClipboardText(version,
			func(operation func() error) error { return withOpenWindowsClipboard(window, operation) },
			clipboardSequence,
			func() error { return writeWindowsClipboardText(encoded) },
			func() error { return confirmWindowsClipboardText(window, encoded) })
		return err
	})
	if err != nil {
		return 0, err
	}
	return
}

// INPUT has an 8-byte union offset and 32-byte union on both Windows amd64
// and arm64. KEYBDINPUT's ULONG_PTR is 8-byte aligned inside that union.
type keyboardInput struct {
	Kind         uint32
	Padding      uint32
	VirtualKey   uint16
	ScanCode     uint16
	Flags        uint32
	Time         uint32
	Alignment    uint32
	ExtraInfo    uintptr
	UnionPadding [8]byte
}

func (desktop *windowsDesktop) dispatch(ctx context.Context, target string, version uint64) (err error) {
	defer func() { err = describeNativeError(err) }()
	return dispatchWindowsPaste(ctx, func() error { return desktop.validate(target, version) }, desktop.modifiersReleased, sendWindowsPasteInput)
}

func (*windowsDesktop) validateClipboard(version uint64) error {
	if version == 0 || clipboardSequence() != version {
		return describeNativeError(errors.New("clipboard changed; direct paste cancelled"))
	}
	return nil
}

func (desktop *windowsDesktop) dispatchDirect(ctx context.Context, version uint64) (err error) {
	defer func() { err = describeNativeError(err) }()
	// Explicit Ctrl+C*3 experiment: no target, caret, focus or UIA inspection.
	return dispatchWindowsPaste(ctx, func() error { return desktop.validateClipboard(version) }, desktop.modifiersReleased, sendWindowsPasteInput)
}

func dispatchWindowsPaste(ctx context.Context, validate func() error, modifiersReleased func() bool, sendInput func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validate(); err != nil {
		return err
	}
	if !modifiersReleased() {
		return errors.New("modifier pressed before dispatch")
	}
	// Fingerprint messages may have waited for a slow target. A cancellation
	// during that bounded wait must not be followed by synthetic input.
	if err := ctx.Err(); err != nil {
		return err
	}
	return sendInput()
}

func sendWindowsPasteInput() error {
	inputs := []keyboardInput{{Kind: 1, VirtualKey: 0x11}, {Kind: 1, VirtualKey: 0x56}, {Kind: 1, VirtualKey: 0x56, Flags: 2}, {Kind: 1, VirtualKey: 0x11, Flags: 2}}
	sent, _, dispatchError := user32.NewProc("SendInput").Call(uintptr(len(inputs)), uintptr(unsafe.Pointer(&inputs[0])), unsafe.Sizeof(inputs[0]))
	if sent != uintptr(len(inputs)) {
		// Balance any synthetic keydowns from a partial dispatch, never retry paste.
		if sent > 0 {
			user32.NewProc("SendInput").Call(2, uintptr(unsafe.Pointer(&inputs[2])), unsafe.Sizeof(inputs[0]))
		}
		return fmt.Errorf("paste input dispatch failed (%d/4); UIPI may block elevated targets: %v", sent, dispatchError)
	}
	return nil
}

func openBrowser(address string) error {
	operation, _ := windows.UTF16PtrFromString("open")
	target, _ := windows.UTF16PtrFromString(address)
	result, _, _ := shell32.NewProc("ShellExecuteW").Call(0, uintptr(unsafe.Pointer(operation)), uintptr(unsafe.Pointer(target)), 0, 0, 1)
	if result <= 32 {
		return fmt.Errorf("default browser launch failed (code %d)", result)
	}
	return nil
}

func getCredential(namespace, id string) (string, error) {
	value, err := keyring.Get(namespace, id)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", nil
	}
	return value, err
}
func setCredential(namespace, id, value string) error { return keyring.Set(namespace, id, value) }
func deleteCredential(namespace, id string) error {
	err := keyring.Delete(namespace, id)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
func hasCredential(namespace, id string) (bool, error) {
	_, err := keyring.Get(namespace, id)
	if errors.Is(err, keyring.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}
