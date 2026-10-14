package platform

import "time"

// Normalized events from the pass-through macOS tap. The reducer is pure Go so
// physical-key filtering and timing can be tested without installing a hook.
const (
	macCopyTimer = iota
	macCopyKeyDown
	macCopyKeyUp
	macCopyFlagsChanged
	macCopyInterrupted
	macCopyListenerFailed
)

const (
	macKeyCopy         = 8
	macKeyCommand      = 55
	macKeyRightCommand = 54
)

type macCopyEvent struct {
	kind        int
	key         int
	commandOnly bool
	autoRepeat  bool
	injected    bool
}

type macCopyObserver struct {
	sequence   copySequence
	generation uint64
}

func (observer *macCopyObserver) observe(event macCopyEvent, now time.Time, generation uint64, snapshot func() (uintptr, uint64)) copyTrigger {
	if observer.generation != generation {
		observer.sequence.cancel()
		observer.generation = generation
	}
	if event.injected || event.kind == macCopyInterrupted || event.kind == macCopyListenerFailed {
		observer.sequence.cancel()
		return copyTrigger{}
	}
	if event.kind == macCopyKeyUp && event.key == macKeyCopy {
		observer.sequence.release()
		return copyTrigger{}
	}
	if generation == 0 {
		if event.kind == macCopyKeyDown && event.key == macKeyCopy {
			observer.sequence.copyHeld = true
		}
		return copyTrigger{}
	}
	switch event.kind {
	case macCopyTimer:
		if observer.sequence.pressCount != 0 {
			foreground, version := snapshot()
			return observer.sequence.complete(now, foreground, version)
		}
	case macCopyFlagsChanged:
		if event.key != macKeyCommand && event.key != macKeyRightCommand {
			observer.sequence.cancel()
		}
	case macCopyKeyDown:
		if event.key != macKeyCopy || !event.commandOnly {
			observer.sequence.cancel()
			if event.key == macKeyCopy {
				observer.sequence.copyHeld = true
			}
			return copyTrigger{}
		}
		if event.autoRepeat {
			observer.sequence.copyHeld = true
			return copyTrigger{}
		}
		if !observer.sequence.copyHeld {
			foreground, version := snapshot()
			return observer.sequence.press(now, foreground, version)
		}
	}
	return copyTrigger{}
}
