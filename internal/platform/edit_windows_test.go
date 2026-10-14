//go:build windows && (amd64 || arm64)

package platform

import (
	"errors"
	"testing"
	"unicode/utf16"
)

type mockEdit struct {
	text           string
	selection      uintptr
	password       uintptr
	failedMessage  uintptr
	reportedLength *uintptr
	textReads      int
	onRead         func()
	buffers        [][]uint16
}

func (edit *mockEdit) read(message uintptr, buffer []uint16) (uintptr, error) {
	if message == edit.failedMessage {
		return 0, errors.New("message timeout or access denied")
	}
	switch message {
	case messageGetPasswordCharacter:
		return edit.password, nil
	case messageGetTextLength:
		if edit.reportedLength != nil {
			return *edit.reportedLength, nil
		}
		return uintptr(len(utf16.Encode([]rune(edit.text)))), nil
	case messageGetSelection:
		return edit.selection, nil
	case messageGetText:
		edit.textReads++
		if edit.onRead != nil {
			edit.onRead()
		}
		encoded := utf16.Encode([]rune(edit.text))
		copied := copy(buffer[:len(buffer)-1], encoded)
		edit.buffers = append(edit.buffers, buffer)
		return uintptr(copied), nil
	default:
		return 0, errors.New("unexpected native message")
	}
}

func TestEditFingerprintDetectsSameLengthEditsAndCaretMovement(t *testing.T) {
	edit := &mockEdit{text: "draft", selection: 2 | 2<<16}
	original, err := readEditFingerprint(edit.read)
	if err != nil {
		t.Fatal(err)
	}
	unchanged, err := readEditFingerprint(edit.read)
	if err != nil || unchanged != original {
		t.Fatal("unchanged target should match")
	}
	edit.text = "craft"
	typed, err := readEditFingerprint(edit.read)
	if err != nil || typed == original {
		t.Fatal("same-length typing was not detected")
	}
	edit.text = "draft"
	edit.selection = 3 | 3<<16
	moved, err := readEditFingerprint(edit.read)
	if err != nil || moved == original {
		t.Fatal("caret movement was not detected")
	}
	edit.selection = 1 | 4<<16
	selected, err := readEditFingerprint(edit.read)
	if err != nil || selected == original {
		t.Fatal("selection change was not detected")
	}
	for _, buffer := range edit.buffers {
		for _, character := range buffer {
			if character != 0 {
				t.Fatal("target text retained in temporary buffer")
			}
		}
	}
}

func TestEditFingerprintRejectsUnreliableTargets(t *testing.T) {
	oversized := uintptr(maximumTargetCharacters + 1)
	for _, scenario := range []struct {
		name string
		edit mockEdit
	}{
		{"password", mockEdit{text: "secret", password: '*'}},
		{"timeout", mockEdit{text: "draft", failedMessage: messageGetTextLength}},
		{"text denied", mockEdit{text: "draft", failedMessage: messageGetText}},
		{"selection denied", mockEdit{text: "draft", failedMessage: messageGetSelection}},
		{"selection overflow", mockEdit{text: "draft", selection: 0xffffffff}},
		{"selection outside text", mockEdit{text: "draft", selection: 6 | 6<<16}},
		{"oversized", mockEdit{text: "draft", reportedLength: &oversized}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			_, err := readEditFingerprint(scenario.edit.read)
			if err == nil {
				t.Fatal("unreliable target accepted")
			}
			if scenario.name == "password" && (!errors.Is(err, errPasswordTarget) || scenario.edit.textReads != 0) {
				t.Fatal("password target was read or lost privacy error")
			}
		})
	}
}

func TestEditFingerprintRejectsConcurrentTextChanges(t *testing.T) {
	edit := &mockEdit{text: "draft"}
	edit.onRead = func() {
		if edit.textReads == 2 {
			edit.text = "craft"
		}
	}
	if _, err := readEditFingerprint(edit.read); err == nil {
		t.Fatal("unstable text snapshots accepted")
	}
	edit = &mockEdit{text: "draft"}
	edit.onRead = func() { edit.text = "x" }
	if _, err := readEditFingerprint(edit.read); err == nil {
		t.Fatal("truncated snapshot accepted")
	}
}

func TestSupportedEditClassesExcludeCustomEditors(t *testing.T) {
	for _, class := range []string{"edit", "richedit20w", "richedit50w", "richedit60w", "richeditd2d", "richeditd2dpt"} {
		if !supportedEditClass(class) {
			t.Errorf("missing native edit class %s", class)
		}
	}
	for _, class := range []string{"chrome_renderwidgethosthwnd", "richedit-custom", "richedit20a", "terminal"} {
		if supportedEditClass(class) {
			t.Errorf("accepted unsupported editor class %s", class)
		}
	}
}
