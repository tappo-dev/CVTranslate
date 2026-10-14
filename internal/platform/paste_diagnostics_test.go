package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type pasteDiagnosticNative struct {
	mockNative
	failureStage string
	failure      error
}

func (native *pasteDiagnosticNative) validate(target string, version uint64) error {
	if native.failureStage == "validation" {
		return native.failure
	}
	return native.mockNative.validate(target, version)
}

func (native *pasteDiagnosticNative) write(version uint64, text string) (uint64, error) {
	if native.failureStage == "publication" {
		return 0, native.failure
	}
	return native.mockNative.write(version, text)
}

func (native *pasteDiagnosticNative) dispatch(ctx context.Context, target string, version uint64) error {
	if native.failureStage == "dispatch" {
		return native.failure
	}
	return native.mockNative.dispatch(ctx, target, version)
}

func TestPasteDiagnosticsDistinguishPrewriteAndPostwriteFailure(test *testing.T) {
	for _, scenario := range []struct {
		failureStage string
		wantPhase    string
		wantWrites   int
	}{
		{"validation", "target/clipboard validation before clipboard write", 0},
		{"publication", "publishing translation to clipboard", 0},
		{"dispatch", "paste dispatch after clipboard write", 1},
	} {
		test.Run(scenario.failureStage, func(test *testing.T) {
			failure := describeNativeError(errors.New("clipboard changed; paste cancelled"))
			native := &pasteDiagnosticNative{
				mockNative:   mockNative{text: "source", version: 7, target: "target", released: true},
				failureStage: scenario.failureStage,
				failure:      failure,
			}
			desktop := &Desktop{native: native}
			capture, err := desktop.CaptureClipboard()
			if err != nil {
				test.Fatal(err)
			}
			err = desktop.Paste(context.Background(), capture, "translation")
			if !errors.Is(err, failure) || !strings.HasPrefix(DiagnosticMessage(err), scenario.wantPhase+": ") {
				test.Fatalf("paste failure lost its execution phase: %v", err)
			}
			if native.writes != scenario.wantWrites || native.dispatches != 0 {
				test.Fatal("reporting the failure changed clipboard or dispatch behavior")
			}
			if scenario.wantWrites == 1 && native.text != "translation" {
				test.Fatal("late failure restored the previous clipboard")
			}
		})
	}
}
