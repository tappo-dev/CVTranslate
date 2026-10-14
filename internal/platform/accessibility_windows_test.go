//go:build windows && (amd64 || arm64)

package platform

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf16"
	"unsafe"
)

// These tests exercise only injected snapshots and pure validation. They never
// instantiate COM, resolve a DLL, inspect windows, read clipboard or send input.
type mockAccessibilityReader struct {
	identity        accessibilityIdentity
	snapshots       []accessibilitySnapshot
	readCount       int
	inspectionCount int
	inspectHook     func(int, accessibilityIdentity) (accessibilityIdentity, error)
	readError       error
}

func (reader *mockAccessibilityReader) inspect() (accessibilityIdentity, error) {
	reader.inspectionCount++
	if reader.inspectHook != nil {
		return reader.inspectHook(reader.inspectionCount, reader.identity)
	}
	return reader.identity, nil
}

func (reader *mockAccessibilityReader) read(accessibilityIdentity) (accessibilitySnapshot, error) {
	reader.readCount++
	if reader.readError != nil {
		return accessibilitySnapshot{}, reader.readError
	}
	return reader.snapshots[min(reader.readCount-1, len(reader.snapshots)-1)], nil
}

func newMockAccessibilityReader(t *testing.T) *mockAccessibilityReader {
	t.Helper()
	snapshot, err := assembleAccessibilitySnapshot(utf16.Encode([]rune("private selected tail")), utf16.Encode([]rune("selected")), utf16.Encode([]rune("private ")), utf16.Encode([]rune("private selected")), true)
	if err != nil {
		t.Fatal(err)
	}
	return &mockAccessibilityReader{
		identity:  accessibilityIdentity{Foreground: 100, ProcessID: 200, RootRuntimeID: "aabb", RuntimeID: "ccdd", PasswordKnown: true, Enabled: true, Focused: true},
		snapshots: []accessibilitySnapshot{snapshot},
	}
}

func TestAccessibilityRequiresSafeIdentityBeforeText(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(*accessibilityIdentity)
	}{
		{"password", func(identity *accessibilityIdentity) { identity.Password = true }},
		{"unknown password", func(identity *accessibilityIdentity) { identity.PasswordKnown = false }},
		{"disabled", func(identity *accessibilityIdentity) { identity.Enabled = false }},
		{"unfocused", func(identity *accessibilityIdentity) { identity.Focused = false }},
		{"different foreground", func(identity *accessibilityIdentity) { identity.Foreground++ }},
		{"missing process", func(identity *accessibilityIdentity) { identity.ProcessID = 0 }},
		{"missing runtime ID", func(identity *accessibilityIdentity) { identity.RuntimeID = "" }},
		{"missing root", func(identity *accessibilityIdentity) { identity.RootRuntimeID = "" }},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			reader := newMockAccessibilityReader(t)
			scenario.change(&reader.identity)
			target, err := readAccessibilityTarget(reader, 100)
			if err == nil || target != "" || reader.readCount != 0 {
				t.Fatalf("unsafe identity accessed text: target=%q reads=%d err=%v", target, reader.readCount, err)
			}
		})
	}
}

func TestAccessibilityReadOnlyControlHasNoPasteTarget(t *testing.T) {
	reader := newMockAccessibilityReader(t)
	reader.snapshots[0].Editable = false
	if target, err := readAccessibilityTarget(reader, 100); err == nil || target != "" {
		t.Fatal("read-only target was accepted for automatic paste")
	}
}

func TestAccessibilityInspectionProducesStablePrivateTarget(t *testing.T) {
	firstTarget, err := readAccessibilityTarget(newMockAccessibilityReader(t), 100)
	if err != nil || !strings.HasPrefix(firstTarget, "uia:") {
		t.Fatalf("inspection failed: %q %v", firstTarget, err)
	}
	inspectedTarget, err := readAccessibilityTarget(newMockAccessibilityReader(t), 100)
	if err != nil || inspectedTarget != firstTarget {
		t.Fatalf("inspection did not reproduce target: %q %v", inspectedTarget, err)
	}
	for _, privateText := range []string{"private", "selected", "tail"} {
		if strings.Contains(firstTarget, privateText) {
			t.Fatal("target contains document contents")
		}
	}
}

func TestAccessibilityRejectsChangesAcrossSnapshots(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(*accessibilitySnapshot)
	}{
		{"document", func(snapshot *accessibilitySnapshot) { snapshot.DocumentDigest[0]++ }},
		{"selection digest", func(snapshot *accessibilitySnapshot) { snapshot.SelectedDigest[0]++ }},
		{"caret", func(snapshot *accessibilitySnapshot) { snapshot.SelectionStart++ }},
		{"prefix", func(snapshot *accessibilitySnapshot) { snapshot.StartDigest[0]++ }},
		{"read-only transition", func(snapshot *accessibilitySnapshot) { snapshot.Editable = false }},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			reader := newMockAccessibilityReader(t)
			changed := reader.snapshots[0]
			scenario.change(&changed)
			reader.snapshots = append(reader.snapshots, changed)
			target, err := readAccessibilityTarget(reader, 100)
			if err == nil || target != "" {
				t.Fatal("changed snapshot escaped capture guards")
			}
		})
	}
}

func TestAccessibilityRejectsFocusChangesAndProviderFailure(t *testing.T) {
	for _, mutate := range []func(*accessibilityIdentity){
		func(identity *accessibilityIdentity) { identity.RuntimeID += "ee" },
		func(identity *accessibilityIdentity) { identity.RootRuntimeID += "ee" },
		func(identity *accessibilityIdentity) { identity.ProcessID++ },
		func(identity *accessibilityIdentity) { identity.Foreground++ },
		func(identity *accessibilityIdentity) { identity.Password = true },
	} {
		for changeAt := 2; changeAt <= 4; changeAt++ {
			reader := newMockAccessibilityReader(t)
			reader.inspectHook = func(count int, identity accessibilityIdentity) (accessibilityIdentity, error) {
				if count >= changeAt {
					mutate(&identity)
				}
				return identity, nil
			}
			target, err := readAccessibilityTarget(reader, 100)
			if err == nil || target != "" {
				t.Fatal("changed focused element escaped capture guards")
			}
		}
	}
	reader := newMockAccessibilityReader(t)
	reader.readError = errors.New("provider timeout")
	if target, err := readAccessibilityTarget(reader, 100); err == nil || target != "" {
		t.Fatal("provider failure returned a usable capture")
	}
}

func TestAccessibilityCaretSupportsPasteTargetInspection(t *testing.T) {
	reader := newMockAccessibilityReader(t)
	snapshot, err := assembleAccessibilitySnapshot([]uint16{'a', 'b'}, nil, []uint16{'a'}, []uint16{'a'}, true)
	if err != nil {
		t.Fatal(err)
	}
	reader.snapshots = []accessibilitySnapshot{snapshot}
	if target, err := readAccessibilityTarget(reader, 100); err != nil || target == "" {
		t.Fatalf("safe caret was rejected for paste: %v", err)
	}
}

func TestAccessibilityRangePrefixesDisambiguateRepeatedText(t *testing.T) {
	document := utf16.Encode([]rune("same same"))
	first, err := assembleAccessibilitySnapshot(document, document[:4], nil, document[:4], true)
	if err != nil {
		t.Fatal(err)
	}
	second, err := assembleAccessibilitySnapshot(document, document[5:], document[:5], document, true)
	if err != nil {
		t.Fatal(err)
	}
	if first.SelectedDigest != second.SelectedDigest || first.SelectionStart == second.SelectionStart || first.StartDigest == second.StartDigest {
		t.Fatal("range prefixes did not distinguish repeated selected text")
	}
	if _, err := assembleAccessibilitySnapshot(document, document[:4], document[:5], document[:4], true); err == nil {
		t.Fatal("reversed endpoints accepted")
	}
	if _, err := assembleAccessibilitySnapshot(document, []uint16{'x'}, nil, document[:4], true); err == nil {
		t.Fatal("selected text inconsistent with range accepted")
	}
	if _, err := assembleAccessibilitySnapshot(document, document[:4], []uint16{'x'}, document[:5], true); err == nil {
		t.Fatal("prefix inconsistent with document accepted")
	}
}

func TestAccessibilityRejectsMalformedOrTruncatedText(t *testing.T) {
	for _, characters := range [][]uint16{{0}, {0xd800}, {0xdc00}, {'a', 0xd800, 'b'}, make([]uint16, maximumTargetCharacters+1)} {
		if err := validateAccessibilityText(characters); err == nil {
			t.Fatal("malformed or oversized UTF-16 accepted")
		}
	}
	if err := validateAccessibilityText([]uint16{0xd83d, 0xde00}); err != nil {
		t.Fatalf("valid surrogate pair rejected: %v", err)
	}
}

func TestAccessibilityBooleanDoesNotDefaultUnsupportedPasswordToFalse(t *testing.T) {
	for _, value := range []accessibilityVariant{{}, {Type: 13}, {Type: 3}, {Type: 11, Value: 1}} {
		if _, err := accessibilityBoolean(value); err == nil {
			t.Fatal("unknown or mistyped property became a definite false value")
		}
	}
	for _, value := range []uint64{0, 0xffff} {
		result, err := accessibilityBoolean(accessibilityVariant{Type: 11, Value: value})
		if err != nil || result != (value == 0xffff) {
			t.Fatal("canonical VARIANT_BOOL rejected")
		}
	}
	// This is an ABI safety assertion, not a native call. A 16-byte VARIANT
	// would let Windows overwrite Go memory on both supported architectures.
	if unsafe.Sizeof(accessibilityVariant{}) != 24 || unsafe.Offsetof(accessibilityVariant{}.Value) != 8 {
		t.Fatal("incorrect 64-bit VARIANT ABI")
	}
}

func TestAccessibilityProviderFailuresAreNotMissingPatterns(t *testing.T) {
	for _, failure := range []error{
		accessibilityCOMError(0x80040201), // UIA_E_ELEMENTNOTAVAILABLE
		accessibilityCOMError(0x80131505), // UIA_E_TIMEOUT
		accessibilityCOMError(0x80004005), // E_FAIL
		errors.New("inspection budget expired"),
	} {
		if accessibilityPatternUnsupported(failure) {
			t.Fatal("provider failure was downgraded to an optional missing pattern")
		}
	}
	for _, failure := range []error{accessibilityCOMError(0x80004002), accessibilityCOMError(0x80040204)} {
		if !accessibilityPatternUnsupported(failure) {
			t.Fatal("a genuinely unavailable optional ValuePattern was not recognized")
		}
	}
}
