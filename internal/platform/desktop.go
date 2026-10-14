// Package platform contains conservative desktop integrations for Windows and macOS.
package platform

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"cvtranslate/internal/domain"
)

var ErrUnsupported = errors.New("desktop integration is unsupported on this build")

// ErrHotkeysDegraded means rollback could not restore native registrations.
// All shortcut callbacks are disabled until the Desktop is closed and recreated.
var ErrHotkeysDegraded = errors.New("shortcut rollback failed; shortcuts disabled until desktop restart")

type Status struct {
	OS               string `json:"os"`
	HotkeysAvailable bool   `json:"hotkeysAvailable"`
	PasteAvailable   bool   `json:"pasteAvailable"`
	Message          string `json:"message"`
}

// ClipboardCapture binds copied source text to a clipboard generation and paste
// target. The private token belongs to its Desktop and is never a request to
// focus a target.
// A paste-only failure travels with the source instead of being lost or replaced
// by a later capture. Only the explicit three-copy experiment bypasses that
// failure; source-capture errors remain fatal for every action.
type ClipboardCapture struct {
	Text        string
	owner       *Desktop
	version     uint64
	target      string
	pasteError  error
	directPaste bool
}

// UsesDirectPaste identifies the user-approved three-copy direct-paste action.
// Only native shortcut routing can set it; HTTP callers cannot opt a capture in.
func (capture ClipboardCapture) UsesDirectPaste() bool { return capture.directPaste }

// ShortcutEvent may carry the capture already bound to a repeated-copy gesture.
// It is transient; never serialize, persist, or log its clipboard contents.
type ShortcutEvent struct {
	Action       string
	Capture      *ClipboardCapture
	CaptureError error
}

type copyCaptureDesktop interface {
	captureCopy(copyTrigger) (ClipboardCapture, error)
}

// directPasteDesktop deliberately omits target validation. It is implemented
// on Windows/macOS for the explicitly requested three-copy paste action.
type directPasteDesktop interface {
	validateClipboard(version uint64) error
	dispatchDirect(ctx context.Context, version uint64) error
}

type nativeDesktop interface {
	status() Status
	capture() (ClipboardCapture, error)
	validate(target string, version uint64) error
	modifiersReleased() bool
	write(version uint64, text string) (uint64, error)
	dispatch(ctx context.Context, target string, version uint64) error
	close() error
}

// A failed close must leave the binding usable; successful close is idempotent.
type binding interface{ close() error }
type copySequenceBinding interface {
	binding
	activate(generation uint64)
	failure() error
}

const copySequenceBindingName = "COPY*"

func copyShortcutForOS(operatingSystem string) string {
	if operatingSystem == "darwin" {
		return "CMD+C"
	}
	return "CTRL+C"
}

type combination struct {
	name        string
	modifiers   []string
	key         string
	repetitions int
}
type registration struct {
	binding       binding
	action        string
	repeatActions map[int]string
}

type Desktop struct {
	mutex                   sync.Mutex
	native                  nativeDesktop
	register                func(combination, func()) (binding, error)
	registerSequence        func(func(copyTrigger, uint64)) (copySequenceBinding, error)
	sequenceGeneration      uint64
	bindings                map[string]registration
	callback                func(ShortcutEvent)
	closed                  bool
	lastError               string
	hotkeysDegraded         bool
	orphanedBindings        []binding
	fixedClipboardShortcuts bool
}

func NewDesktop() *Desktop {
	return &Desktop{
		native: newNativeDesktop(), register: registerNativeHotkey,
		registerSequence: registerNativeCopySequence, bindings: make(map[string]registration),
		fixedClipboardShortcuts: runtime.GOOS == "windows" || runtime.GOOS == "darwin",
	}
}

func (desktop *Desktop) Status() Status {
	desktop.mutex.Lock()
	defer desktop.mutex.Unlock()
	status := desktop.native.status()
	status.OS = runtime.GOOS
	if registered, exists := desktop.bindings[copySequenceBindingName]; exists {
		if err := registered.binding.(copySequenceBinding).failure(); err != nil {
			status.HotkeysAvailable = false
			status.Message += "; Repeated-copy listener stopped; restart the application"
		}
		if registered.repeatActions[3] == "translate-paste" {
			status.Message += "; Experimental three-copy paste bypasses target checks and sends the paste shortcut to whichever window is active after translation"
		}
	} else if desktop.fixedClipboardShortcuts {
		status.HotkeysAvailable = false
		status.Message += "; Repeated-copy listener is not active; check permissions and restart the application"
	}
	if desktop.hotkeysDegraded {
		status.HotkeysAvailable = false
	}
	if desktop.closed {
		status.HotkeysAvailable = false
		status.PasteAvailable = false
		status.Message = "Desktop integration is closed"
	}
	if desktop.lastError != "" {
		status.Message += "; " + desktop.lastError
	}
	return status
}

func normalizeHotkey(value string) (combination, error) {
	return normalizeHotkeyForOS(value, runtime.GOOS)
}

func normalizeHotkeyForOS(value, operatingSystem string) (combination, error) {
	result := combination{repetitions: 1}
	if strings.TrimSpace(value) == "" {
		return result, nil
	}
	if strings.Contains(value, "*") {
		sequenceParts := strings.Split(value, "*")
		if len(sequenceParts) != 2 || (strings.TrimSpace(sequenceParts[1]) != "3" && strings.TrimSpace(sequenceParts[1]) != "4") {
			return result, errors.New("repeated copy shortcuts require exactly three or four presses")
		}
		if operatingSystem != "windows" && operatingSystem != "darwin" {
			return result, errors.New("repeated copy shortcuts are available on Windows and macOS only")
		}
		result.repetitions = int(strings.TrimSpace(sequenceParts[1])[0] - '0')
		value = sequenceParts[0]
	}
	parts := strings.Split(value, "+")
	modifiers := map[string]bool{}
	for index, part := range parts {
		part = strings.ToUpper(strings.TrimSpace(part))
		if index == len(parts)-1 {
			if !validKey(part) {
				return result, fmt.Errorf("unsupported shortcut key %q", part)
			}
			result.key = part
			continue
		}
		switch part {
		case "CTRL", "CONTROL":
			part = "CTRL"
		case "ALT", "OPTION":
			part = "ALT"
		case "SHIFT":
		case "CMD", "COMMAND":
			part = "CMD"
		case "WIN", "SUPER":
			part = "WIN"
		default:
			return result, fmt.Errorf("unsupported shortcut modifier %q", part)
		}
		if (operatingSystem == "windows" && part == "CMD") || (operatingSystem == "darwin" && part == "WIN") {
			return result, fmt.Errorf("modifier %s is not available on %s", part, operatingSystem)
		}
		if modifiers[part] {
			return result, errors.New("shortcut repeats a modifier")
		}
		modifiers[part] = true
		result.modifiers = append(result.modifiers, part)
	}
	if len(result.modifiers) == 0 {
		return result, errors.New("global shortcuts require a modifier")
	}
	sort.Strings(result.modifiers)
	result.name = strings.Join(append(append([]string{}, result.modifiers...), result.key), "+")
	if result.repetitions > 1 {
		if result.name != copyShortcutForOS(operatingSystem) {
			return result, errors.New("only the platform copy shortcut supports repeated presses")
		}
		result.name += fmt.Sprintf("*%d", result.repetitions)
	}
	return result, nil
}

func validKey(key string) bool {
	if len(key) == 1 {
		return (key[0] >= 'A' && key[0] <= 'Z') || (key[0] >= '0' && key[0] <= '9')
	}
	switch key {
	case "SPACE", "RETURN", "ESCAPE", "TAB", "LEFT", "RIGHT", "UP", "DOWN":
		return true
	}
	for number := 1; number <= 12; number++ {
		if key == fmt.Sprintf("F%d", number) {
			return true
		}
	}
	return false
}

// ConfigureHotkeys stages additions before removing old bindings. A registration
// conflict therefore leaves the complete previous configuration active.
func (desktop *Desktop) ConfigureHotkeys(keys domain.Hotkeys, callback func(ShortcutEvent)) error {
	desktop.mutex.Lock()
	defer desktop.mutex.Unlock()
	if desktop.closed {
		return errors.New("desktop integration is closed")
	}
	if desktop.hotkeysDegraded {
		return ErrHotkeysDegraded
	}
	if group, exists := desktop.bindings[copySequenceBindingName]; exists && group.binding.(copySequenceBinding).failure() != nil {
		return errors.New("repeated-copy listener stopped; restart the application before configuring shortcuts")
	}
	desired := make(map[string]registration)
	combinations := make(map[string]combination)
	shortcutItems := []struct{ action, value string }{{"open-ui", keys.OpenUI}}
	if desktop.fixedClipboardShortcuts {
		copyShortcut := copyShortcutForOS(runtime.GOOS)
		shortcutItems = append(shortcutItems,
			struct{ action, value string }{"translate-paste", copyShortcut + "*3"},
			struct{ action, value string }{"translate-ui", copyShortcut + "*4"},
		)
	}
	for _, item := range shortcutItems {
		key, err := normalizeHotkey(item.value)
		if err != nil {
			return err
		}
		if key.name == "" {
			continue
		}
		if key.repetitions > 1 {
			if item.action == "open-ui" {
				return errors.New("repeated copy gestures are fixed and cannot be assigned to open-WebUI")
			}
			group := desired[copySequenceBindingName]
			if group.repeatActions == nil {
				group.repeatActions = make(map[int]string)
			}
			if _, exists := group.repeatActions[key.repetitions]; exists {
				return errors.New("shortcuts must use distinct key combinations and repeat counts")
			}
			group.repeatActions[key.repetitions] = item.action
			desired[copySequenceBindingName] = group
			combinations[copySequenceBindingName] = combination{name: copySequenceBindingName}
			continue
		}
		if _, exists := desired[key.name]; exists {
			return errors.New("shortcuts must use distinct key combinations")
		}
		desired[key.name] = registration{action: item.action}
		combinations[key.name] = key
	}
	if _, repeated := desired[copySequenceBindingName]; repeated {
		if _, single := desired[copyShortcutForOS(runtime.GOOS)]; single {
			return errors.New("the copy shortcut conflicts with fixed repeated-copy gestures; change or disable the open-WebUI binding")
		}
	}
	added := make(map[string]registration)
	for name, item := range desired {
		if old, exists := desktop.bindings[name]; exists {
			item.binding = old.binding
			desired[name] = item
			continue
		}
		registered, err := desktop.registerCombination(combinations[name])
		if err != nil {
			desktop.lastError = "Shortcut registration failed; previous bindings retained"
			return errors.Join(err, desktop.discardStagedBindings(added))
		}
		item.binding = registered
		desired[name] = item
		added[name] = item
	}
	removed := make(map[string]registration)
	// Stable order also makes rollback behavior reproducible in diagnostics/tests.
	oldNames := make([]string, 0, len(desktop.bindings))
	for name := range desktop.bindings {
		oldNames = append(oldNames, name)
	}
	sort.Strings(oldNames)
	for _, name := range oldNames {
		old := desktop.bindings[name]
		if _, keep := desired[name]; !keep {
			if err := old.binding.close(); err != nil {
				// A failed close keeps its binding active. Re-register only those
				// already removed and retain the previous action map/callback.
				desktop.lastError = "Shortcut removal failed; previous bindings restored"
				rollbackError := desktop.discardStagedBindings(added)
				for removedName, previous := range removed {
					key, parseError := normalizeHotkey(removedName)
					if removedName == copySequenceBindingName {
						key, parseError = combination{name: copySequenceBindingName}, nil
					}
					var restored binding
					var restoreError error
					if parseError == nil {
						restored, restoreError = desktop.registerCombination(key)
					} else {
						restoreError = parseError
					}
					if restoreError != nil {
						rollbackError = errors.Join(rollbackError, restoreError, desktop.disableHotkeys())
						continue
					}
					previous.binding = restored
					desktop.bindings[removedName] = previous
					if removedName == copySequenceBindingName {
						desktop.sequenceGeneration++
						restored.(copySequenceBinding).activate(desktop.sequenceGeneration)
					}
				}
				return errors.Join(err, rollbackError)
			}
			removed[name] = old
		}
	}
	desktop.bindings = desired
	desktop.callback = callback
	desktop.sequenceGeneration++
	if group, exists := desired[copySequenceBindingName]; exists {
		group.binding.(copySequenceBinding).activate(desktop.sequenceGeneration)
	}
	desktop.lastError = ""
	return nil
}

func (desktop *Desktop) registerCombination(key combination) (binding, error) {
	if key.name == copySequenceBindingName {
		return desktop.registerSequence(desktop.invokeCopySequence)
	}
	return desktop.register(key, func() { desktop.invoke(key.name) })
}

func (desktop *Desktop) invokeCopySequence(trigger copyTrigger, generation uint64) {
	desktop.mutex.Lock()
	group, exists := desktop.bindings[copySequenceBindingName]
	action := group.repeatActions[trigger.repetitions]
	callback := desktop.callback
	active := exists && !desktop.closed && !desktop.hotkeysDegraded && generation == desktop.sequenceGeneration
	if active {
		active = group.binding.(copySequenceBinding).failure() == nil
	}
	event := ShortcutEvent{Action: action}
	// A recognized gesture without a completed copy is a visible capture failure,
	// not permission to submit the previous clipboard contents.
	if action != "" && action != "open-ui" && !trigger.clipboardUpdated {
		event.CaptureError = describeNativeError(errors.New("repeated copy was received, but the clipboard did not update after the final copy; no translation was submitted"))
	}
	if active && action != "" && action != "open-ui" && callback != nil && event.CaptureError == nil {
		var capture ClipboardCapture
		var err error
		if native, supported := desktop.native.(copyCaptureDesktop); supported {
			capture, err = native.captureCopy(trigger)
		} else {
			err = ErrUnsupported
		}
		event.CaptureError = err
		if err == nil {
			capture.owner = desktop
			capture.directPaste = (runtime.GOOS == "windows" || runtime.GOOS == "darwin") && action == "translate-paste" && trigger.repetitions == 3
			event.Capture = &capture
		}
	}
	desktop.mutex.Unlock()
	if active && action != "" && callback != nil {
		callback(event)
	}
}

func (desktop *Desktop) disableHotkeys() error {
	desktop.hotkeysDegraded = true
	desktop.lastError = ErrHotkeysDegraded.Error()
	return ErrHotkeysDegraded
}

func (desktop *Desktop) discardStagedBindings(added map[string]registration) error {
	var cleanupError error
	for _, staged := range added {
		if err := staged.binding.close(); err != nil {
			desktop.orphanedBindings = append(desktop.orphanedBindings, staged.binding)
			cleanupError = errors.Join(cleanupError, err, desktop.disableHotkeys())
		}
	}
	return cleanupError
}

func (desktop *Desktop) invoke(name string) {
	desktop.mutex.Lock()
	item, exists := desktop.bindings[name]
	callback := desktop.callback
	closed := desktop.closed || desktop.hotkeysDegraded
	event := ShortcutEvent{Action: item.action}
	desktop.mutex.Unlock()
	if exists && !closed && callback != nil {
		callback(event)
	}
}

func (desktop *Desktop) CaptureClipboard() (ClipboardCapture, error) {
	desktop.mutex.Lock()
	defer desktop.mutex.Unlock()
	if desktop.closed {
		return ClipboardCapture{}, errors.New("desktop integration is closed")
	}
	capture, err := desktop.native.capture()
	if err != nil {
		return ClipboardCapture{}, err
	}
	capture.owner = desktop
	return capture, nil
}

func (desktop *Desktop) Paste(ctx context.Context, capture ClipboardCapture, text string) error {
	if capture.owner != desktop {
		return errors.New("paste capture does not belong to this desktop")
	}
	validateCapture := func() error { return desktop.native.validate(capture.target, capture.version) }
	dispatchPaste := func(version uint64) error { return desktop.native.dispatch(ctx, capture.target, version) }
	validationPhase := "target/clipboard validation before clipboard write"
	if capture.directPaste {
		native, supported := desktop.native.(directPasteDesktop)
		if (runtime.GOOS != "windows" && runtime.GOOS != "darwin") || !supported {
			return ErrUnsupported
		}
		validateCapture = func() error { return native.validateClipboard(capture.version) }
		dispatchPaste = func(version uint64) error { return native.dispatchDirect(ctx, version) }
		validationPhase = "clipboard validation before experimental direct paste"
	} else {
		if capture.pasteError != nil {
			return capture.pasteError
		}
		if capture.target == "" {
			return describeNativeError(errors.New("no verified editable text/selection target was available at capture"))
		}
	}
	// Do not hold the desktop lock while waiting for the shortcut to be released.
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(15 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		desktop.mutex.Lock()
		if desktop.closed {
			desktop.mutex.Unlock()
			return errors.New("desktop integration is closed")
		}
		err := describeNativeOperation(validationPhase, validateCapture())
		released := err == nil && desktop.native.modifiersReleased()
		if err == nil && released {
			if err = ctx.Err(); err == nil {
				var writtenVersion uint64
				writtenVersion, err = desktop.native.write(capture.version, text)
				err = describeNativeOperation("publishing translation to clipboard", err)
				if err == nil {
					err = ctx.Err()
				}
				if err == nil {
					err = describeNativeOperation("paste dispatch after clipboard write", dispatchPaste(writtenVersion))
				}
			}
		}
		desktop.mutex.Unlock()
		if err != nil || released {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("release shortcut modifiers before pasting")
		case <-ticker.C:
		}
	}
}

func (desktop *Desktop) Close() error {
	desktop.mutex.Lock()
	defer desktop.mutex.Unlock()
	if desktop.closed {
		return nil
	}
	desktop.closed = true
	var closeError error
	for _, item := range desktop.bindings {
		closeError = errors.Join(closeError, item.binding.close())
	}
	for _, orphaned := range desktop.orphanedBindings {
		closeError = errors.Join(closeError, orphaned.close())
	}
	desktop.orphanedBindings = nil
	desktop.bindings = nil
	return errors.Join(closeError, desktop.native.close())
}

func OpenBrowser(address string) error {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || strings.ContainsAny(address, "\x00\r\n") {
		return errors.New("browser address must be an absolute HTTP(S) URL")
	}
	return openBrowser(address)
}

type Credentials struct{ namespace string }

func NewCredentials(namespace string) *Credentials { return &Credentials{namespace: namespace} }
func (credentials *Credentials) validate(id string) error {
	if strings.TrimSpace(credentials.namespace) == "" || strings.TrimSpace(id) == "" || strings.ContainsRune(credentials.namespace+id, 0) || !utf8.ValidString(credentials.namespace) || !utf8.ValidString(id) {
		return errors.New("credential namespace and ID must be nonempty UTF-8 and contain no NUL")
	}
	return nil
}
func (credentials *Credentials) Get(id string) (string, error) {
	if err := credentials.validate(id); err != nil {
		return "", err
	}
	return getCredential(credentials.namespace, id)
}
func (credentials *Credentials) Set(id, value string) error {
	if err := credentials.validate(id); err != nil {
		return err
	}
	return setCredential(credentials.namespace, id, value)
}
func (credentials *Credentials) Delete(id string) error {
	if err := credentials.validate(id); err != nil {
		return err
	}
	return deleteCredential(credentials.namespace, id)
}
func (credentials *Credentials) Has(id string) (bool, error) {
	if err := credentials.validate(id); err != nil {
		return false, err
	}
	return hasCredential(credentials.namespace, id)
}
