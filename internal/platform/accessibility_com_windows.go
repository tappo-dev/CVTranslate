//go:build windows && (amd64 || arm64)

package platform

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ABI source: Microsoft's Windows SDK UIAutomationClient.h/.idl and oaidl.h:
// https://github.com/microsoft/win32metadata/tree/main/generation/WinSDK/RecompiledIdlHeaders/um
// Slots below include IUnknown (0..2). IUIAutomation has slots 3..57;
// IUIAutomation2 appends AutoSetFocus get/put 58/59, ConnectionTimeout
// get/put 60/61 and TransactionTimeout get/put 62/63. BOOL is int32;
// VARIANT_BOOL is int16; the amd64/arm64 VARIANT has size 24, not 16.
const (
	accessibilityCompareElements               = 3
	accessibilityElementFromHandle             = 6
	accessibilityGetFocusedElement             = 8
	accessibilityRawViewWalker                 = 16
	accessibilitySetAutoFocus                  = 59
	accessibilitySetConnectionTimeout          = 61
	accessibilitySetTransactionTimeout         = 63
	accessibilityGetRuntimeID                  = 4
	accessibilityGetProperty                   = 11 // GetCurrentPropertyValueEx(ignoreDefaultValue=TRUE)
	accessibilityGetPattern                    = 14 // GetCurrentPatternAs: typed interface, not IUnknown
	accessibilityTextPatternID                 = 10014
	accessibilityValuePatternID                = 10002
	accessibilityProcessProperty               = 30002
	accessibilityFocusProperty                 = 30008
	accessibilityEnabledProperty               = 30010
	accessibilityPasswordProperty              = 30019
	accessibilityReadOnlyAttribute             = 40015
	accessibilityCallTimeout                   = 75 * time.Millisecond
	accessibilityInspectionBudget              = 2 * time.Second
	accessibilityMaximumRuntimeID              = 64
	accessibilityRangeStart            uintptr = 0
	accessibilityRangeEnd              uintptr = 1
)

var (
	accessibilityOle32   = windows.NewLazySystemDLL("ole32.dll")
	accessibilityOleAut  = windows.NewLazySystemDLL("oleaut32.dll")
	accessibilityClass   = windows.GUID{Data1: 0xe22ad333, Data2: 0xb25f, Data3: 0x460c, Data4: [8]byte{0x83, 0xd0, 0x05, 0x81, 0x10, 0x73, 0x95, 0xc9}} // CUIAutomation8
	accessibilityClient  = windows.GUID{Data1: 0x34723aff, Data2: 0x0c9d, Data3: 0x49d0, Data4: [8]byte{0x98, 0x96, 0x7a, 0xb5, 0x2d, 0xf8, 0xcd, 0x8a}} // IUIAutomation2
	accessibilityText    = windows.GUID{Data1: 0x32eba289, Data2: 0x3583, Data3: 0x42c9, Data4: [8]byte{0x9c, 0x59, 0x3b, 0x6d, 0x9a, 0x1e, 0x9b, 0x6a}} // IUIAutomationTextPattern
	accessibilityValue   = windows.GUID{Data1: 0xa94cd8b1, Data2: 0x0844, Data3: 0x4cd6, Data4: [8]byte{0x9d, 0x2d, 0x64, 0x05, 0x37, 0xab, 0x39, 0xe9}} // IUIAutomationValuePattern
	accessibilityUnknown = windows.GUID{Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}                                                                    // IID_IUnknown
)

type accessibilityObject struct{ methods *[64]uintptr }
type accessibilitySafeArray struct{ unused byte }

type accessibilityVariant struct {
	Type     uint16
	Reserved [3]uint16
	Value    uint64
	Extra    uint64
}

type accessibilityCOMError uint32

func (failure accessibilityCOMError) Error() string {
	return fmt.Sprintf("UI Automation request failed (HRESULT 0x%08x)", uint32(failure))
}

func accessibilityHRESULT(result uintptr) error {
	if int32(result) < 0 {
		return accessibilityCOMError(uint32(result))
	}
	return nil
}

func accessibilityPatternUnsupported(err error) bool {
	// E_NOINTERFACE and UIA_E_NOTSUPPORTED, never a timeout/provider failure.
	return errors.Is(err, accessibilityCOMError(0x80004002)) || errors.Is(err, accessibilityCOMError(0x80040204))
}

func (object *accessibilityObject) release() {
	if object != nil {
		syscall.SyscallN(object.methods[2], uintptr(unsafe.Pointer(object)))
	}
}

// uintptr arguments are native pointers at call sites. Keep their Go storage
// alive and immovable throughout the wrapper, including before SyscallN.
//
//go:uintptrescapes
func invokeAccessibility(object *accessibilityObject, slot int, arguments ...uintptr) error {
	if object == nil {
		return errors.New("UI Automation returned a missing interface")
	}
	parameters := append([]uintptr{uintptr(unsafe.Pointer(object))}, arguments...)
	result, _, _ := syscall.SyscallN(object.methods[slot], parameters...)
	runtime.KeepAlive(object)
	return accessibilityHRESULT(result)
}

type windowsAccessibilityReader struct {
	client     *accessibilityObject
	foreground uintptr
	deadline   time.Time
}

func (reader *windowsAccessibilityReader) check() error {
	if time.Until(reader.deadline) < accessibilityCallTimeout {
		return errors.New("UI Automation inspection budget expired")
	}
	foreground, _, _ := user32.NewProc("GetForegroundWindow").Call()
	if foreground == 0 || foreground != reader.foreground {
		return errors.New("foreground changed during UI Automation inspection")
	}
	return nil
}

//go:uintptrescapes
func (reader *windowsAccessibilityReader) call(object *accessibilityObject, slot int, arguments ...uintptr) error {
	if err := reader.check(); err != nil {
		return err
	}
	if err := invokeAccessibility(object, slot, arguments...); err != nil {
		return err
	}
	return reader.check()
}

func withWindowsAccessibility(foreground uintptr) (string, error) {
	// An operation owns one MTA thread and releases all interfaces before
	// CoUninitialize. There are no timer goroutines or abandoned COM calls.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	reader := windowsAccessibilityReader{foreground: foreground, deadline: time.Now().Add(accessibilityInspectionBudget)}
	if err := reader.check(); err != nil {
		return "", err
	}
	initialized, _, _ := accessibilityOle32.NewProc("CoInitializeEx").Call(0, 0) // COINIT_MULTITHREADED
	if err := accessibilityHRESULT(initialized); err != nil {
		return "", fmt.Errorf("initialize UI Automation COM apartment: %w", err) // Includes RPC_E_CHANGED_MODE.
	}
	defer accessibilityOle32.NewProc("CoUninitialize").Call()
	created, _, _ := accessibilityOle32.NewProc("CoCreateInstance").Call(
		uintptr(unsafe.Pointer(&accessibilityClass)), 0, 1, // CLSCTX_INPROC_SERVER
		uintptr(unsafe.Pointer(&accessibilityClient)), uintptr(unsafe.Pointer(&reader.client)))
	defer reader.client.release()
	if err := accessibilityHRESULT(created); err != nil {
		return "", fmt.Errorf("create UI Automation client: %w", err)
	}
	// Require IUIAutomation2's timeouts. Never fall back to an unbounded legacy
	// client. AutoSetFocus is explicitly disabled, although no action API is used.
	for _, setting := range []struct{ slot, value int }{
		{accessibilitySetAutoFocus, 0},
		{accessibilitySetConnectionTimeout, int(accessibilityCallTimeout / time.Millisecond)},
		{accessibilitySetTransactionTimeout, int(accessibilityCallTimeout / time.Millisecond)},
	} {
		if err := reader.call(reader.client, setting.slot, uintptr(setting.value)); err != nil {
			return "", fmt.Errorf("configure UI Automation client timeouts/focus policy: %w", err)
		}
	}
	return readAccessibilityTarget(&reader, foreground)
}

func clearAccessibilityVariant(value *accessibilityVariant) {
	accessibilityOleAut.NewProc("VariantClear").Call(uintptr(unsafe.Pointer(value)))
}

func accessibilityBoolean(value accessibilityVariant) (bool, error) {
	if value.Type != 11 || (uint16(value.Value) != 0 && uint16(value.Value) != 0xffff) { // VT_BOOL
		return false, errors.New("UI Automation did not supply a definite Boolean property")
	}
	return uint16(value.Value) == 0xffff, nil
}

func (reader *windowsAccessibilityReader) booleanProperty(element *accessibilityObject, property uintptr) (bool, error) {
	propertyName := "Boolean property"
	switch property {
	case accessibilityPasswordProperty:
		propertyName = "IsPassword"
	case accessibilityEnabledProperty:
		propertyName = "IsEnabled"
	case accessibilityFocusProperty:
		propertyName = "HasKeyboardFocus"
	}
	var value accessibilityVariant
	defer clearAccessibilityVariant(&value)
	if err := reader.call(element, accessibilityGetProperty, property, 1, uintptr(unsafe.Pointer(&value))); err != nil {
		return false, fmt.Errorf("read UI Automation %s: %w", propertyName, err)
	}
	result, err := accessibilityBoolean(value)
	if err != nil {
		return false, fmt.Errorf("read UI Automation %s: %w", propertyName, err)
	}
	return result, nil
}

func (reader *windowsAccessibilityReader) processProperty(element *accessibilityObject) (uint32, error) {
	var value accessibilityVariant
	defer clearAccessibilityVariant(&value)
	if err := reader.call(element, accessibilityGetProperty, accessibilityProcessProperty, 1, uintptr(unsafe.Pointer(&value))); err != nil {
		return 0, err
	}
	if value.Type != 3 || int32(value.Value) <= 0 { // VT_I4
		return 0, errors.New("UI Automation process identity is unavailable")
	}
	return uint32(value.Value), nil
}

func (reader *windowsAccessibilityReader) runtimeID(element *accessibilityObject) (string, error) {
	var array *accessibilitySafeArray
	err := reader.call(element, accessibilityGetRuntimeID, uintptr(unsafe.Pointer(&array)))
	if array != nil {
		defer accessibilityOleAut.NewProc("SafeArrayDestroy").Call(uintptr(unsafe.Pointer(array)))
	}
	if err != nil {
		return "", err
	}
	if array == nil {
		return "", errors.New("UI Automation runtime ID is missing")
	}
	dimensions, _, _ := accessibilityOleAut.NewProc("SafeArrayGetDim").Call(uintptr(unsafe.Pointer(array)))
	if dimensions != 1 {
		return "", errors.New("UI Automation runtime ID dimensions are invalid")
	}
	var elementType uint16
	result, _, _ := accessibilityOleAut.NewProc("SafeArrayGetVartype").Call(uintptr(unsafe.Pointer(array)), uintptr(unsafe.Pointer(&elementType)))
	if err := accessibilityHRESULT(result); err != nil {
		return "", err
	}
	if elementType != 3 {
		return "", errors.New("UI Automation runtime ID type is invalid")
	}
	var lowerBound, upperBound int32
	result, _, _ = accessibilityOleAut.NewProc("SafeArrayGetLBound").Call(uintptr(unsafe.Pointer(array)), 1, uintptr(unsafe.Pointer(&lowerBound)))
	if err := accessibilityHRESULT(result); err != nil {
		return "", err
	}
	result, _, _ = accessibilityOleAut.NewProc("SafeArrayGetUBound").Call(uintptr(unsafe.Pointer(array)), 1, uintptr(unsafe.Pointer(&upperBound)))
	if err := accessibilityHRESULT(result); err != nil {
		return "", err
	}
	length := int64(upperBound) - int64(lowerBound) + 1
	if length < 1 || length > accessibilityMaximumRuntimeID {
		return "", errors.New("UI Automation runtime ID length is invalid")
	}
	encoded := make([]byte, int(length)*4)
	for offset := int64(0); offset < length; offset++ {
		index := int32(int64(lowerBound) + offset)
		var component int32
		result, _, _ = accessibilityOleAut.NewProc("SafeArrayGetElement").Call(uintptr(unsafe.Pointer(array)), uintptr(unsafe.Pointer(&index)), uintptr(unsafe.Pointer(&component)))
		if err := accessibilityHRESULT(result); err != nil {
			return "", err
		}
		binary.LittleEndian.PutUint32(encoded[int(offset)*4:], uint32(component))
	}
	return hex.EncodeToString(encoded), nil
}

func (reader *windowsAccessibilityReader) inspect() (accessibilityIdentity, error) {
	identity, element, err := reader.focusedElement()
	defer element.release()
	return identity, err
}

func (reader *windowsAccessibilityReader) focusedElement() (accessibilityIdentity, *accessibilityObject, error) {
	identity := accessibilityIdentity{Foreground: reader.foreground}
	if err := reader.check(); err != nil {
		return identity, nil, err
	}
	var processID uint32
	threadID, _, _ := user32.NewProc("GetWindowThreadProcessId").Call(reader.foreground, uintptr(unsafe.Pointer(&processID)))
	if threadID == 0 || processID == 0 || processID == windows.GetCurrentProcessId() {
		return identity, nil, errors.New("UI Automation foreground process is unavailable or belongs to this application")
	}
	var element *accessibilityObject
	if err := reader.call(reader.client, accessibilityGetFocusedElement, uintptr(unsafe.Pointer(&element))); err != nil {
		return identity, element, fmt.Errorf("get UI Automation focused element: %w", err)
	}
	password, err := reader.booleanProperty(element, accessibilityPasswordProperty)
	if err != nil {
		return identity, element, err
	}
	identity.PasswordKnown, identity.Password = true, password
	if password {
		return identity, element, errPasswordTarget
	}
	identity.Enabled, err = reader.booleanProperty(element, accessibilityEnabledProperty)
	if err != nil {
		return identity, element, err
	}
	identity.Focused, err = reader.booleanProperty(element, accessibilityFocusProperty)
	if err != nil {
		return identity, element, err
	}
	identity.ProcessID, err = reader.processProperty(element)
	if err != nil {
		return identity, element, err
	}
	if identity.ProcessID != processID {
		return identity, element, errors.New("UI Automation focus belongs to a different process")
	}
	identity.RuntimeID, err = reader.runtimeID(element)
	if err != nil {
		return identity, element, err
	}
	var root *accessibilityObject
	err = reader.call(reader.client, accessibilityElementFromHandle, reader.foreground, uintptr(unsafe.Pointer(&root)))
	defer root.release()
	if err != nil {
		return identity, element, err
	}
	rootProcess, err := reader.processProperty(root)
	if err != nil {
		return identity, element, err
	}
	if rootProcess != processID {
		return identity, element, errors.New("UI Automation foreground root identity disagrees")
	}
	identity.RootRuntimeID, err = reader.runtimeID(root)
	if err != nil {
		return identity, element, err
	}
	if err := reader.requireDescendant(element, root); err != nil {
		return identity, element, err
	}
	return identity, element, validateAccessibilityIdentity(identity, reader.foreground)
}

func (reader *windowsAccessibilityReader) requireDescendant(element, root *accessibilityObject) error {
	var walker *accessibilityObject
	err := reader.call(reader.client, accessibilityRawViewWalker, uintptr(unsafe.Pointer(&walker)))
	defer walker.release()
	if err != nil {
		return err
	}
	ancestor := element // Borrow the first element; subsequent parents are owned.
	var owned *accessibilityObject
	defer func() { owned.release() }()
	for depth := 0; depth < 64; depth++ {
		var same int32
		if err := reader.call(reader.client, accessibilityCompareElements, uintptr(unsafe.Pointer(ancestor)), uintptr(unsafe.Pointer(root)), uintptr(unsafe.Pointer(&same))); err != nil {
			return err
		}
		if same != 0 {
			return nil
		}
		var parent *accessibilityObject
		err := reader.call(walker, 3, uintptr(unsafe.Pointer(ancestor)), uintptr(unsafe.Pointer(&parent))) // IUIAutomationTreeWalker::GetParentElement
		owned.release()
		owned = parent
		if err != nil {
			return err
		}
		if parent == nil {
			break
		}
		ancestor = parent
	}
	return errors.New("UI Automation focus is not a bounded descendant of the foreground window")
}

func (reader *windowsAccessibilityReader) readText(textRange *accessibilityObject, expected accessibilityIdentity) ([]uint16, error) {
	// Re-obtain real focus and IsPassword before EVERY GetText, not just once
	// when discovering the element. Cross-process UIA is still not atomic.
	identity, err := reader.inspect()
	if err != nil {
		return nil, err
	}
	if identity != expected {
		return nil, errors.New("UI Automation identity changed before text access")
	}
	var text *uint16
	err = reader.call(textRange, 12, maximumTargetCharacters+1, uintptr(unsafe.Pointer(&text))) // IUIAutomationTextRange::GetText
	if text != nil {
		defer accessibilityOleAut.NewProc("SysFreeString").Call(uintptr(unsafe.Pointer(text)))
	}
	if err != nil {
		return nil, err
	}
	if text == nil {
		return nil, nil // A null BSTR is the valid empty string.
	}
	length, _, _ := accessibilityOleAut.NewProc("SysStringLen").Call(uintptr(unsafe.Pointer(text)))
	byteLength, _, _ := accessibilityOleAut.NewProc("SysStringByteLen").Call(uintptr(unsafe.Pointer(text)))
	if length > maximumTargetCharacters || byteLength != length*2 {
		return nil, errors.New("UI Automation text was truncated, oversized or malformed")
	}
	characters := unsafe.Slice(text, int(length))
	defer clear(characters)
	if err := validateAccessibilityText(characters); err != nil {
		return nil, err
	}
	return append([]uint16(nil), characters...), nil
}

func (reader *windowsAccessibilityReader) compareEndpoints(source *accessibilityObject, sourceEndpoint uintptr, target *accessibilityObject, targetEndpoint uintptr) (int32, error) {
	var comparison int32
	err := reader.call(source, 5, sourceEndpoint, uintptr(unsafe.Pointer(target)), targetEndpoint, uintptr(unsafe.Pointer(&comparison))) // IUIAutomationTextRange::CompareEndpoints
	return comparison, err
}

func validateAccessibilityRangeBounds(document, selection *accessibilityObject, compare func(*accessibilityObject, uintptr, *accessibilityObject, uintptr) (int32, error)) error {
	// CompareEndpoints returns positive when the source endpoint is LATER.
	// Preserve document.Start <= selection.Start <= selection.End <= document.End;
	// never clamp an out-of-scope selection or substitute an ancestor's document.
	for _, boundary := range []struct {
		source, target                 *accessibilityObject
		sourceEndpoint, targetEndpoint uintptr
		comparisonName, failureReason  string
	}{
		{document, selection, accessibilityRangeStart, accessibilityRangeStart, "document start versus selection start", "selection starts before the focused control's document"},
		{selection, selection, accessibilityRangeStart, accessibilityRangeEnd, "selection start versus selection end", "selection start is after its end"},
		{selection, document, accessibilityRangeEnd, accessibilityRangeEnd, "selection end versus document end", "selection ends after the focused control's document"},
	} {
		comparison, err := compare(boundary.source, boundary.sourceEndpoint, boundary.target, boundary.targetEndpoint)
		if err != nil {
			return fmt.Errorf("UI Automation range comparison (%s): %w", boundary.comparisonName, err)
		}
		if comparison > 0 {
			return fmt.Errorf("UI Automation range boundary rejected: %s", boundary.failureReason)
		}
	}
	return nil
}

func (reader *windowsAccessibilityReader) prefix(document, selection *accessibilityObject, endpoint uintptr, identity accessibilityIdentity) ([]uint16, error) {
	var prefix *accessibilityObject
	err := reader.call(document, 3, uintptr(unsafe.Pointer(&prefix))) // Clone
	defer prefix.release()
	if err != nil {
		return nil, err
	}
	if err := reader.call(prefix, 15, accessibilityRangeEnd, uintptr(unsafe.Pointer(selection)), endpoint); err != nil { // MoveEndpointByRange(End, selection, Start/End)
		return nil, err
	}
	return reader.readText(prefix, identity)
}

func (reader *windowsAccessibilityReader) editable(element, document *accessibilityObject) (bool, error) {
	var attribute accessibilityVariant
	defer clearAccessibilityVariant(&attribute)
	if err := reader.call(document, 9, accessibilityReadOnlyAttribute, uintptr(unsafe.Pointer(&attribute))); err != nil {
		return false, err
	}
	textReadOnly, textError := accessibilityBoolean(attribute)
	// VT_UNKNOWN includes UIA's reserved mixed/unsupported attribute values.
	// Only VT_BOOL false positively proves the complete range editable.
	if textError == nil && textReadOnly {
		return false, nil
	}
	if textError != nil {
		unsupported, err := reader.isUnsupportedAttribute(&attribute)
		if err != nil {
			return false, err
		}
		if !unsupported {
			// Mixed read-only ranges or malformed attributes cannot be overridden
			// by an element-wide ValuePattern.IsReadOnly=false declaration.
			return false, nil
		}
	}
	var valuePattern *accessibilityObject
	err := reader.call(element, accessibilityGetPattern, accessibilityValuePatternID, uintptr(unsafe.Pointer(&accessibilityValue)), uintptr(unsafe.Pointer(&valuePattern)))
	defer valuePattern.release()
	if err != nil && !accessibilityPatternUnsupported(err) {
		return false, err
	}
	if err != nil || valuePattern == nil {
		// TextPattern alone may prove editability; never read ValuePattern.Value.
		if checkError := reader.check(); checkError != nil {
			return false, checkError
		}
		return textError == nil && !textReadOnly, nil
	}
	var valueReadOnly int32 = -1
	if err := reader.call(valuePattern, 5, uintptr(unsafe.Pointer(&valueReadOnly))); err != nil { // CurrentIsReadOnly
		return false, err
	}
	return valueReadOnly == 0, nil
}

func (reader *windowsAccessibilityReader) isUnsupportedAttribute(attribute *accessibilityVariant) (bool, error) {
	if attribute.Type != 13 { // VT_UNKNOWN
		return false, nil
	}
	// Read the native VARIANT union as its declared IUnknown* member. This
	// avoids an unsafe integer-to-pointer reconstruction of a native pointer.
	attributeObject := *(**accessibilityObject)(unsafe.Pointer(&attribute.Value))
	if attributeObject == nil {
		return false, nil
	}
	var reserved, reservedIdentity, attributeIdentity *accessibilityObject
	err := reader.call(reader.client, 54, uintptr(unsafe.Pointer(&reserved))) // ReservedNotSupportedValue
	defer reserved.release()
	if err != nil {
		return false, err
	}
	err = reader.call(reserved, 0, uintptr(unsafe.Pointer(&accessibilityUnknown)), uintptr(unsafe.Pointer(&reservedIdentity))) // QueryInterface(IUnknown)
	defer reservedIdentity.release()
	if err != nil {
		return false, err
	}
	err = reader.call(attributeObject, 0, uintptr(unsafe.Pointer(&accessibilityUnknown)), uintptr(unsafe.Pointer(&attributeIdentity)))
	defer attributeIdentity.release()
	if err != nil {
		return false, err
	}
	return reservedIdentity != nil && reservedIdentity == attributeIdentity, nil
}

func (reader *windowsAccessibilityReader) read(expected accessibilityIdentity) (accessibilitySnapshot, error) {
	identity, element, err := reader.focusedElement()
	defer element.release()
	if err != nil {
		return accessibilitySnapshot{}, err
	}
	if identity != expected {
		return accessibilitySnapshot{}, errors.New("UI Automation focus changed before snapshot")
	}
	var pattern *accessibilityObject
	err = reader.call(element, accessibilityGetPattern, accessibilityTextPatternID, uintptr(unsafe.Pointer(&accessibilityText)), uintptr(unsafe.Pointer(&pattern)))
	defer pattern.release()
	if err != nil {
		return accessibilitySnapshot{}, fmt.Errorf("get focused input TextPattern: %w", err)
	}
	var ranges *accessibilityObject
	err = reader.call(pattern, 5, uintptr(unsafe.Pointer(&ranges))) // TextPattern::GetSelection
	defer ranges.release()
	if err != nil {
		return accessibilitySnapshot{}, fmt.Errorf("get focused input selection/caret ranges: %w", err)
	}
	var count int32
	if err := reader.call(ranges, 3, uintptr(unsafe.Pointer(&count))); err != nil {
		return accessibilitySnapshot{}, err
	}
	if count != 1 {
		return accessibilitySnapshot{}, errors.New("UI Automation requires exactly one selection or caret range")
	}
	var selection, document *accessibilityObject
	err = reader.call(ranges, 4, 0, uintptr(unsafe.Pointer(&selection))) // TextRangeArray::GetElement
	defer selection.release()
	if err != nil {
		return accessibilitySnapshot{}, err
	}
	err = reader.call(pattern, 7, uintptr(unsafe.Pointer(&document))) // TextPattern::DocumentRange
	defer document.release()
	if err != nil {
		return accessibilitySnapshot{}, err
	}
	if err := validateAccessibilityRangeBounds(document, selection, reader.compareEndpoints); err != nil {
		return accessibilitySnapshot{}, err
	}
	editable, err := reader.editable(element, document)
	if err != nil {
		return accessibilitySnapshot{}, err
	}
	documentText, err := reader.readText(document, identity)
	defer clear(documentText)
	if err != nil {
		return accessibilitySnapshot{}, err
	}
	selectedText, err := reader.readText(selection, identity)
	defer clear(selectedText)
	if err != nil {
		return accessibilitySnapshot{}, err
	}
	startPrefix, err := reader.prefix(document, selection, accessibilityRangeStart, identity)
	defer clear(startPrefix)
	if err != nil {
		return accessibilitySnapshot{}, err
	}
	endPrefix, err := reader.prefix(document, selection, accessibilityRangeEnd, identity)
	defer clear(endPrefix)
	if err != nil {
		return accessibilitySnapshot{}, err
	}
	return assembleAccessibilitySnapshot(documentText, selectedText, startPrefix, endPrefix, editable)
}
