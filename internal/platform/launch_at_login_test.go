package platform

import (
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileLaunchAgentRecognizesOnlyItsOwnRegistration(test *testing.T) {
	path := filepath.Join(test.TempDir(), "LaunchAgents", "item.plist")
	agent := fileLaunchAgent{path: path, contents: macLaunchAgent("/Applications/CVTranslate.app/Contents/MacOS/cvtranslate")}
	if enabled, err := agent.enabled(); err != nil || enabled {
		test.Fatalf("missing item read as enabled=%v error=%v", enabled, err)
	}
	if err := agent.setEnabled(false); err != nil {
		test.Fatalf("disabling a missing item failed: %v", err)
	}
	if err := agent.setEnabled(true); err != nil {
		test.Fatal(err)
	}
	if enabled, err := agent.enabled(); err != nil || !enabled {
		test.Fatalf("saved item read as enabled=%v error=%v", enabled, err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		test.Fatalf("temporary login item files were left behind: %d entries", len(entries))
	}
	other := fileLaunchAgent{path: path, contents: macLaunchAgent("/Users/me/Downloads/CVTranslate.app/Contents/MacOS/cvtranslate")}
	if enabled, err := other.enabled(); err != nil || enabled {
		test.Fatalf("another executable's item read as enabled=%v error=%v", enabled, err)
	}
	if err := agent.setEnabled(false); err != nil {
		test.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		test.Fatalf("disabled item still exists: %v", err)
	}
}

func TestMacLaunchAgentIsValidXMLWithoutKeepAlive(test *testing.T) {
	executable := "/Applications/A & <B>/CVTranslate.app/Contents/MacOS/cvtranslate"
	plist := macLaunchAgent(executable)
	decoder := xml.NewDecoder(strings.NewReader(string(plist)))
	decoder.Strict = true
	var text strings.Builder
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			test.Fatalf("invalid plist XML: %v", err)
		}
		if data, ok := token.(xml.CharData); ok {
			text.Write(data)
		}
	}
	if !strings.Contains(text.String(), executable) || !strings.Contains(text.String(), launchAtLoginArgument) {
		test.Fatalf("plist lost its program arguments: %s", plist)
	}
	if strings.Contains(string(plist), "KeepAlive") {
		test.Fatal("login item would relaunch the application after Quit")
	}
}
