//go:build windows && (amd64 || arm64)

package platform

import (
	"errors"
	"strings"
	"testing"
)

func TestClipboardCaptureRetainsPasteInspectionFailure(test *testing.T) {
	initialFailure := errors.New("UI Automation focus belongs to a different process")
	confirmationFailure := errors.New("UI Automation inspection budget expired")
	for _, scenario := range []struct {
		name              string
		initialTarget     string
		confirmedTarget   string
		initialError      error
		confirmationError error
		wantError         error
		wantPhase         string
	}{
		{name: "valid", initialTarget: "uia:stable", confirmedTarget: "uia:stable"},
		{name: "initial refusal then recovery", initialError: initialFailure, confirmedTarget: "uia:recovered", wantError: initialFailure, wantPhase: "initial inspection"},
		{name: "two different refusals", initialError: initialFailure, confirmationError: confirmationFailure, wantError: initialFailure, wantPhase: "initial inspection"},
		{name: "confirmation timeout", initialTarget: "uia:stable", confirmationError: confirmationFailure, wantError: confirmationFailure, wantPhase: "confirmation"},
		{name: "changed target", initialTarget: "uia:first", confirmedTarget: "uia:second", wantPhase: "changed while"},
		{name: "missing target", wantPhase: "no verified editable input"},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			inspections := 0
			capture, err := captureWindowsClipboardWithTarget(func() (string, error) {
				inspections++
				if inspections == 1 {
					return scenario.initialTarget, scenario.initialError
				}
				return scenario.confirmedTarget, scenario.confirmationError
			}, func() (string, uint64, error) { return "explicit clipboard text", 7, nil })
			if err != nil || capture.Text != "explicit clipboard text" || capture.version != 7 || inspections != 2 {
				test.Fatalf("paste-only refusal broke explicit text capture: %v", err)
			}
			if scenario.name == "valid" {
				if capture.target != "uia:stable" || capture.pasteError != nil {
					test.Fatal("valid target acquired a diagnostic failure")
				}
				return
			}
			if capture.target != "" || capture.pasteError == nil || !strings.Contains(DiagnosticMessage(capture.pasteError), scenario.wantPhase) {
				test.Fatal("failed target was authorized or lost its inspection phase")
			}
			if scenario.wantError != nil && !errors.Is(capture.pasteError, scenario.wantError) {
				test.Fatal("native cause was replaced")
			}
		})
	}
}

func TestClipboardDiagnosticNeverDowngradesPasswordOrSourceFailure(test *testing.T) {
	for _, scenario := range []string{"password before read", "password after read", "clipboard sensitive"} {
		test.Run(scenario, func(test *testing.T) {
			inspections, clipboardReads := 0, 0
			capture, err := captureWindowsClipboardWithTarget(func() (string, error) {
				inspections++
				if scenario == "password before read" || (scenario == "password after read" && inspections == 2) {
					return "", errPasswordTarget
				}
				return "", errors.New("UIA inspection unavailable")
			}, func() (string, uint64, error) {
				clipboardReads++
				if scenario == "clipboard sensitive" {
					return "never expose this", 7, errors.New("clipboard marked sensitive")
				}
				return "never expose this", 7, nil
			})
			if err == nil || capture != (ClipboardCapture{}) {
				test.Fatal("fatal privacy failure returned source or a recoverable paste failure")
			}
			if scenario == "password before read" && clipboardReads != 0 {
				test.Fatal("recognized password input did not stop clipboard read")
			}
			if scenario == "clipboard sensitive" && inspections != 1 {
				test.Fatal("source failure continued to native inspection")
			}
		})
	}
}
