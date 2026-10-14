//go:build windows && (amd64 || arm64)

package platform

import (
	"errors"
	"strings"
	"testing"
)

// These comparisons use synthetic positions only, with the documented UIA
// ordering. No COM activation, real window inspection or text access occurs.
func TestAccessibilityRangeBoundsPreserveContainmentAndIdentifyRejectedEndpoint(test *testing.T) {
	for _, scenario := range []struct {
		name            string
		selection       [2]int32
		wantReason      string
		wantComparisons int
	}{
		{"inside", [2]int32{12, 18}, "", 3},
		{"whole document", [2]int32{10, 20}, "", 3},
		{"caret at start", [2]int32{10, 10}, "", 3},
		{"caret at end", [2]int32{20, 20}, "", 3},
		{"starts outside", [2]int32{9, 18}, "selection starts before", 1},
		{"reversed", [2]int32{18, 12}, "selection start is after", 2},
		{"ends outside", [2]int32{12, 21}, "selection ends after", 3},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			document, selection := &accessibilityObject{}, &accessibilityObject{}
			positions := map[*accessibilityObject][2]int32{document: {10, 20}, selection: scenario.selection}
			comparisons := 0
			err := validateAccessibilityRangeBounds(document, selection, func(source *accessibilityObject, sourceEndpoint uintptr, target *accessibilityObject, targetEndpoint uintptr) (int32, error) {
				comparisons++
				return positions[source][sourceEndpoint] - positions[target][targetEndpoint], nil
			})
			if comparisons != scenario.wantComparisons {
				test.Fatal("range validation skipped a boundary or continued after refusal")
			}
			if scenario.wantReason == "" {
				if err != nil {
					test.Fatalf("in-bounds selection or caret rejected: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), scenario.wantReason) {
				test.Fatalf("invalid range lost the failed boundary: %v", err)
			}
		})
	}
}

func TestAccessibilityRangeComparisonFailureRetainsCauseAndBoundary(test *testing.T) {
	failure := accessibilityCOMError(0x80131505)
	for failureAt, expectedBoundary := range []string{"document start versus selection start", "selection start versus selection end", "selection end versus document end"} {
		test.Run(expectedBoundary, func(test *testing.T) {
			comparisons := 0
			err := validateAccessibilityRangeBounds(&accessibilityObject{}, &accessibilityObject{}, func(*accessibilityObject, uintptr, *accessibilityObject, uintptr) (int32, error) {
				comparisons++
				if comparisons == failureAt+1 {
					return 0, failure
				}
				return -1, nil
			})
			if !errors.Is(err, failure) || !strings.Contains(err.Error(), expectedBoundary) || comparisons != failureAt+1 {
				test.Fatalf("failed comparison became an accepted range or lost its context: %v", err)
			}
		})
	}
}
