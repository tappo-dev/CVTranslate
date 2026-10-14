//go:build (!windows && !darwin) || (windows && !amd64 && !arm64) || (darwin && !cgo)

package platform

// StartTray is inert on builds without a supported native desktop adapter.
func StartTray() (*Tray, error) { return &Tray{}, nil }
