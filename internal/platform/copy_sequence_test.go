package platform

import (
	"testing"
	"time"
)

func pressCopy(sequence *copySequence, moment time.Time, foreground uintptr, version uint64) {
	sequence.press(moment, foreground, version)
	sequence.release()
}

func TestCopySequenceUsesExactCountsAfterQuietInterval(test *testing.T) {
	start := time.Unix(1000, 0)
	for _, count := range []int{1, 2, 3, 4, 5, 8} {
		sequence := &copySequence{}
		for index := 0; index < count; index++ {
			moment := start.Add(time.Duration(index) * 100 * time.Millisecond)
			pressCopy(sequence, moment, 11, uint64(index+1))
			if actual := sequence.complete(moment.Add(50*time.Millisecond), 11, uint64(index+2)); actual.repetitions != 0 {
				test.Fatalf("sequence of %d fired early at press %d", count, index+1)
			}
		}
		deadline := start.Add(time.Duration(count-1)*100*time.Millisecond + copySequenceInterval)
		expected := 0
		if count == 3 || count == 4 {
			expected = count
		}
		if actual := sequence.complete(deadline, 11, uint64(count+1)); actual.repetitions != expected || (expected != 0 && !actual.clipboardUpdated) {
			test.Fatalf("sequence of %d fired as %+v, expected %d", count, actual, expected)
		}
		if actual := sequence.complete(deadline.Add(time.Second), 11, uint64(count+1)); actual.repetitions != 0 {
			test.Fatal("one gesture dispatched more than once")
		}
	}
}

func TestCopySequenceFourthPressReplacesThird(test *testing.T) {
	sequence := &copySequence{}
	start := time.Unix(1000, 0)
	for index := 0; index < 3; index++ {
		pressCopy(sequence, start.Add(time.Duration(index)*100*time.Millisecond), 11, uint64(index+1))
	}
	if actual := sequence.complete(start.Add(690*time.Millisecond), 11, 4); actual.repetitions != 0 {
		test.Fatal("third press fired before the fourth-press deadline")
	}
	pressCopy(sequence, start.Add(695*time.Millisecond), 11, 4)
	if actual := sequence.complete(start.Add(1200*time.Millisecond), 11, 5); actual.repetitions != 4 {
		test.Fatalf("fourth press did not replace pending third press: %+v", actual)
	}
}

func TestCopySequenceRejectsHeldKeysAndUnconfirmedCopy(test *testing.T) {
	start := time.Unix(1000, 0)
	sequence := &copySequence{}
	for index := 0; index < 5; index++ {
		sequence.press(start.Add(time.Duration(index)*50*time.Millisecond), 11, 1)
	}
	sequence.release()
	if actual := sequence.complete(start.Add(time.Second), 11, 2); actual.repetitions != 0 {
		test.Fatal("holding C counted as multiple presses")
	}
	for _, scenario := range []string{"held third press", "copy not confirmed", "clipboard unavailable", "foreground changed"} {
		test.Run(scenario, func(test *testing.T) {
			sequence := &copySequence{}
			for index := 0; index < 3; index++ {
				pressCopy(sequence, start.Add(time.Duration(index)*100*time.Millisecond), 11, 7)
			}
			foreground, version := uintptr(11), uint64(8)
			switch scenario {
			case "held third press":
				sequence.copyHeld = true
			case "copy not confirmed":
				version = 7
			case "clipboard unavailable":
				version = 0
			case "foreground changed":
				foreground = 12
			}
			if actual := sequence.complete(start.Add(time.Second), foreground, version); actual.repetitions != 0 && actual.clipboardUpdated {
				test.Fatal("unsafe or incomplete sequence dispatched")
			}
		})
	}
}

func TestCopySequenceDoesNotJoinSeparateGestures(test *testing.T) {
	start := time.Unix(1000, 0)
	for _, interruption := range []string{"timeout", "other key", "foreground switch", "foreground switch between presses"} {
		test.Run(interruption, func(test *testing.T) {
			sequence := &copySequence{}
			pressCopy(sequence, start, 11, 1)
			pressCopy(sequence, start.Add(100*time.Millisecond), 11, 2)
			last := start.Add(200 * time.Millisecond)
			foreground := uintptr(11)
			switch interruption {
			case "timeout":
				last = start.Add(time.Second)
			case "other key":
				sequence.cancel()
			case "foreground switch":
				sequence.complete(start.Add(150*time.Millisecond), 12, 3)
			case "foreground switch between presses":
				foreground = 12
			}
			pressCopy(sequence, last, foreground, 3)
			if actual := sequence.complete(last.Add(copySequenceInterval), foreground, 4); actual.repetitions != 0 {
				test.Fatal("interrupted gesture was joined to a later copy")
			}
		})
	}
}

func TestCopySequenceResolvesDeadlineBeforeALaterPress(test *testing.T) {
	start := time.Unix(1000, 0)
	sequence := &copySequence{}
	for index := 0; index < 3; index++ {
		pressCopy(sequence, start.Add(time.Duration(index)*100*time.Millisecond), 11, uint64(index+1))
	}
	trigger := sequence.press(start.Add(710*time.Millisecond), 11, 4)
	if trigger.repetitions != 3 || !trigger.clipboardUpdated || sequence.pressCount != 1 {
		test.Fatalf("delayed timer lost the completed gesture: %+v, next count %d", trigger, sequence.pressCount)
	}
}

func TestCopyCaptureRemainsBoundToGestureSnapshot(test *testing.T) {
	for _, scenario := range []string{"unchanged", "clipboard changed", "foreground changed", "foreground changed during capture", "sensitive capture failure"} {
		test.Run(scenario, func(test *testing.T) {
			foreground, version := uintptr(11), uint64(7)
			if scenario == "clipboard changed" {
				version++
			}
			if scenario == "foreground changed" {
				foreground++
			}
			capture, err := captureClipboardCopyAtSnapshot(confirmedCopy(3), func() uintptr { return foreground }, func() (ClipboardCapture, error) {
				if scenario == "foreground changed during capture" {
					foreground++
				}
				if scenario == "sensitive capture failure" {
					return ClipboardCapture{}, ErrUnsupported
				}
				return ClipboardCapture{Text: "source", version: version, target: "target"}, nil
			})
			if scenario == "unchanged" {
				if err != nil || capture.Text != "source" || capture.version != 7 || capture.target != "target" {
					test.Fatal("unchanged copy snapshot was not captured")
				}
			} else if err == nil || capture.Text != "" || capture.target != "" || capture.version != 0 {
				test.Fatal("mismatched or sensitive capture returned clipboard content")
			}
		})
	}
}
