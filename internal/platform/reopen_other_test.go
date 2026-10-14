//go:build !darwin || !cgo

package platform

import "testing"

func TestOnReopenIsNoOpOutsideNativeMacOS(t *testing.T) {
	unsubscribe := OnReopen(func() { t.Fatal("non-macOS reopen callback invoked") })
	unsubscribe()
	unsubscribe()
	OnReopen(nil)()
}
