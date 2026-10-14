//go:build windows && (amd64 || arm64)

package platform

import (
	"errors"
	"testing"
)

func TestAccessibilityTargetRoutingRetainsNativeRefusals(test *testing.T) {
	for _, scenario := range []struct {
		name               string
		focusError         error
		accessibilityError error
		wantNative         bool
		wantAccessibility  bool
		wantTarget         string
		wantPassword       bool
	}{
		{name: "native edit", wantNative: true, wantTarget: "native-target"},
		{name: "custom editor", focusError: errUnsupportedWindowsTextControl, wantAccessibility: true, wantTarget: "uia:target"},
		{name: "native password", focusError: errPasswordTarget, wantPassword: true},
		{name: "inaccessible focus", focusError: errors.New("focus unavailable")},
		{name: "UIA password", focusError: errUnsupportedWindowsTextControl, accessibilityError: errPasswordTarget, wantAccessibility: true, wantPassword: true},
		{name: "UIA unavailable permits clipboard only", focusError: errUnsupportedWindowsTextControl, accessibilityError: ErrUnsupported, wantAccessibility: true},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			nativeRead, accessibilityRead := false, false
			target, err := readWindowsPasteTarget(windowsTextFocus{foreground: 91}, scenario.focusError, func() (string, error) {
				nativeRead = true
				return "native-target", nil
			}, func(foreground uintptr) (string, error) {
				accessibilityRead = true
				if foreground != 91 {
					test.Fatal("accessibility inspection changed target window")
				}
				return "uia:target", scenario.accessibilityError
			})
			if nativeRead != scenario.wantNative || accessibilityRead != scenario.wantAccessibility || target != scenario.wantTarget || errors.Is(err, errPasswordTarget) != scenario.wantPassword {
				test.Fatalf("incorrect adapter routing or password handling: target=%q error=%v", target, err)
			}
			expectedError := scenario.focusError
			if scenario.wantAccessibility {
				expectedError = scenario.accessibilityError
			}
			if expectedError != nil && !errors.Is(err, expectedError) {
				test.Fatal("target inspection discarded the original diagnostic cause")
			}
		})
	}
}

func TestPasteRevalidatesUsingTheOriginalCaptureAdapter(test *testing.T) {
	for _, target := range []string{"native:input", "uia:rich-edit-range"} {
		nativeCalls, accessibilityCalls := 0, 0
		actual, err := inspectWindowsTargetWithReader(target, func() (string, error) {
			nativeCalls++
			return "native:input", nil
		}, func() (string, error) {
			accessibilityCalls++
			return "uia:rich-edit-range", nil
		})
		if err != nil || actual != target || nativeCalls+accessibilityCalls != 1 {
			test.Fatal("paste validation changed the capture adapter")
		}
	}
}
