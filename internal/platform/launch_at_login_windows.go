//go:build windows

package platform

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	windowsRunKey             = `Software\Microsoft\Windows\CurrentVersion\Run`
	windowsStartupApprovedKey = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`
	windowsLaunchValueName    = "CVTranslate"
)

// The per-user Run key needs no elevation. Task Manager's Startup apps page
// disables an entry through StartupApproved without removing it.
type launchAtLoginNative struct {
	runKey, approvedKey string
	command             string
}

func newLaunchAtLoginNative(executable string) (launchAtLoginNative, error) {
	return launchAtLoginNative{
		runKey: windowsRunKey, approvedKey: windowsStartupApprovedKey,
		// Windows paths cannot contain quotes, so quoting is unambiguous.
		command: `"` + executable + `" ` + launchAtLoginArgument,
	}, nil
}

func (native launchAtLoginNative) enabled() (bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, native.runKey, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read login items: %w", err)
	}
	defer key.Close()
	command, _, err := key.GetStringValue(windowsLaunchValueName)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read login item: %w", err)
	}
	if !strings.EqualFold(command, native.command) {
		return false, nil
	}
	return !native.startupDisabled(), nil
}

// StartupApproved data starts with an odd byte when the user disabled the entry.
func (native launchAtLoginNative) startupDisabled() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, native.approvedKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	state, _, err := key.GetBinaryValue(windowsLaunchValueName)
	return err == nil && len(state) > 0 && state[0]&1 == 1
}

func (native launchAtLoginNative) setEnabled(enabled bool) error {
	if enabled {
		key, _, err := registry.CreateKey(registry.CURRENT_USER, native.runKey, registry.SET_VALUE)
		if err != nil {
			return fmt.Errorf("open login items: %w", err)
		}
		defer key.Close()
		if err := key.SetStringValue(windowsLaunchValueName, native.command); err != nil {
			return fmt.Errorf("save login item: %w", err)
		}
	} else if err := deleteRegistryValue(native.runKey, windowsLaunchValueName); err != nil {
		return fmt.Errorf("remove login item: %w", err)
	}
	// An explicit choice here supersedes an earlier Task Manager decision.
	if err := deleteRegistryValue(native.approvedKey, windowsLaunchValueName); err != nil {
		return fmt.Errorf("reset login item approval: %w", err)
	}
	return nil
}

func deleteRegistryValue(path, name string) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, path, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer key.Close()
	if err := key.DeleteValue(name); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}
