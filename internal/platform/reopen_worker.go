package platform

import (
	"sync"
	"time"
)

// startReopenWorker serializes callbacks off the native event thread. Unsubscribe
// joins an in-flight callback; callbacks must not call their own unsubscribe.
func startReopenWorker(ticks <-chan time.Time, consume func() bool, callback func()) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			case _, open := <-ticks:
				if !open {
					return
				}
				select {
				case <-stop:
					return
				default:
				}
				if consume() && callback != nil {
					select {
					case <-stop:
						return
					default:
					}
					callback()
				}
			}
		}
	}()
	return func() { once.Do(func() { close(stop) }); <-done }
}
