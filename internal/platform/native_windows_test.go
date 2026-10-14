//go:build windows && (amd64 || arm64)

package platform

import (
	"testing"
	"unsafe"
)

func TestWindowsInputABILayout(t *testing.T) {
	input := keyboardInput{}
	if unsafe.Sizeof(input) != 40 || unsafe.Offsetof(input.VirtualKey) != 8 || unsafe.Offsetof(input.ExtraInfo) != 24 {
		t.Fatalf("incorrect 64-bit INPUT ABI: size=%d key=%d extra=%d", unsafe.Sizeof(input), unsafe.Offsetof(input.VirtualKey), unsafe.Offsetof(input.ExtraInfo))
	}
	if unsafe.Sizeof(guiThreadInfo{}) != 72 {
		t.Fatalf("incorrect GUITHREADINFO ABI: %d", unsafe.Sizeof(guiThreadInfo{}))
	}
}
