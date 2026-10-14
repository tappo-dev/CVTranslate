package platform

import "testing"

func TestTrayQuitReplacesPendingOpenAndPreventsLaterActions(test *testing.T) {
	queue := newTrayEventQueue()
	queue.requestOpen()
	queue.requestOpen()
	queue.requestQuit()
	queue.requestOpen()
	queue.requestQuit()
	if event := <-queue.events; event != TrayQuit {
		test.Fatalf("exit was lost behind a browser request: %v", event)
	}
	queue.requestOpen()
	queue.requestQuit()
	select {
	case event := <-queue.events:
		test.Fatalf("action was queued after exit: %v", event)
	default:
	}
}

func TestTrayOpenCoalescesWithoutDisablingFutureRequests(test *testing.T) {
	queue := newTrayEventQueue()
	for range 5 {
		queue.requestOpen()
	}
	if event := <-queue.events; event != TrayOpenUI {
		test.Fatalf("unexpected tray event: %v", event)
	}
	select {
	case event := <-queue.events:
		test.Fatalf("repeated open was not coalesced: %v", event)
	default:
	}
	queue.requestOpen()
	if event := <-queue.events; event != TrayOpenUI {
		test.Fatalf("later open request was not accepted: %v", event)
	}
}
