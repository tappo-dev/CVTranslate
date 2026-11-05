package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// launchAtLoginArgument keeps login launches in the background; the tray or a
// later application launch opens the WebUI explicitly.
const launchAtLoginArgument = "--no-open"

// LaunchAtLogin registers the current executable as a per-user login item. The
// OS registration is the only source of truth, so WebUI and tray changes cannot
// diverge and nothing is persisted in application storage.
type LaunchAtLogin struct {
	mutex  sync.Mutex
	native launchAtLoginNative
}

func NewLaunchAtLogin() (*LaunchAtLogin, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, errors.New("cannot locate the application executable")
	}
	if executable, err = filepath.EvalSymlinks(executable); err != nil {
		return nil, errors.New("cannot resolve the application executable")
	}
	native, err := newLaunchAtLoginNative(executable)
	if err != nil {
		return nil, err
	}
	return &LaunchAtLogin{native: native}, nil
}

func (launch *LaunchAtLogin) Enabled() (bool, error) {
	launch.mutex.Lock()
	defer launch.mutex.Unlock()
	return launch.native.enabled()
}

func (launch *LaunchAtLogin) SetEnabled(enabled bool) error {
	launch.mutex.Lock()
	defer launch.mutex.Unlock()
	return launch.native.setEnabled(enabled)
}

// toggle inverts the current registration for menu actions and returns the
// resulting state, which is re-read so a failed write is not misreported.
func (launch *LaunchAtLogin) toggle() (bool, error) {
	launch.mutex.Lock()
	defer launch.mutex.Unlock()
	enabled, err := launch.native.enabled()
	if err != nil {
		return false, err
	}
	writeError := launch.native.setEnabled(!enabled)
	enabled, err = launch.native.enabled()
	return enabled, errors.Join(writeError, err)
}

// fileLaunchAgent owns one generated file whose exact contents identify this
// executable's registration. A file for another executable reads as disabled.
type fileLaunchAgent struct {
	path     string
	contents []byte
}

func (agent fileLaunchAgent) enabled() (bool, error) {
	existing, err := os.ReadFile(agent.path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read login item: %w", err)
	}
	return string(existing) == string(agent.contents), nil
}

func (agent fileLaunchAgent) setEnabled(enabled bool) error {
	if !enabled {
		if err := os.Remove(agent.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove login item: %w", err)
		}
		return nil
	}
	directory := filepath.Dir(agent.path)
	if err := os.MkdirAll(directory, 0755); err != nil {
		return fmt.Errorf("create login item directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".cvtranslate-login-*")
	if err != nil {
		return fmt.Errorf("create login item: %w", err)
	}
	defer os.Remove(temporary.Name())
	_, writeError := temporary.Write(agent.contents)
	writeError = errors.Join(writeError, temporary.Chmod(0644), temporary.Close())
	if writeError != nil {
		return fmt.Errorf("write login item: %w", writeError)
	}
	if err := os.Rename(temporary.Name(), agent.path); err != nil {
		return fmt.Errorf("save login item: %w", err)
	}
	return nil
}
