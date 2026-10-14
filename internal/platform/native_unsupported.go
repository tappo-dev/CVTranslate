//go:build (!windows && !darwin) || (windows && !amd64 && !arm64) || (darwin && !cgo)

package platform

import "context"

func RunMain(run func()) { run() }

type unsupportedDesktop struct{}

func newNativeDesktop() nativeDesktop { return unsupportedDesktop{} }
func (unsupportedDesktop) status() Status {
	return Status{Message: "Native desktop support requires Windows amd64/arm64 or macOS with cgo and Apple frameworks"}
}
func (unsupportedDesktop) capture() (ClipboardCapture, error) {
	return ClipboardCapture{}, ErrUnsupported
}
func (unsupportedDesktop) validate(string, uint64) error                  { return ErrUnsupported }
func (unsupportedDesktop) modifiersReleased() bool                        { return false }
func (unsupportedDesktop) write(uint64, string) (uint64, error)           { return 0, ErrUnsupported }
func (unsupportedDesktop) dispatch(context.Context, string, uint64) error { return ErrUnsupported }
func (unsupportedDesktop) close() error                                   { return nil }
func registerNativeHotkey(combination, func()) (binding, error)           { return nil, ErrUnsupported }
func openBrowser(string) error                                            { return ErrUnsupported }
func getCredential(string, string) (string, error)                        { return "", ErrUnsupported }
func setCredential(string, string, string) error                          { return ErrUnsupported }
func deleteCredential(string, string) error                               { return ErrUnsupported }
func hasCredential(string, string) (bool, error)                          { return false, ErrUnsupported }
