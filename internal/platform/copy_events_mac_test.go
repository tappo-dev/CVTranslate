package platform

import (
	"testing"
	"time"
)

func TestMacCopyEventsUseExactCountsWithEitherCommandStyle(test *testing.T) {
	start := time.Unix(1000, 0)
	for _, releaseCommand := range []bool{false, true} {
		for _, count := range []int{1, 2, 3, 4, 5} {
			observer := &macCopyObserver{}
			version := uint64(1)
			snapshot := func() (uintptr, uint64) { return 11, version }
			for index := 0; index < count; index++ {
				moment := start.Add(time.Duration(index) * 100 * time.Millisecond)
				if releaseCommand {
					observer.observe(macCopyEvent{kind: macCopyFlagsChanged, key: macKeyRightCommand}, moment, 1, snapshot)
				}
				trigger := observer.observe(macCopyEvent{kind: macCopyKeyDown, key: macKeyCopy, commandOnly: true}, moment, 1, snapshot)
				if trigger.repetitions != 0 {
					test.Fatal("macOS copy fired before the quiet interval")
				}
				version++
				observer.observe(macCopyEvent{kind: macCopyKeyUp, key: macKeyCopy}, moment, 1, snapshot)
				if releaseCommand {
					observer.observe(macCopyEvent{kind: macCopyFlagsChanged, key: macKeyRightCommand}, moment, 1, snapshot)
				}
				observer.observe(macCopyEvent{kind: macCopyTimer}, moment.Add(25*time.Millisecond), 1, snapshot)
			}
			trigger := observer.observe(macCopyEvent{kind: macCopyTimer}, start.Add(2*time.Second), 1, snapshot)
			wantCount := 0
			if count == 3 || count == 4 {
				wantCount = count
			}
			if trigger.repetitions != wantCount || (wantCount != 0 && !trigger.clipboardUpdated) {
				test.Fatalf("count=%d releaseCommand=%v returned %+v", count, releaseCommand, trigger)
			}
			if repeated := observer.observe(macCopyEvent{kind: macCopyTimer}, start.Add(3*time.Second), 1, snapshot); repeated.repetitions != 0 {
				test.Fatal("one gesture fired twice")
			}
		}
	}
}

func TestMacCopyEventsCancelPartialGestures(test *testing.T) {
	scenarios := []struct {
		name  string
		event macCopyEvent
	}{
		{"other key", macCopyEvent{kind: macCopyKeyDown, key: 0}},
		{"Control+C instead of Command+C", macCopyEvent{kind: macCopyKeyDown, key: macKeyCopy}},
		{"additional modifier", macCopyEvent{kind: macCopyFlagsChanged, key: 56}},
		{"injected press", macCopyEvent{kind: macCopyKeyDown, key: macKeyCopy, commandOnly: true, injected: true}},
		{"injected release", macCopyEvent{kind: macCopyKeyUp, key: macKeyCopy, injected: true}},
		{"mouse or secure input interruption", macCopyEvent{kind: macCopyInterrupted}},
		{"listener failure", macCopyEvent{kind: macCopyListenerFailed}},
	}
	for _, scenario := range scenarios {
		test.Run(scenario.name, func(test *testing.T) {
			start := time.Unix(1000, 0)
			observer := &macCopyObserver{generation: 1}
			snapshot := func() (uintptr, uint64) { return 11, 7 }
			for index := 0; index < 3; index++ {
				moment := start.Add(time.Duration(index) * 100 * time.Millisecond)
				observer.observe(macCopyEvent{kind: macCopyKeyDown, key: macKeyCopy, commandOnly: true}, moment, 1, snapshot)
				observer.observe(macCopyEvent{kind: macCopyKeyUp, key: macKeyCopy}, moment, 1, snapshot)
			}
			observer.observe(scenario.event, start.Add(250*time.Millisecond), 1, snapshot)
			if trigger := observer.observe(macCopyEvent{kind: macCopyTimer}, start.Add(time.Second), 1, func() (uintptr, uint64) { return 11, 8 }); trigger.repetitions != 0 {
				test.Fatal("interrupted macOS gesture fired")
			}
		})
	}
}

func TestMacCopyEventsRejectAutoRepeatAndReconfiguration(test *testing.T) {
	start := time.Unix(1000, 0)
	observer := &macCopyObserver{}
	snapshot := func() (uintptr, uint64) { return 11, 7 }
	press := macCopyEvent{kind: macCopyKeyDown, key: macKeyCopy, commandOnly: true}
	release := macCopyEvent{kind: macCopyKeyUp, key: macKeyCopy}
	observer.observe(press, start, 1, snapshot)
	for index := 1; index < 5; index++ {
		repeated := press
		repeated.autoRepeat = true
		observer.observe(repeated, start.Add(time.Duration(index)*100*time.Millisecond), 1, snapshot)
	}
	if observer.sequence.pressCount != 1 {
		test.Fatal("macOS auto-repeat counted as physical copy presses")
	}
	observer.observe(macCopyEvent{kind: macCopyTimer}, start, 2, snapshot)
	observer.observe(press, start, 2, snapshot)
	if observer.sequence.pressCount != 0 || !observer.sequence.copyHeld {
		test.Fatal("reconfiguration lost held-key state or retained old presses")
	}
	observer.observe(release, start, 2, snapshot)
	observer.observe(press, start, 2, snapshot)
	if observer.sequence.pressCount != 1 {
		test.Fatal("new physical press failed after reconfiguration")
	}
	observer.observe(macCopyEvent{kind: macCopyTimer}, start.Add(time.Second), 0, snapshot)
	if observer.sequence.pressCount != 0 {
		test.Fatal("deactivated listener retained a pending gesture")
	}
}

func TestMacCopyEventsRequireFreshClipboardAndUnchangedWindow(test *testing.T) {
	for _, scenario := range []string{"copied", "stale clipboard", "window changed"} {
		test.Run(scenario, func(test *testing.T) {
			observer := &macCopyObserver{}
			start := time.Unix(1000, 0)
			foreground, version := uintptr(11), uint64(7)
			snapshot := func() (uintptr, uint64) { return foreground, version }
			for index := 0; index < 3; index++ {
				moment := start.Add(time.Duration(index) * 100 * time.Millisecond)
				observer.observe(macCopyEvent{kind: macCopyKeyDown, key: macKeyCopy, commandOnly: true}, moment, 1, snapshot)
				observer.observe(macCopyEvent{kind: macCopyKeyUp, key: macKeyCopy}, moment, 1, snapshot)
			}
			if scenario != "stale clipboard" {
				version++
			}
			if scenario == "window changed" {
				foreground++
			}
			trigger := observer.observe(macCopyEvent{kind: macCopyTimer}, start.Add(time.Second), 1, snapshot)
			if (trigger.repetitions == 3 && trigger.clipboardUpdated) != (scenario == "copied") {
				test.Fatalf("incorrect clipboard/window confirmation: %+v", trigger)
			}
		})
	}
}

func TestMacCopyIdleListenerDoesNotReadSnapshots(test *testing.T) {
	observer := &macCopyObserver{}
	snapshot := func() (uintptr, uint64) {
		test.Fatal("idle or unrelated input queried clipboard metadata")
		return 0, 0
	}
	for _, event := range []macCopyEvent{{kind: macCopyTimer}, {kind: macCopyKeyDown, key: 0}, {kind: macCopyKeyUp, key: 0}, {kind: macCopyFlagsChanged, key: macKeyCommand}} {
		observer.observe(event, time.Now(), 1, snapshot)
	}
}
