package platform

import (
	"context"
	"errors"
	"testing"

	"cvtranslate/internal/domain"
)

type mockNative struct {
	text           string
	target         string
	version        uint64
	released       bool
	captureError   error
	pasteError     error
	writes         int
	dispatches     int
	validateCalls  int
	onValidate     func()
	onWrite        func()
	closeCalls     int
	copyForeground uintptr
}

func (native *mockNative) status() Status {
	return Status{HotkeysAvailable: true, PasteAvailable: true}
}
func (native *mockNative) capture() (ClipboardCapture, error) {
	return ClipboardCapture{Text: native.text, version: native.version, target: native.target, pasteError: native.pasteError}, native.captureError
}
func (native *mockNative) captureCopy(trigger copyTrigger) (ClipboardCapture, error) {
	return captureClipboardCopyAtSnapshot(trigger, func() uintptr { return native.copyForeground }, native.capture)
}
func (native *mockNative) validate(target string, version uint64) error {
	native.validateCalls++
	if native.onValidate != nil {
		native.onValidate()
	}
	if target != native.target || version != native.version {
		return errors.New("target or clipboard changed")
	}
	return nil
}
func (native *mockNative) modifiersReleased() bool { return native.released }
func (native *mockNative) write(version uint64, text string) (uint64, error) {
	if version != native.version {
		return 0, errors.New("clipboard changed before write")
	}
	native.writes++
	native.version++
	native.text = text
	if native.onWrite != nil {
		native.onWrite()
	}
	return native.version, nil
}
func (native *mockNative) dispatch(ctx context.Context, target string, version uint64) error {
	if err := native.validate(target, version); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	native.dispatches++
	return nil
}
func (native *mockNative) close() error { native.closeCalls++; return nil }

type mockBinding struct {
	closed     bool
	closeError error
}

func (registered *mockBinding) close() error {
	if registered.closeError != nil {
		return registered.closeError
	}
	registered.closed = true
	return nil
}
func mockDesktop(native *mockNative) *Desktop {
	return &Desktop{native: native, bindings: make(map[string]registration)}
}

func TestNormalizeHotkeys(t *testing.T) {
	first, err := normalizeHotkey(" shift + Control + a ")
	if err != nil || first.name != "CTRL+SHIFT+A" {
		t.Fatalf("normalization = %+v, %v", first, err)
	}
	second, err := normalizeHotkey("CTRL+SHIFT+A")
	if err != nil || first.name != second.name {
		t.Fatal("equivalent shortcuts differ")
	}
	space, err := normalizeHotkey("Ctrl+Space")
	if err != nil || space.name != "CTRL+SPACE" || space.key != "SPACE" {
		t.Fatalf("Ctrl+Space normalization = %+v, %v", space, err)
	}
	for _, invalid := range []string{"A", "CTRL+", "CTRL+CTRL+A", "CTRL++A", "HYPER+A", "CTRL+F13"} {
		if _, err := normalizeHotkey(invalid); err == nil {
			t.Errorf("accepted invalid shortcut %q", invalid)
		}
	}
	blank, err := normalizeHotkey("  ")
	if err != nil || blank.name != "" {
		t.Fatal("blank must disable shortcut")
	}
}

func TestHotkeyConflictRollsBackAdditionsAndRetainsCallback(t *testing.T) {
	desktop := mockDesktop(&mockNative{})
	var bindings []*mockBinding
	desktop.register = func(key combination, _ func()) (binding, error) {
		if key.name == "CTRL+B" {
			return nil, errors.New("combination reserved by another application")
		}
		registered := &mockBinding{}
		bindings = append(bindings, registered)
		return registered, nil
	}
	action := ""
	if err := desktop.ConfigureHotkeys(domain.Hotkeys{OpenUI: "CTRL+A"}, func(event ShortcutEvent) { action = event.Action }); err != nil {
		t.Fatal(err)
	}
	err := desktop.ConfigureHotkeys(domain.Hotkeys{OpenUI: "CTRL+B"}, func(ShortcutEvent) { t.Fatal("replacement callback became active") })
	if err == nil {
		t.Fatal("expected registration conflict")
	}
	if bindings[0].closed {
		t.Fatal("rollback did not retain original binding")
	}
	if len(desktop.bindings) != 1 {
		t.Fatal("rollback changed active configuration")
	}
	desktop.invoke("CTRL+A")
	if action != "open-ui" {
		t.Fatalf("old callback = %q", action)
	}
}

func TestHotkeyReplacementAndBlankDisable(t *testing.T) {
	desktop := mockDesktop(&mockNative{})
	registrations := 0
	desktop.register = func(combination, func()) (binding, error) { registrations++; return &mockBinding{}, nil }
	if err := desktop.ConfigureHotkeys(domain.Hotkeys{OpenUI: "CTRL+A"}, nil); err != nil {
		t.Fatal(err)
	}
	action := ""
	previous := desktop.bindings["CTRL+A"].binding.(*mockBinding)
	if err := desktop.ConfigureHotkeys(domain.Hotkeys{OpenUI: "CTRL+B"}, func(event ShortcutEvent) { action = event.Action }); err != nil {
		t.Fatal(err)
	}
	desktop.invoke("CTRL+B")
	if registrations != 2 || action != "open-ui" || !previous.closed {
		t.Fatal("replacement did not activate the new open-WebUI shortcut")
	}
	current := desktop.bindings["CTRL+B"].binding.(*mockBinding)
	if err := desktop.ConfigureHotkeys(domain.Hotkeys{}, nil); err != nil {
		t.Fatal(err)
	}
	if len(desktop.bindings) != 0 || !current.closed {
		t.Fatal("blank shortcuts did not unregister")
	}
}

func TestHotkeyRemovalFailureRestoresOldBindingsAndCallback(t *testing.T) {
	desktop := mockDesktop(&mockNative{})
	created := make(map[string][]*mockBinding)
	desktop.register = func(key combination, callback func()) (binding, error) {
		registered := &mockBinding{}
		created[key.name] = append(created[key.name], registered)
		return registered, nil
	}
	var actions []string
	if err := desktop.ConfigureHotkeys(domain.Hotkeys{OpenUI: "CTRL+A"}, func(event ShortcutEvent) { actions = append(actions, event.Action) }); err != nil {
		t.Fatal(err)
	}
	created["CTRL+A"][0].closeError = errors.New("unregister failed")
	err := desktop.ConfigureHotkeys(domain.Hotkeys{OpenUI: "CTRL+B"}, func(ShortcutEvent) { t.Fatal("failed configuration activated new callback") })
	if err == nil || errors.Is(err, ErrHotkeysDegraded) {
		t.Fatalf("expected removal error with successful rollback: %v", err)
	}
	if created["CTRL+A"][0].closed || !created["CTRL+B"][0].closed {
		t.Fatal("failed old binding or staged addition has wrong lifetime")
	}
	if len(desktop.bindings) != 1 || !desktop.Status().HotkeysAvailable {
		t.Fatal("previous configuration not fully restored")
	}
	desktop.invoke("CTRL+A")
	desktop.invoke("CTRL+B")
	if len(actions) != 1 || actions[0] != "open-ui" {
		t.Fatalf("rollback actions = %v", actions)
	}
}

func TestPasteGuards(t *testing.T) {
	for _, scenario := range []string{"clipboard changed", "target changed", "cancelled", "cancel during write", "unknown target", "foreign capture", "closed", "successful"} {
		t.Run(scenario, func(t *testing.T) {
			native := &mockNative{text: "source", target: "input-1", version: 7, released: true}
			desktop := mockDesktop(native)
			capture, err := desktop.CaptureClipboard()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch scenario {
			case "clipboard changed":
				native.version++
			case "target changed":
				native.target = "input-2"
			case "cancelled":
				cancel()
			case "cancel during write":
				native.onWrite = cancel
			case "unknown target":
				capture.target = ""
			case "foreign capture":
				capture.owner = mockDesktop(native)
			case "closed":
				if err := desktop.Close(); err != nil {
					t.Fatal(err)
				}
			}
			err = desktop.Paste(ctx, capture, "translated")
			if scenario == "successful" {
				if err != nil || native.writes != 1 || native.dispatches != 1 || native.text != "translated" {
					t.Fatalf("paste did not retain translated clipboard: %v", err)
				}
			} else {
				if err == nil || native.dispatches != 0 {
					t.Fatal("guard allowed dispatch")
				}
				if scenario != "cancel during write" && native.writes != 0 {
					t.Fatal("guard modified clipboard")
				}
			}
		})
	}
}

func TestClipboardChangeWhileWaitingForModifierRelease(t *testing.T) {
	native := &mockNative{text: "source", target: "input-1", version: 1, released: false}
	desktop := mockDesktop(native)
	capture, err := desktop.CaptureClipboard()
	if err != nil {
		t.Fatal(err)
	}
	native.onValidate = func() {
		if native.validateCalls == 2 {
			native.version++
		}
	}
	if err := desktop.Paste(context.Background(), capture, "translated"); err == nil {
		t.Fatal("clipboard change during modifier wait was ignored")
	}
	if native.writes != 0 || native.dispatches != 0 {
		t.Fatal("paste occurred while modifier was held")
	}
}

func TestFinalDispatchValidationRejectsLateChanges(t *testing.T) {
	for _, scenario := range []string{"target changed", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			native := &mockNative{text: "source", target: "same-window:original-fingerprint", version: 1, released: true}
			desktop := mockDesktop(native)
			capture, err := desktop.CaptureClipboard()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			native.onValidate = func() {
				if native.validateCalls == 2 {
					if scenario == "target changed" {
						native.target = "same-window:new-fingerprint"
					} else {
						cancel()
					}
				}
			}
			if err := desktop.Paste(ctx, capture, "translated"); err == nil {
				t.Fatal("late dispatch guard allowed paste")
			}
			if native.writes != 1 || native.dispatches != 0 || native.text != "translated" {
				t.Fatal("late rejection should retain translated clipboard without dispatch")
			}
		})
	}
}

func TestTextOnlyCaptureAndSensitiveRejection(t *testing.T) {
	native := &mockNative{text: "source", version: 1}
	desktop := mockDesktop(native)
	capture, err := desktop.CaptureClipboard()
	if err != nil || capture.Text != "source" {
		t.Fatal("text-only capture should work without an editable target")
	}
	if err := desktop.Paste(context.Background(), capture, "translated"); err == nil {
		t.Fatal("unknown input allowed paste")
	}
	native.captureError = errors.New("sensitive data")
	capture, err = desktop.CaptureClipboard()
	if err == nil || capture.Text != "" {
		t.Fatal("sensitive capture exposed text")
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	native := &mockNative{}
	desktop := mockDesktop(native)
	if err := desktop.Close(); err != nil {
		t.Fatal(err)
	}
	if err := desktop.Close(); err != nil {
		t.Fatal(err)
	}
	if native.closeCalls != 1 {
		t.Fatal("native state closed more than once")
	}
	if err := desktop.ConfigureHotkeys(domain.Hotkeys{}, nil); err == nil {
		t.Fatal("closed desktop accepted configuration")
	}
}

func TestBrowserRejectsUnsafeSchemesWithoutLaunching(t *testing.T) {
	for _, address := range []string{"file:///secret", "javascript:alert(1)", "--help", "http://localhost/\n", "https:///missing-host"} {
		if err := OpenBrowser(address); err == nil {
			t.Errorf("unsafe URL accepted: %q", address)
		}
	}
}
