//go:build windows

package platform

import (
	"testing"

	"golang.org/x/sys/windows/registry"
)

// The test uses a private HKCU subtree, never the real Run key.
func testWindowsLaunchAtLogin(test *testing.T) (*LaunchAtLogin, string) {
	const parent = `Software\CVTranslateTest`
	root := parent + `\` + test.Name()
	test.Cleanup(func() {
		// The parent is removed by whichever test cleans up last.
		for _, path := range []string{root + `\Approved`, root + `\Run`, root, parent} {
			_ = registry.DeleteKey(registry.CURRENT_USER, path)
		}
	})
	native, _ := newLaunchAtLoginNative(`C:\Program Files\CVTranslate\cvtranslate.exe`)
	native.runKey, native.approvedKey = root+`\Run`, root+`\Approved`
	return &LaunchAtLogin{native: native}, root
}

func TestWindowsLaunchAtLoginUsesQuotedBackgroundCommand(test *testing.T) {
	launch, root := testWindowsLaunchAtLogin(test)
	if enabled, err := launch.Enabled(); err != nil || enabled {
		test.Fatalf("missing key read as enabled=%v error=%v", enabled, err)
	}
	if enabled, err := launch.toggle(); err != nil || !enabled {
		test.Fatalf("toggle on returned enabled=%v error=%v", enabled, err)
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, root+`\Run`, registry.QUERY_VALUE)
	if err != nil {
		test.Fatal(err)
	}
	command, _, err := key.GetStringValue(windowsLaunchValueName)
	key.Close()
	if err != nil || command != `"C:\Program Files\CVTranslate\cvtranslate.exe" --no-open` {
		test.Fatalf("unexpected login command %q: %v", command, err)
	}
	if enabled, err := launch.toggle(); err != nil || enabled {
		test.Fatalf("toggle off returned enabled=%v error=%v", enabled, err)
	}
}

func TestWindowsLaunchAtLoginHonorsAndResetsTaskManagerDisable(test *testing.T) {
	launch, root := testWindowsLaunchAtLogin(test)
	if err := launch.SetEnabled(true); err != nil {
		test.Fatal(err)
	}
	approved, _, err := registry.CreateKey(registry.CURRENT_USER, root+`\Approved`, registry.SET_VALUE)
	if err != nil {
		test.Fatal(err)
	}
	err = approved.SetBinaryValue(windowsLaunchValueName, []byte{3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	approved.Close()
	if err != nil {
		test.Fatal(err)
	}
	if enabled, err := launch.Enabled(); err != nil || enabled {
		test.Fatalf("Task Manager disabled entry read as enabled=%v error=%v", enabled, err)
	}
	if err := launch.SetEnabled(true); err != nil {
		test.Fatal(err)
	}
	if enabled, err := launch.Enabled(); err != nil || !enabled {
		test.Fatalf("explicit enable did not override Task Manager: enabled=%v error=%v", enabled, err)
	}
}

func TestWindowsLaunchAtLoginIgnoresAnotherExecutable(test *testing.T) {
	launch, root := testWindowsLaunchAtLogin(test)
	key, _, err := registry.CreateKey(registry.CURRENT_USER, root+`\Run`, registry.SET_VALUE)
	if err != nil {
		test.Fatal(err)
	}
	err = key.SetStringValue(windowsLaunchValueName, `"D:\Old\cvtranslate.exe" --no-open`)
	key.Close()
	if err != nil {
		test.Fatal(err)
	}
	if enabled, err := launch.Enabled(); err != nil || enabled {
		test.Fatalf("another executable's entry read as enabled=%v error=%v", enabled, err)
	}
}
