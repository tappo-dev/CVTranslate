//go:build windows && (amd64 || arm64)

package platform

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"time"
	"unsafe"
)

const (
	messageGetText              = 0x000d
	messageGetTextLength        = 0x000e
	messageGetSelection         = 0x00b0
	messageGetPasswordCharacter = 0x00d2
	maximumTargetCharacters     = 32767
)

var errPasswordTarget = errors.New("password input is focused; clipboard capture refused")

type editFingerprint struct {
	Digest    [32]byte
	Length    uint32
	Selection uint32
}

// Only system messages below WM_USER are used: Windows marshals their buffers
// across processes. EM_EXGETSEL/EM_GETTEXTEX would require remote memory and are
// deliberately not used. The cap makes EM_GETSEL's packed 16-bit result exact.
type editMessageReader func(message uintptr, buffer []uint16) (uintptr, error)

func readEditFingerprint(readMessage editMessageReader) (editFingerprint, error) {
	readSnapshot := func() (editFingerprint, error) {
		passwordCharacter, err := readMessage(messageGetPasswordCharacter, nil)
		if err != nil {
			return editFingerprint{}, err
		}
		if passwordCharacter != 0 {
			return editFingerprint{}, errPasswordTarget
		}
		length, err := readMessage(messageGetTextLength, nil)
		if err != nil {
			return editFingerprint{}, err
		}
		if length > maximumTargetCharacters {
			return editFingerprint{}, errors.New("editable target exceeds safe fingerprint limit")
		}
		selection, err := readMessage(messageGetSelection, nil)
		if err != nil {
			return editFingerprint{}, err
		}
		if selection > 0xffffffff || selection == 0xffffffff || selection&0xffff > length || (selection>>16)&0xffff > length {
			return editFingerprint{}, errors.New("editable target selection is unavailable")
		}
		buffer := make([]uint16, int(length)+1)
		defer clear(buffer)
		copied, err := readMessage(messageGetText, buffer)
		if err != nil {
			return editFingerprint{}, err
		}
		if copied != length || buffer[len(buffer)-1] != 0 {
			return editFingerprint{}, errors.New("editable target text changed or was truncated")
		}
		// Retain only the digest, not target contents. Clear temporary UTF-16 and
		// byte buffers promptly; neither enters logs, persistence or provider input.
		encoded := make([]byte, int(length)*2)
		defer clear(encoded)
		for index, character := range buffer[:int(length)] {
			binary.LittleEndian.PutUint16(encoded[index*2:], character)
		}
		return editFingerprint{Digest: sha256.Sum256(encoded), Length: uint32(length), Selection: uint32(selection)}, nil
	}
	first, err := readSnapshot()
	if err != nil {
		return editFingerprint{}, err
	}
	second, err := readSnapshot()
	if err != nil {
		return editFingerprint{}, err
	}
	if first != second {
		return editFingerprint{}, errors.New("editable target changed during fingerprint capture")
	}
	return first, nil
}

func windowsEditFingerprint(window uintptr) (editFingerprint, error) {
	return readEditFingerprint(windowsEditMessageReader(window, false))
}

func windowsEditMessageReader(window uintptr, allowReadOnly bool) editMessageReader {
	deadline := time.Now().Add(400 * time.Millisecond)
	return func(message uintptr, buffer []uint16) (uintptr, error) {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return 0, errors.New("editable target inspection timed out")
		}
		// Recheck the native password/read-only style before every message,
		// especially WM_GETTEXT. A password toggle between checks is still an
		// unavoidable cross-process race, not an atomic security guarantee.
		style, _, styleError := user32.NewProc("GetWindowLongPtrW").Call(window, ^uintptr(15))
		if style == 0 {
			return 0, errors.New("editable target style is unavailable: " + styleError.Error())
		}
		if style&0x20 != 0 {
			return 0, errPasswordTarget
		}
		if style&0x800 != 0 && !allowReadOnly {
			return 0, errors.New("editable target became read-only")
		}
		timeout := min(uintptr(remaining/time.Millisecond), 50)
		if timeout == 0 {
			return 0, errors.New("editable target inspection timed out")
		}
		var result, succeeded uintptr
		// SMTO_BLOCK | SMTO_ABORTIFHUNG | SMTO_ERRORONEXIT. No NOTIMEOUTIFNOTHUNG.
		if message == messageGetText {
			succeeded, _, _ = user32.NewProc("SendMessageTimeoutW").Call(window, message, uintptr(len(buffer)), uintptr(unsafe.Pointer(&buffer[0])), 0x23, timeout, uintptr(unsafe.Pointer(&result)))
		} else {
			succeeded, _, _ = user32.NewProc("SendMessageTimeoutW").Call(window, message, 0, 0, 0x23, timeout, uintptr(unsafe.Pointer(&result)))
		}
		if succeeded == 0 {
			return 0, errors.New("editable target message timed out or was denied")
		}
		return result, nil
	}
}

func supportedEditClass(class string) bool {
	switch class {
	case "edit", "richedit", "richedit20w", "richedit50w", "richedit60w", "richeditd2d", "richeditd2dpt":
		return true
	default:
		return false
	}
}
