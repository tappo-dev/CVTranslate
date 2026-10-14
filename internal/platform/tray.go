package platform

// TrayEvent is an explicit user action, handled outside the native message loop.
type TrayEvent uint8

const (
	TrayOpenUI TrayEvent = iota + 1
	TrayQuit
)

// Tray owns a native notification-area or menu-bar icon. Events closes if its
// event delivery stops; the application must then shut down rather than lose exit.
// Unsupported platforms return an inert Tray with a nil Events channel.
type Tray struct {
	Events <-chan TrayEvent
	close  func() error
}

func (tray *Tray) Close() error {
	if tray.close != nil {
		return tray.close()
	}
	return nil
}

// Each adapter has one producer (a native message thread or a polling worker).
// Coalesce repeated opens and prioritize quit so a full queue never loses exit.
type trayEventQueue struct {
	events   chan TrayEvent
	quitting bool
}

func newTrayEventQueue() *trayEventQueue {
	return &trayEventQueue{events: make(chan TrayEvent, 1)}
}

func (queue *trayEventQueue) requestOpen() {
	if queue.quitting {
		return
	}
	select {
	case queue.events <- TrayOpenUI:
	default:
	}
}

func (queue *trayEventQueue) requestQuit() {
	if queue.quitting {
		return
	}
	queue.quitting = true
	select {
	case <-queue.events:
	default:
	}
	queue.events <- TrayQuit
}
