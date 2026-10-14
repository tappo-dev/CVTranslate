package platform

import (
	"errors"
	"runtime"
	"testing"

	"cvtranslate/internal/domain"
)

type mockCopyBinding struct {
	mockBinding
	generation   uint64
	runtimeError error
}

func (registered *mockCopyBinding) activate(generation uint64) { registered.generation = generation }
func (registered *mockCopyBinding) failure() error             { return registered.runtimeError }

func confirmedCopy(repetitions int) copyTrigger {
	return copyTrigger{repetitions: repetitions, clipboardUpdated: true, foreground: 11, clipboardVersion: 7}
}

func requireDesktopCopyShortcuts(test *testing.T) {
	test.Helper()
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		test.Skip("repeated-copy configuration requires Windows or macOS")
	}
}

func TestNormalizeRepeatedCopyShortcut(test *testing.T) {
	for _, operatingSystem := range []string{"windows", "darwin", "linux"} {
		test.Run(operatingSystem, func(test *testing.T) {
			for _, value := range []string{"Ctrl+C*3", " control + c * 4 ", "Cmd+C*3", " command + c * 4 "} {
				combination, err := normalizeHotkeyForOS(value, operatingSystem)
				valid := (operatingSystem == "windows" && (value == "Ctrl+C*3" || value == " control + c * 4 ")) ||
					(operatingSystem == "darwin" && (value == "Cmd+C*3" || value == " command + c * 4 "))
				if !valid {
					if err == nil {
						test.Fatalf("wrong platform's repeated-copy shortcut accepted: %q", value)
					}
					continue
				}
				if err != nil || combination.key != "C" || combination.repetitions < 3 || combination.repetitions > 4 {
					test.Fatalf("repeated-copy shortcut not normalized: %+v %v", combination, err)
				}
			}
			for _, value := range []string{"Ctrl+C*2", "Ctrl+C*5", "Ctrl+C*3*4", "Ctrl+C*", "Ctrl+V*3", "Ctrl+Shift+C*3", "Cmd+C*2", "Cmd+V*3", "Cmd+Shift+C*3", "Cmd+C*5", "C*3"} {
				if _, err := normalizeHotkeyForOS(value, operatingSystem); err == nil {
					test.Fatalf("invalid repeated-copy shortcut accepted: %q", value)
				}
			}
		})
	}
}

func TestFixedCopyBindingsShareOneListener(test *testing.T) {
	requireDesktopCopyShortcuts(test)
	native := &mockNative{text: "copied text", version: 7, copyForeground: 11}
	desktop := mockDesktop(native)
	desktop.fixedClipboardShortcuts = true
	listener := &mockCopyBinding{}
	registrations := 0
	var deliver func(copyTrigger, uint64)
	desktop.registerSequence = func(callback func(copyTrigger, uint64)) (copySequenceBinding, error) {
		registrations++
		deliver = callback
		return listener, nil
	}
	desktop.register = func(combination, func()) (binding, error) { return &mockBinding{}, nil }
	var actions []string
	callback := func(event ShortcutEvent) {
		actions = append(actions, event.Action)
		if event.Capture == nil || event.Capture.Text != "copied text" || event.Capture.owner != desktop {
			test.Fatal("fixed repeated translation did not carry its bound clipboard capture")
		}
		if event.Capture.UsesDirectPaste() != (event.Action == "translate-paste") {
			test.Fatal("only the three-copy action should carry direct-paste policy")
		}
	}
	if err := desktop.ConfigureHotkeys(domain.Hotkeys{OpenUI: "Ctrl+Space"}, callback); err != nil {
		test.Fatal(err)
	}
	firstGeneration := listener.generation
	deliver(confirmedCopy(3), firstGeneration)
	deliver(confirmedCopy(4), firstGeneration)
	if registrations != 1 || len(actions) != 2 || actions[0] != "translate-paste" || actions[1] != "translate-ui" {
		test.Fatalf("fixed repeat counts dispatched incorrect actions: %v", actions)
	}
	if err := desktop.ConfigureHotkeys(domain.Hotkeys{OpenUI: "Ctrl+Alt+O"}, callback); err != nil {
		test.Fatal(err)
	}
	deliver(confirmedCopy(3), firstGeneration)
	if len(actions) != 2 || registrations != 1 {
		test.Fatal("stale generation fired or changing open-WebUI reinstalled the copy listener")
	}
	deliver(confirmedCopy(4), listener.generation)
	if actions[2] != "translate-ui" {
		test.Fatal("fixed four-copy action changed during reconfiguration")
	}
}

func TestFixedCopyBindingsRejectSingleCopyOpenShortcut(test *testing.T) {
	requireDesktopCopyShortcuts(test)
	desktop := mockDesktop(&mockNative{})
	desktop.fixedClipboardShortcuts = true
	desktop.register = func(combination, func()) (binding, error) {
		test.Fatal("conflicting configuration reached native registration")
		return nil, nil
	}
	desktop.registerSequence = func(func(copyTrigger, uint64)) (copySequenceBinding, error) {
		test.Fatal("conflicting configuration installed a copy listener")
		return nil, nil
	}
	for _, value := range []string{copyShortcutForOS(runtime.GOOS), copyShortcutForOS(runtime.GOOS) + "*3", copyShortcutForOS(runtime.GOOS) + "*4"} {
		if err := desktop.ConfigureHotkeys(domain.Hotkeys{OpenUI: value}, nil); err == nil {
			test.Fatalf("copy gesture was accepted as the open-WebUI shortcut: %s", value)
		}
	}
}

func TestFixedCopyBindingFailureRetainsPreviousConfiguration(test *testing.T) {
	requireDesktopCopyShortcuts(test)
	desktop := mockDesktop(&mockNative{})
	desktop.register = func(combination, func()) (binding, error) { return &mockBinding{}, nil }
	var action string
	if err := desktop.ConfigureHotkeys(domain.Hotkeys{OpenUI: "Ctrl+Space"}, func(event ShortcutEvent) { action = event.Action }); err != nil {
		test.Fatal(err)
	}
	desktop.fixedClipboardShortcuts = true
	desktop.registerSequence = func(func(copyTrigger, uint64)) (copySequenceBinding, error) {
		return nil, errors.New("hook unavailable")
	}
	if err := desktop.ConfigureHotkeys(domain.Hotkeys{OpenUI: "Ctrl+Alt+O"}, nil); err == nil {
		test.Fatal("copy listener failure was hidden")
	}
	desktop.invoke("CTRL+SPACE")
	if action != "open-ui" {
		test.Fatal("failed fixed-listener registration replaced the previous shortcut")
	}
}

func TestFixedCopyRegistrationFailureDoesNotAdvertiseWorkingShortcuts(test *testing.T) {
	requireDesktopCopyShortcuts(test)
	desktop := mockDesktop(&mockNative{})
	desktop.fixedClipboardShortcuts = true
	desktop.registerSequence = func(func(copyTrigger, uint64)) (copySequenceBinding, error) {
		return nil, errors.New("event tap unavailable")
	}
	if err := desktop.ConfigureHotkeys(domain.Hotkeys{}, nil); err == nil {
		test.Fatal("failed registration was accepted")
	}
	if desktop.Status().HotkeysAvailable {
		test.Fatal("native permission alone advertised an unregistered copy listener")
	}
	listener := &mockCopyBinding{}
	desktop.registerSequence = func(func(copyTrigger, uint64)) (copySequenceBinding, error) { return listener, nil }
	if err := desktop.ConfigureHotkeys(domain.Hotkeys{}, nil); err != nil {
		test.Fatal(err)
	}
	if !desktop.Status().HotkeysAvailable {
		test.Fatal("successful registration did not restore shortcut availability")
	}
}

func TestFixedCopyBindingsRejectStaleClipboardAndListenerFailure(test *testing.T) {
	requireDesktopCopyShortcuts(test)
	desktop := mockDesktop(&mockNative{})
	desktop.fixedClipboardShortcuts = true
	listener := &mockCopyBinding{}
	desktop.registerSequence = func(func(copyTrigger, uint64)) (copySequenceBinding, error) { return listener, nil }
	var events []ShortcutEvent
	if err := desktop.ConfigureHotkeys(domain.Hotkeys{}, func(event ShortcutEvent) { events = append(events, event) }); err != nil {
		test.Fatal(err)
	}
	desktop.invokeCopySequence(copyTrigger{repetitions: 3}, listener.generation)
	desktop.invokeCopySequence(copyTrigger{repetitions: 4}, listener.generation)
	if len(events) != 2 || events[0].CaptureError == nil || events[1].CaptureError == nil {
		test.Fatal("fixed clipboard actions translated stale clipboard contents")
	}
	listener.runtimeError = errors.New("message loop stopped")
	desktop.invokeCopySequence(confirmedCopy(4), listener.generation)
	if len(events) != 2 || desktop.Status().HotkeysAvailable {
		test.Fatal("listener failure was hidden or allowed a queued translation")
	}
}
