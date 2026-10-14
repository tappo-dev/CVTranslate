//go:build (windows && (amd64 || arm64)) || (darwin && cgo)

package platform

import (
	"fmt"
	"sync"

	"golang.design/x/hotkey"
)

type nativeBinding struct {
	hotkey *hotkey.Hotkey
	stop   chan struct{}
	mutex  sync.Mutex
	closed bool
}

func registerNativeHotkey(combination combination, callback func()) (binding, error) {
	keys := map[string]hotkey.Key{
		"A": hotkey.KeyA, "B": hotkey.KeyB, "C": hotkey.KeyC, "D": hotkey.KeyD,
		"E": hotkey.KeyE, "F": hotkey.KeyF, "G": hotkey.KeyG, "H": hotkey.KeyH,
		"I": hotkey.KeyI, "J": hotkey.KeyJ, "K": hotkey.KeyK, "L": hotkey.KeyL,
		"M": hotkey.KeyM, "N": hotkey.KeyN, "O": hotkey.KeyO, "P": hotkey.KeyP,
		"Q": hotkey.KeyQ, "R": hotkey.KeyR, "S": hotkey.KeyS, "T": hotkey.KeyT,
		"U": hotkey.KeyU, "V": hotkey.KeyV, "W": hotkey.KeyW, "X": hotkey.KeyX,
		"Y": hotkey.KeyY, "Z": hotkey.KeyZ,
		"0": hotkey.Key0, "1": hotkey.Key1, "2": hotkey.Key2, "3": hotkey.Key3,
		"4": hotkey.Key4, "5": hotkey.Key5, "6": hotkey.Key6, "7": hotkey.Key7,
		"8": hotkey.Key8, "9": hotkey.Key9,
		"SPACE": hotkey.KeySpace, "RETURN": hotkey.KeyReturn, "ESCAPE": hotkey.KeyEscape,
		"TAB": hotkey.KeyTab, "LEFT": hotkey.KeyLeft, "RIGHT": hotkey.KeyRight,
		"UP": hotkey.KeyUp, "DOWN": hotkey.KeyDown,
		"F1": hotkey.KeyF1, "F2": hotkey.KeyF2, "F3": hotkey.KeyF3, "F4": hotkey.KeyF4,
		"F5": hotkey.KeyF5, "F6": hotkey.KeyF6, "F7": hotkey.KeyF7, "F8": hotkey.KeyF8,
		"F9": hotkey.KeyF9, "F10": hotkey.KeyF10, "F11": hotkey.KeyF11, "F12": hotkey.KeyF12,
	}
	var modifiers []hotkey.Modifier
	for _, name := range combination.modifiers {
		modifier, exists := nativeModifiers()[name]
		if !exists {
			return nil, fmt.Errorf("unsupported modifier %s", name)
		}
		modifiers = append(modifiers, modifier)
	}
	native := hotkey.New(modifiers, keys[combination.key])
	if err := native.Register(); err != nil {
		return nil, err
	}
	registered := &nativeBinding{hotkey: native, stop: make(chan struct{})}
	keydown, keyup := native.Keydown(), native.Keyup()
	go func() {
		pressed := false
		active := true
		stop := registered.stop
		for keydown != nil || keyup != nil {
			select {
			case <-stop:
				// Unregister closes both event streams. Drain their queued events
				// without callbacks so the library's channel brokers can exit.
				active = false
				stop = nil
			case _, open := <-keydown:
				if !open {
					keydown = nil
					continue
				}
				if active && !pressed {
					pressed = true
					go callback()
				}
			case _, open := <-keyup:
				if !open {
					keyup = nil
					continue
				}
				pressed = false
			}
		}
	}()
	return registered, nil
}

func (registered *nativeBinding) close() error {
	registered.mutex.Lock()
	defer registered.mutex.Unlock()
	if registered.closed {
		return nil
	}
	// The library leaves a registration intact when Unregister returns an
	// error. Do not stop delivery or poison retries: rollback retains it.
	if err := registered.hotkey.Unregister(); err != nil {
		return err
	}
	registered.closed = true
	close(registered.stop)
	return nil
}
