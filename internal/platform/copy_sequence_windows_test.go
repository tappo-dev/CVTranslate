//go:build windows && (amd64 || arm64)

package platform

import (
	"testing"
	"unsafe"
)

func TestCopyListenerNativeABILayout(test *testing.T) {
	message := copyThreadMessage{}
	event := copyKeyboardEvent{}
	if unsafe.Sizeof(message) != 48 || unsafe.Offsetof(message.Parameter) != 16 || unsafe.Offsetof(message.Data) != 24 {
		test.Fatal("incorrect Win32 MSG layout")
	}
	if unsafe.Sizeof(event) != 24 || unsafe.Offsetof(event.ExtraInfo) != 16 {
		test.Fatal("incorrect KBDLLHOOKSTRUCT layout")
	}
}

func TestCopyListenerFiltersInjectedKeysAndUnrelatedInput(test *testing.T) {
	for _, scenario := range []string{"injected press", "injected release", "other key", "control release", "physical release"} {
		test.Run(scenario, func(test *testing.T) {
			listener := &windowsCopySequence{activeGeneration: 1}
			listener.generation.Store(1)
			listener.sequence.pressCount = 3
			listener.sequence.copyHeld = true
			message := uintptr(messageKeyDown)
			event := copyKeyboardEvent{VirtualKey: virtualKeyCopy}
			expectedCount, expectedHeld := 0, true
			switch scenario {
			case "injected press":
				event.Flags = 0x10
			case "injected release":
				event.Flags = 0x02
				message = messageKeyUp
			case "other key":
				event.VirtualKey = 0x41
			case "control release":
				event.VirtualKey = 0xA2
				message = messageKeyUp
				expectedCount = 3
			case "physical release":
				message = messageKeyUp
				expectedCount, expectedHeld = 3, false
			}
			// These branches never query OS input state or touch the clipboard.
			listener.observeKey(message, event)
			if listener.sequence.pressCount != expectedCount || listener.sequence.copyHeld != expectedHeld {
				test.Fatalf("unexpected filtered state: %+v", listener.sequence)
			}
		})
	}
}

func TestCopyListenerActivationDiscardsPartialGestures(test *testing.T) {
	listener := &windowsCopySequence{}
	listener.observeKey(messageKeyDown, copyKeyboardEvent{VirtualKey: virtualKeyCopy})
	if listener.sequence.pressCount != 0 || !listener.sequence.copyHeld {
		test.Fatal("inactive listener counted a key")
	}
	listener.activate(1)
	listener.sequence.pressCount = 3
	if !listener.synchronizeGeneration() || listener.sequence.pressCount != 0 || !listener.sequence.copyHeld {
		test.Fatal("activation retained a previous partial gesture or lost held-key state")
	}
	listener.sequence.pressCount = 3
	listener.activate(2)
	if !listener.synchronizeGeneration() || listener.sequence.pressCount != 0 {
		test.Fatal("reconfiguration retained a previous partial gesture")
	}
	listener.activate(0)
	if listener.synchronizeGeneration() {
		test.Fatal("deactivated listener remained active")
	}
}
