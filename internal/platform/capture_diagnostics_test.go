package platform

import (
	"context"
	"errors"
	"testing"

	"cvtranslate/internal/domain"
)

func TestCapturePasteFailureIsBoundToItsSource(test *testing.T) {
	failure := describeNativeError(errors.New("UI Automation target inspection: TextPattern unavailable"))
	native := &mockNative{text: "copied source", version: 7, pasteError: failure, released: true}
	desktop := mockDesktop(native)
	capture, err := desktop.CaptureClipboard()
	if err != nil || capture.Text != "copied source" || capture.owner != desktop {
		test.Fatal("paste-only diagnostic broke source capture")
	}
	// A later source cannot replace the failed capture's explanation or make it
	// eligible for paste. Diagnostics belong to captures, not mutable globals.
	native.pasteError = nil
	native.target = "later-target"
	if _, err := desktop.CaptureClipboard(); err != nil {
		test.Fatal(err)
	}
	if err := desktop.Paste(context.Background(), capture, "translated"); !errors.Is(err, failure) || DiagnosticMessage(err) != failure.Error() {
		test.Fatal("original paste failure was lost")
	}
	if native.writes != 0 || native.dispatches != 0 || native.validateCalls != 0 {
		test.Fatal("failed capture reacquired a target or modified clipboard")
	}
}

func TestFourCopyPastePreservesPasteFailureWithoutWeakeningSnapshot(test *testing.T) {
	requireDesktopCopyShortcuts(test)
	failure := describeNativeError(errors.New("UI Automation target inspection timed out"))
	native := &mockNative{text: "copied source", version: 7, copyForeground: 11, pasteError: failure}
	desktop := mockDesktop(native)
	desktop.fixedClipboardShortcuts = true
	listener := &mockCopyBinding{}
	desktop.registerSequence = func(func(copyTrigger, uint64)) (copySequenceBinding, error) { return listener, nil }
	var event ShortcutEvent
	if err := desktop.ConfigureHotkeys(domain.Hotkeys{}, func(captured ShortcutEvent) { event = captured }); err != nil {
		test.Fatal(err)
	}
	defer desktop.Close()
	desktop.invokeCopySequence(confirmedCopy(4), listener.generation)
	if event.CaptureError != nil || event.Capture == nil || event.Capture.Text != "copied source" || !errors.Is(event.Capture.pasteError, failure) {
		test.Fatal("repeat handoff discarded source or its original paste-only failure")
	}
	if err := desktop.Paste(context.Background(), *event.Capture, "translated"); !errors.Is(err, failure) {
		test.Fatal("repeat paste did not report original refusal")
	}
	native.version++
	desktop.invokeCopySequence(confirmedCopy(4), listener.generation)
	if event.CaptureError == nil || event.Capture != nil {
		test.Fatal("paste-only diagnostic bypassed stale clipboard rejection")
	}
	native.version = 7
	native.copyForeground++
	desktop.invokeCopySequence(confirmedCopy(4), listener.generation)
	if event.CaptureError == nil || event.Capture != nil {
		test.Fatal("paste-only diagnostic bypassed foreground rejection")
	}
	if native.writes != 0 || native.dispatches != 0 {
		test.Fatal("unverified repeat target received input")
	}
}
