//go:build !darwin || !cgo

package platform

// OnReopen is a no-op outside native macOS. Windows uses the authenticated
// second-process instance-lock path owned by the application entry point.
func OnReopen(callback func()) func() { return func() {} }
