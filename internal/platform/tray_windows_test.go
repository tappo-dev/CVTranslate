//go:build windows && (amd64 || arm64)

package platform

import (
	"errors"
	"reflect"
	"testing"
	"unsafe"
)

func TestWindowsTrayABILayout(test *testing.T) {
	icon := trayIconData{}
	if unsafe.Sizeof(icon) != 976 || unsafe.Offsetof(icon.Window) != 8 ||
		unsafe.Offsetof(icon.Icon) != 32 || unsafe.Offsetof(icon.Tooltip) != 40 ||
		unsafe.Offsetof(icon.TimeoutOrVersion) != 816 || unsafe.Offsetof(icon.GUID) != 952 ||
		unsafe.Offsetof(icon.BalloonIcon) != 968 {
		test.Fatalf("incorrect 64-bit NOTIFYICONDATAW layout: size=%d", unsafe.Sizeof(icon))
	}
	windowClass := trayWindowClass{}
	if unsafe.Sizeof(windowClass) != 80 || unsafe.Offsetof(windowClass.WindowProcedure) != 8 ||
		unsafe.Offsetof(windowClass.ClassName) != 64 {
		test.Fatalf("incorrect 64-bit WNDCLASSEXW layout: size=%d", unsafe.Sizeof(windowClass))
	}
	message := trayWindowMessage{}
	if unsafe.Sizeof(message) != 48 || unsafe.Offsetof(message.Parameter) != 16 || unsafe.Offsetof(message.Data) != 24 {
		test.Fatalf("incorrect 64-bit MSG layout: size=%d", unsafe.Sizeof(message))
	}
}

func TestTrayIconRecoveryRenegotiatesKeyboardProtocol(test *testing.T) {
	var operations []uintptr
	native := &windowsTray{notifyIcon: func(operation uintptr, icon *trayIconData) error {
		operations = append(operations, operation)
		if operation == trayIconAdd && icon.TimeoutOrVersion != 0 {
			test.Fatal("re-added icon retained its old protocol union value")
		}
		if operation == trayIconSetVersion && icon.TimeoutOrVersion != 4 {
			test.Fatal("icon registration omitted the keyboard-aware protocol")
		}
		return nil
	}}
	for range 2 {
		native.iconAdded = false // Explorer discarded the previous notification area.
		if err := native.addIcon(); err != nil || !native.iconAdded {
			test.Fatalf("icon registration failed: added=%v error=%v", native.iconAdded, err)
		}
	}
	if !reflect.DeepEqual(operations, []uintptr{trayIconAdd, trayIconSetVersion, trayIconAdd, trayIconSetVersion}) {
		test.Fatalf("unexpected recovery protocol: %v", operations)
	}
}

func TestTrayIconRegistrationFailureRetainsCleanupOwnership(test *testing.T) {
	for _, failedOperation := range []uintptr{trayIconAdd, trayIconSetVersion} {
		shellFailure := errors.New("shell unavailable")
		native := &windowsTray{notifyIcon: func(operation uintptr, _ *trayIconData) error {
			if operation == failedOperation {
				return shellFailure
			}
			return nil
		}}
		err := native.addIcon()
		if !errors.Is(err, shellFailure) || native.iconAdded != (failedOperation == trayIconSetVersion) {
			test.Fatalf("failed operation %d lost error or cleanup ownership: added=%v error=%v", failedOperation, native.iconAdded, err)
		}
	}
}
