//go:build windows && (amd64 || arm64)

package platform

import (
	"errors"
	"slices"
	"unsafe"
)

// Publication can synthesize additional formats during CloseClipboard. A
// sequence sampled before that close is not the committed write's generation.
// Reopen once and verify ownership/content before accepting a post-close value;
// simply sampling a newer generation would bless an intervening external copy.
func publishWindowsClipboardText(expectedVersion uint64, withClipboard func(func() error) error, currentVersion func() uint64, writeText, confirmText func() error) (uint64, error) {
	if err := withClipboard(func() error {
		if expectedVersion == 0 || currentVersion() != expectedVersion {
			return errors.New("clipboard changed before write; paste cancelled")
		}
		return writeText()
	}); err != nil {
		return 0, err
	}
	var writtenVersion uint64
	if err := withClipboard(func() error {
		versionBeforeConfirmation := currentVersion()
		if versionBeforeConfirmation == 0 {
			return errors.New("clipboard generation unavailable after publication")
		}
		if err := confirmText(); err != nil {
			return err
		}
		if currentVersion() != versionBeforeConfirmation {
			return errors.New("clipboard changed during publication confirmation; paste cancelled")
		}
		writtenVersion = versionBeforeConfirmation
		return nil
	}); err != nil {
		return 0, err
	}
	// Confirmation is read-only. Never refresh this receipt after releasing its
	// lock; the final dispatch check must still reject any subsequent clipboard edit.
	return writtenVersion, nil
}

// Caller owns the clipboard lock. encoded includes the terminating UTF-16 NUL.
func writeWindowsClipboardText(encoded []uint16) error {
	handle, _, _ := kernel32.NewProc("GlobalAlloc").Call(0x0002, uintptr(len(encoded)*2))
	if handle == 0 {
		return errors.New("clipboard allocation failed")
	}
	owned := true
	defer func() {
		if owned {
			kernel32.NewProc("GlobalFree").Call(handle)
		}
	}()
	pointer, _, _ := kernel32.NewProc("GlobalLock").Call(handle)
	if pointer == 0 {
		return errors.New("clipboard allocation lock failed")
	}
	kernel32.NewProc("RtlMoveMemory").Call(pointer, uintptr(unsafe.Pointer(&encoded[0])), uintptr(len(encoded)*2))
	kernel32.NewProc("GlobalUnlock").Call(handle)
	emptied, _, _ := user32.NewProc("EmptyClipboard").Call()
	if emptied == 0 {
		return errors.New("clipboard could not be cleared")
	}
	result, _, _ := user32.NewProc("SetClipboardData").Call(13, handle) // CF_UNICODETEXT
	if result == 0 {
		return errors.New("clipboard write failed after clearing")
	}
	owned = false // SetClipboardData transfers allocation ownership to Windows.
	return nil
}

func confirmWindowsClipboardText(window uintptr, expected []uint16) error {
	return verifyPublishedClipboardText(window, expected, func() uintptr {
		owner, _, _ := user32.NewProc("GetClipboardOwner").Call()
		return owner
	}, func() ([]uint16, error) {
		if clipboardSensitive() {
			return nil, errors.New("clipboard became sensitive during publication; paste cancelled")
		}
		// Reacquire the handle while locked; never access a pre-close HGLOBAL.
		handle, _, _ := user32.NewProc("GetClipboardData").Call(13)
		if handle == 0 {
			return nil, errors.New("published clipboard Unicode text is unavailable")
		}
		size, _, _ := kernel32.NewProc("GlobalSize").Call(handle)
		if size < uintptr(len(expected)*2) || size%2 != 0 {
			return nil, errors.New("published clipboard text size changed; paste cancelled")
		}
		pointer, _, _ := kernel32.NewProc("GlobalLock").Call(handle)
		if pointer == 0 {
			return nil, errors.New("published clipboard text could not be locked")
		}
		defer kernel32.NewProc("GlobalUnlock").Call(handle)
		// Read no more than our known output length, including its terminator.
		actual := make([]uint16, len(expected))
		kernel32.NewProc("RtlMoveMemory").Call(uintptr(unsafe.Pointer(&actual[0])), pointer, uintptr(len(actual)*2))
		return actual, nil
	})
}

// The write's message-only owner stays alive until this locked check finishes.
// Matching text alone is insufficient: another application's identical copy
// must not become our publication receipt. No unrelated owner's text is read.
func verifyPublishedClipboardText(expectedOwner uintptr, expectedText []uint16, currentOwner func() uintptr, readText func() ([]uint16, error)) error {
	if expectedOwner == 0 || currentOwner() != expectedOwner {
		return errors.New("clipboard ownership changed during publication; paste cancelled")
	}
	actualText, err := readText()
	defer clear(actualText)
	if err != nil {
		return err
	}
	if !slices.Equal(actualText, expectedText) {
		return errors.New("published clipboard text changed; paste cancelled")
	}
	if currentOwner() != expectedOwner {
		return errors.New("clipboard ownership changed during confirmation; paste cancelled")
	}
	return nil
}
