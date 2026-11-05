//go:build !windows && !darwin

package platform

import "errors"

type launchAtLoginNative struct{}

func newLaunchAtLoginNative(string) (launchAtLoginNative, error) {
	return launchAtLoginNative{}, errors.New("launch at login is unsupported on this platform")
}

func (launchAtLoginNative) enabled() (bool, error) { return false, nil }

func (launchAtLoginNative) setEnabled(bool) error {
	return errors.New("launch at login is unsupported on this platform")
}
