package platform

import (
	"errors"
	"fmt"
	"testing"
)

func TestDiagnosticBoundaryPreservesSafeNativeCauseOnly(test *testing.T) {
	privateFailure := errors.New("provider payload or source text")
	if DiagnosticMessage(privateFailure) != "" || DiagnosticMessage(nil) != "" {
		test.Fatal("unvetted error entered shortcut diagnostics")
	}
	nativeFailure := errors.New("UI Automation requires one selection range")
	wrapped := describeNativeError(nativeFailure)
	if !errors.Is(wrapped, nativeFailure) || DiagnosticMessage(fmt.Errorf("capture failed: %w", wrapped)) != nativeFailure.Error() {
		test.Fatal("safe explanation or error identity was lost")
	}
	if describeNativeError(nil) != nil || describeNativeError(wrapped) != wrapped {
		test.Fatal("diagnostic wrapper changed success or repeatedly wrapped errors")
	}
}

func TestNativeOperationRetainsPhaseWithoutExposingOuterPayload(test *testing.T) {
	privateFailure := errors.New("private provider payload")
	if actual := describeNativeOperation("before clipboard write", privateFailure); actual != privateFailure || DiagnosticMessage(actual) != "" {
		test.Fatal("adding a phase trusted an unvetted error")
	}
	if describeNativeOperation("before clipboard write", nil) != nil {
		test.Fatal("successful operation acquired a diagnostic")
	}
	nativeFailure := errors.New("clipboard changed; paste cancelled")
	outerFailure := fmt.Errorf("private source text: %w", describeNativeError(nativeFailure))
	actual := describeNativeOperation("before clipboard write", outerFailure)
	if !errors.Is(actual, nativeFailure) || !errors.Is(actual, outerFailure) {
		test.Fatal("operation label discarded the native cause")
	}
	expected := "before clipboard write: clipboard changed; paste cancelled"
	if actual.Error() != expected || DiagnosticMessage(actual) != expected {
		test.Fatal("operation diagnostic lost its phase or exposed unvetted text")
	}
}
