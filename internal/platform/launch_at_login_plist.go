package platform

import (
	"bytes"
	"encoding/xml"
)

const (
	macLaunchAgentLabel = "com.cvtranslate.desktop.login"
	macBundleIdentifier = "com.cvtranslate.desktop"
)

// macLaunchAgent runs the bundle executable once at login, without KeepAlive,
// so quitting from the menu bar stays effective for the rest of the session.
func macLaunchAgent(executable string) []byte {
	var plist bytes.Buffer
	escape := func(value string) string {
		var escaped bytes.Buffer
		_ = xml.EscapeText(&escaped, []byte(value))
		return escaped.String()
	}
	plist.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>` + macLaunchAgentLabel + `</string>
  <key>ProgramArguments</key>
  <array>
    <string>` + escape(executable) + `</string>
    <string>` + launchAtLoginArgument + `</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>LimitLoadToSessionType</key><string>Aqua</string>
  <key>AssociatedBundleIdentifiers</key>
  <array><string>` + macBundleIdentifier + `</string></array>
</dict>
</plist>
`)
	return plist.Bytes()
}
