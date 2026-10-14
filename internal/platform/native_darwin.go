//go:build darwin && cgo

package platform

/*
#cgo CFLAGS: -x objective-c -fblocks
#cgo LDFLAGS: -framework Cocoa -framework ApplicationServices -framework Security -framework Carbon
#include <stdlib.h>
#include "native_darwin.h"
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"unsafe"

	"golang.design/x/hotkey"
	"golang.design/x/hotkey/mainthread"
)

// RunMain must be called directly from main. The supplied function runs off the
// Cocoa thread; it must close the Desktop before returning and ending the loop.
func RunMain(run func()) {
	mainthread.Init(func() {
		installReopen()
		defer uninstallReopen()
		enableMacTray()
		defer disableMacTray()
		run()
	})
}
func nativeModifiers() map[string]hotkey.Modifier {
	return map[string]hotkey.Modifier{"CTRL": hotkey.ModCtrl, "ALT": hotkey.ModOption, "SHIFT": hotkey.ModShift, "CMD": hotkey.ModCmd}
}

type macDesktop struct{ state unsafe.Pointer }

func newNativeDesktop() nativeDesktop { return &macDesktop{state: C.cv_create()} }
func (desktop *macDesktop) status() Status {
	trusted := desktop.state != nil && C.cv_trusted() != 0
	listening := trusted && C.cv_copy_permitted() != 0
	message := "Command+C*3 translates and pastes without target verification; Command+C*4 translates and opens WebUI. Only explicitly copied text is submitted"
	if !trusted {
		message = "Accessibility permission unavailable; shortcuts and automatic paste are unavailable while WebUI remains usable. Grant Accessibility and Input Monitoring in System Settings > Privacy & Security, then restart the application"
	} else if !listening {
		message = "Input Monitoring permission unavailable; repeated-copy shortcuts are unavailable while WebUI remains usable. Grant permission in System Settings > Privacy & Security > Input Monitoring, then restart the application"
	}
	return Status{HotkeysAvailable: listening, PasteAvailable: trusted, Message: message}
}

func macError(code C.int) (err error) {
	defer func() { err = describeNativeError(err) }()
	switch code {
	case 0:
		return nil
	case 1:
		return errors.New("Accessibility permission unavailable")
	case 2:
		return errors.New("sensitive clipboard or password target; capture refused")
	case 3:
		return errors.New("clipboard has no supported text or memory is unavailable")
	case 4:
		return errors.New("clipboard changed; capture or paste cancelled")
	case 5:
		return errors.New("focused editable input changed or is unavailable; paste cancelled")
	case 6:
		return errors.New("modifier pressed before paste dispatch")
	case 9:
		return errors.New("Input Monitoring permission unavailable; grant it manually and restart the application")
	case 10:
		return errors.New("could not install the macOS repeated-copy listener; check permissions and restart the application")
	default:
		return errors.New("native paste event creation failed")
	}
}

func (desktop *macDesktop) capture() (ClipboardCapture, error) {
	if desktop.state == nil {
		return ClipboardCapture{}, errors.New("native desktop is unavailable")
	}
	var text *C.char
	var version, token C.uint64_t
	if err := macError(C.cv_capture(desktop.state, &text, &version, &token)); err != nil {
		return ClipboardCapture{}, err
	}
	defer C.free(unsafe.Pointer(text))
	target := ""
	if token != 0 {
		target = strconv.FormatUint(uint64(token), 10)
	}
	return ClipboardCapture{Text: C.GoString(text), version: uint64(version), target: target}, nil
}

func (desktop *macDesktop) validate(target string, version uint64) error {
	token, err := strconv.ParseUint(target, 10, 64)
	if err != nil || desktop.state == nil {
		return errors.New("native target is unavailable")
	}
	return macError(C.cv_validate(desktop.state, C.uint64_t(token), C.uint64_t(version)))
}
func (*macDesktop) modifiersReleased() bool { return C.cv_released() != 0 }
func (*macDesktop) write(version uint64, text string) (uint64, error) {
	if strings.ContainsRune(text, 0) {
		return 0, errors.New("translation contains a NUL character")
	}
	contents := C.CString(text)
	defer C.free(unsafe.Pointer(contents))
	var written C.uint64_t
	err := macError(C.cv_write(C.uint64_t(version), contents, &written))
	return uint64(written), err
}
func (desktop *macDesktop) dispatch(ctx context.Context, target string, version uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	token, err := strconv.ParseUint(target, 10, 64)
	if err != nil || desktop.state == nil {
		return errors.New("native target is unavailable")
	}
	return macError(C.cv_dispatch(desktop.state, C.uint64_t(token), C.uint64_t(version)))
}
func (desktop *macDesktop) close() error { C.cv_close(desktop.state); desktop.state = nil; return nil }
func openBrowser(address string) error   { return exec.Command("/usr/bin/open", address).Run() }

func credentialError(code C.int) error {
	if code == 0 {
		return nil
	}
	return fmt.Errorf("macOS Keychain operation failed (OSStatus %d); unlock or authorize the Keychain manually", int(code))
}
func credentialNames(namespace, id string) (*C.char, *C.char, func()) {
	service, account := C.CString(namespace), C.CString(id)
	return service, account, func() { C.free(unsafe.Pointer(service)); C.free(unsafe.Pointer(account)) }
}
func getCredential(namespace, id string) (string, error) {
	service, account, cleanup := credentialNames(namespace, id)
	defer cleanup()
	var value unsafe.Pointer
	var length C.size_t
	status := C.cv_credential_get(service, account, &value, &length)
	if status == -25300 {
		return "", nil
	} // errSecItemNotFound
	if err := credentialError(status); err != nil {
		return "", err
	}
	defer C.free(value)
	return string(C.GoBytes(value, C.int(length))), nil
}
func setCredential(namespace, id, value string) error {
	service, account, cleanup := credentialNames(namespace, id)
	defer cleanup()
	contents := C.CBytes([]byte(value))
	defer C.free(contents)
	return credentialError(C.cv_credential_set(service, account, contents, C.size_t(len(value))))
}
func deleteCredential(namespace, id string) error {
	service, account, cleanup := credentialNames(namespace, id)
	defer cleanup()
	status := C.cv_credential_delete(service, account)
	if status == -25300 {
		return nil
	}
	return credentialError(status)
}
func hasCredential(namespace, id string) (bool, error) {
	service, account, cleanup := credentialNames(namespace, id)
	defer cleanup()
	var value unsafe.Pointer
	var length C.size_t
	status := C.cv_credential_get(service, account, &value, &length)
	if value != nil {
		C.free(value)
	}
	if status == -25300 {
		return false, nil
	}
	return status == 0, credentialError(status)
}
