//go:build windows && (amd64 || arm64)

package platform

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestClipboardPublicationUsesPostCloseVersionWithoutAbsorbingLaterCopy(test *testing.T) {
	for _, changedAfterConfirmation := range []bool{false, true} {
		name := "system finalizes own write"
		if changedAfterConfirmation {
			name = "external copy after confirmation"
		}
		test.Run(name, func(test *testing.T) {
			version := uint64(7)
			locked := false
			lockCalls, writes, confirmations, dispatches := 0, 0, 0, 0
			withClipboard := func(operation func() error) error {
				lockCalls++
				locked = true
				err := operation()
				locked = false
				if lockCalls == 1 {
					version = 10 // CloseClipboard finalizes formats after SetClipboardData.
				} else if changedAfterConfirmation {
					version++
				}
				return err
			}
			writtenVersion, err := publishWindowsClipboardText(7, withClipboard, func() uint64 {
				if !locked {
					test.Fatal("receipt accepted an unlocked generation")
				}
				return version
			}, func() error {
				writes++
				version = 8
				return nil
			}, func() error {
				if !locked || lockCalls != 2 || version != 10 {
					test.Fatal("receipt checked before publication or without clipboard lock")
				}
				confirmations++
				return nil
			})
			if err != nil || writtenVersion != 10 || writes != 1 || confirmations != 1 || lockCalls != 2 {
				test.Fatalf("publication returned a pre-close generation or retried: %d, %v", writtenVersion, err)
			}
			err = dispatchWindowsPaste(context.Background(), func() error {
				if version != writtenVersion {
					return errors.New("clipboard changed")
				}
				return nil
			}, func() bool { return true }, func() error { dispatches++; return nil })
			if changedAfterConfirmation {
				if err == nil || dispatches != 0 {
					test.Fatal("late copy was treated as part of our write")
				}
			} else if err != nil || dispatches != 1 {
				test.Fatal("own close-time sequence update incorrectly blocked dispatch")
			}
		})
	}
}

func TestClipboardPublicationFailuresDoNotReturnUsableVersion(test *testing.T) {
	for _, scenario := range []string{"stale source", "missing source version", "write failure", "first close failure", "confirmation open failure", "receipt rejected", "missing published version", "changed during confirmation", "confirmation close failure"} {
		test.Run(scenario, func(test *testing.T) {
			failure := errors.New("native clipboard operation failed")
			version := uint64(7)
			if scenario == "stale source" {
				version++
			}
			expectedVersion := uint64(7)
			if scenario == "missing source version" {
				expectedVersion = 0
			}
			lockCalls, writes, confirmations := 0, 0, 0
			withClipboard := func(operation func() error) error {
				lockCalls++
				if lockCalls == 2 && scenario == "confirmation open failure" {
					return failure
				}
				err := operation()
				if (lockCalls == 1 && scenario == "first close failure") || (lockCalls == 2 && scenario == "confirmation close failure") {
					return errors.Join(err, failure)
				}
				if lockCalls == 1 && scenario == "missing published version" {
					version = 0
				}
				return err
			}
			writtenVersion, err := publishWindowsClipboardText(expectedVersion, withClipboard, func() uint64 { return version }, func() error {
				writes++
				version++
				if scenario == "write failure" {
					return failure
				}
				return nil
			}, func() error {
				confirmations++
				if scenario == "receipt rejected" {
					return failure
				}
				if scenario == "changed during confirmation" {
					version++
				}
				return nil
			})
			if err == nil || writtenVersion != 0 || writes > 1 || confirmations > 1 || lockCalls > 2 {
				test.Fatal("publication failure produced a paste receipt or silently retried")
			}
			if (scenario == "stale source" || scenario == "missing source version") && writes != 0 {
				test.Fatal("publication overwrote a stale or unknown source generation")
			}
			if (scenario == "write failure" || scenario == "first close failure") && lockCalls != 1 {
				test.Fatal("confirmation continued after failed publication")
			}
		})
	}
}

func TestClipboardPublicationVerifiesOwnerBeforeReadingAndMatchesExactText(test *testing.T) {
	for _, scenario := range []string{"own text", "identical text from another owner", "missing owner", "changed text", "extended text", "read failure", "owner changed during confirmation"} {
		test.Run(scenario, func(test *testing.T) {
			expectedText := []uint16{'o', 'k', 0}
			actualText := slices.Clone(expectedText)
			owner := uintptr(100)
			switch scenario {
			case "identical text from another owner":
				owner++
			case "missing owner":
				owner = 0
			case "changed text":
				actualText[0] = 'x'
			case "extended text":
				actualText[2] = 'x' // Same prefix is insufficient; the terminator must match.
			}
			reads := 0
			readFailure := errors.New("clipboard text unavailable")
			err := verifyPublishedClipboardText(100, expectedText, func() uintptr { return owner }, func() ([]uint16, error) {
				reads++
				if scenario == "read failure" {
					return actualText, readFailure
				}
				if scenario == "owner changed during confirmation" {
					owner++
				}
				return actualText, nil
			})
			if scenario == "own text" {
				if err != nil || reads != 1 {
					test.Fatalf("own publication could not be confirmed: %v", err)
				}
			} else if err == nil {
				test.Fatal("unverified clipboard became the committed publication")
			}
			if (scenario == "identical text from another owner" || scenario == "missing owner") && reads != 0 {
				test.Fatal("confirmation read another owner's clipboard")
			}
			if scenario == "read failure" && !errors.Is(err, readFailure) {
				test.Fatal("confirmation lost the native failure")
			}
			if reads != 0 && !slices.Equal(actualText, []uint16{0, 0, 0}) {
				test.Fatal("temporary confirmation text was retained")
			}
			if !slices.Equal(expectedText, []uint16{'o', 'k', 0}) {
				test.Fatal("confirmation altered the translated output")
			}
		})
	}
}
