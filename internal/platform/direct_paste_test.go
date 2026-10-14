package platform

import (
	"context"
	"errors"
	"testing"

	"cvtranslate/internal/domain"
)

type mockDirectPasteNative struct {
	mockNative
	directDispatches int
	onDirectDispatch func()
	onClipboardCheck func()
}

func (native *mockDirectPasteNative) validateClipboard(version uint64) error {
	if native.onClipboardCheck != nil {
		native.onClipboardCheck()
	}
	if version == 0 || version != native.version {
		return errors.New("clipboard changed")
	}
	return nil
}

func (native *mockDirectPasteNative) dispatchDirect(ctx context.Context, version uint64) error {
	if native.onDirectDispatch != nil {
		native.onDirectDispatch()
	}
	if err := native.validateClipboard(version); err != nil {
		return err
	}
	if !native.modifiersReleased() {
		return errors.New("modifier pressed before dispatch")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	native.directDispatches++
	return nil
}

func newDirectPasteTestDesktop(test *testing.T) (*Desktop, *mockDirectPasteNative, *mockCopyBinding) {
	test.Helper()
	requireDesktopCopyShortcuts(test)
	native := &mockDirectPasteNative{mockNative: mockNative{text: "source", version: 7, copyForeground: 11, released: true}}
	listener := &mockCopyBinding{}
	desktop := &Desktop{
		native: native, bindings: make(map[string]registration),
		registerSequence:        func(func(copyTrigger, uint64)) (copySequenceBinding, error) { return listener, nil },
		fixedClipboardShortcuts: true,
	}
	test.Cleanup(func() { _ = desktop.Close() })
	return desktop, native, listener
}

func captureThreeCopyPaste(test *testing.T, desktop *Desktop, listener *mockCopyBinding) ClipboardCapture {
	test.Helper()
	var event ShortcutEvent
	if err := desktop.ConfigureHotkeys(domain.Hotkeys{}, func(value ShortcutEvent) { event = value }); err != nil {
		test.Fatal(err)
	}
	desktop.invokeCopySequence(confirmedCopy(3), listener.generation)
	if event.CaptureError != nil || event.Capture == nil || !event.Capture.UsesDirectPaste() {
		test.Fatalf("three-copy paste did not capture experimental mode: %v", event.CaptureError)
	}
	return *event.Capture
}

func TestThreeCopyPasteIgnoresMissingAndChangedTargets(test *testing.T) {
	for _, target := range []string{"", "original-editor"} {
		test.Run(target, func(test *testing.T) {
			desktop, native, listener := newDirectPasteTestDesktop(test)
			native.target = target
			if target == "" {
				native.pasteError = describeNativeError(errors.New("target is not positively editable"))
			}
			capture := captureThreeCopyPaste(test, desktop, listener)
			native.target = "different-window-and-caret"
			native.copyForeground++
			if err := desktop.Paste(context.Background(), capture, "translation"); err != nil {
				test.Fatalf("direct paste still depends on original target: %v", err)
			}
			if native.validateCalls != 0 || native.dispatches != 0 || native.directDispatches != 1 || native.writes != 1 || native.text != "translation" {
				test.Fatal("direct paste inspected the target or failed to write and dispatch once")
			}
		})
	}
}

func TestDirectPasteRetainsNonTargetGuards(test *testing.T) {
	for _, scenario := range []string{"foreign capture", "closed", "clipboard changed", "cancelled", "cancel during write", "clipboard changed before dispatch", "modifier pressed before dispatch", "cancel while modifier held"} {
		test.Run(scenario, func(test *testing.T) {
			desktop, native, listener := newDirectPasteTestDesktop(test)
			capture := captureThreeCopyPaste(test, desktop, listener)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			wantWrites := 0
			switch scenario {
			case "foreign capture":
				capture.owner = &Desktop{}
			case "closed":
				_ = desktop.Close()
			case "clipboard changed":
				native.version++
			case "cancelled":
				cancel()
			case "cancel during write":
				native.onWrite = cancel
				wantWrites = 1
			case "clipboard changed before dispatch":
				native.onDirectDispatch = func() { native.version++ }
				wantWrites = 1
			case "modifier pressed before dispatch":
				native.onDirectDispatch = func() { native.released = false }
				wantWrites = 1
			case "cancel while modifier held":
				native.released = false
				native.onClipboardCheck = cancel
			}
			if err := desktop.Paste(ctx, capture, "translation"); err == nil {
				test.Fatal("non-target guard was bypassed")
			}
			if native.writes != wantWrites || native.directDispatches != 0 || native.dispatches != 0 || native.validateCalls != 0 {
				test.Fatal("rejected direct paste changed input or invoked a target check")
			}
		})
	}
}

func TestDirectPasteModeIsLimitedToThreeCopyClipboardPaste(test *testing.T) {
	desktop, native, listener := newDirectPasteTestDesktop(test)
	captureThreeCopyPaste(test, desktop, listener)
	var event ShortcutEvent
	if err := desktop.ConfigureHotkeys(domain.Hotkeys{}, func(value ShortcutEvent) { event = value }); err != nil {
		test.Fatal(err)
	}
	desktop.invokeCopySequence(confirmedCopy(4), listener.generation)
	if event.Action != "translate-ui" || event.CaptureError != nil || event.Capture == nil || event.Capture.UsesDirectPaste() {
		test.Fatal("fixed four-copy WebUI action inherited direct paste mode")
	}
	ordinaryCapture, err := desktop.CaptureClipboard()
	if err != nil || ordinaryCapture.UsesDirectPaste() {
		test.Fatal("ordinary clipboard capture inherited direct paste mode")
	}
	if err := desktop.Paste(context.Background(), ordinaryCapture, "translation"); err == nil || native.directDispatches != 0 {
		test.Fatal("ordinary paste accepted the missing original target")
	}
}

func TestThreeCopyDirectPasteDoesNotBypassSourceRefusal(test *testing.T) {
	for _, scenario := range []string{"password", "sensitive clipboard", "no completed copy", "stale clipboard"} {
		test.Run(scenario, func(test *testing.T) {
			desktop, native, listener := newDirectPasteTestDesktop(test)
			trigger := confirmedCopy(3)
			switch scenario {
			case "password":
				native.captureError = errors.New("password target; capture refused")
			case "sensitive clipboard":
				native.captureError = errors.New("clipboard marked sensitive")
			case "no completed copy":
				trigger.clipboardUpdated = false
			case "stale clipboard":
				native.version++
			}
			var event ShortcutEvent
			if err := desktop.ConfigureHotkeys(domain.Hotkeys{}, func(value ShortcutEvent) { event = value }); err != nil {
				test.Fatal(err)
			}
			desktop.invokeCopySequence(trigger, listener.generation)
			if event.CaptureError == nil || event.Capture != nil {
				test.Fatal("source refusal was downgraded to a paste-only failure")
			}
		})
	}
}
