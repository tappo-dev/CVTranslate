package platform

import (
	"sync"
	"time"
)

// startTrayWorker bridges polled native actions without running application
// callbacks on the Cocoa thread. consume must not dispatch to that thread.
func startTrayWorker(ticks <-chan time.Time, consume func() TrayEvent, remove func() error) *Tray {
	queue := newTrayEventQueue()
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer close(queue.events)
		for {
			select {
			case <-stop:
				return
			case _, available := <-ticks:
				if !available {
					return
				}
				select {
				case <-stop:
					return
				default:
				}
				event := consume()
				select {
				case <-stop:
					return
				default:
				}
				switch event {
				case TrayOpenUI:
					queue.requestOpen()
				case TrayQuit:
					queue.requestQuit()
				}
			}
		}
	}()
	var once sync.Once
	var closeError error
	return &Tray{Events: queue.events, close: func() error {
		once.Do(func() {
			close(stop)
			<-done
			closeError = remove()
		})
		return closeError
	}}
}
