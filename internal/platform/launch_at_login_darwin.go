//go:build darwin

package platform

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type launchAtLoginNative = fileLaunchAgent

// A per-user LaunchAgent needs no launchctl call or extra permission; launchd
// reads it at the next login.
func newLaunchAtLoginNative(executable string) (launchAtLoginNative, error) {
	// Gatekeeper's randomized translocation path disappears after the app quits.
	if strings.Contains(executable, "/AppTranslocation/") {
		return launchAtLoginNative{}, errors.New("move CVTranslate to the Applications folder to enable launch at login")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return launchAtLoginNative{}, errors.New("cannot locate the user LaunchAgents directory")
	}
	return launchAtLoginNative{
		path:     filepath.Join(home, "Library", "LaunchAgents", macLaunchAgentLabel+".plist"),
		contents: macLaunchAgent(executable),
	}, nil
}
