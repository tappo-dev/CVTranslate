//go:build windows && (amd64 || arm64)

package platform

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
)

// UIA identities and document digests are transient paste guards, never source
// text. No UIA operation in this adapter changes focus, selection or content.
type accessibilityIdentity struct {
	Foreground    uintptr
	ProcessID     uint32
	RootRuntimeID string
	RuntimeID     string
	PasswordKnown bool
	Password      bool
	Enabled       bool
	Focused       bool
}

type accessibilitySnapshot struct {
	DocumentDigest [32]byte
	DocumentLength int
	StartDigest    [32]byte
	EndDigest      [32]byte
	SelectionStart int
	SelectionEnd   int
	SelectedDigest [32]byte
	Editable       bool
}

type accessibilityReader interface {
	inspect() (accessibilityIdentity, error)
	read(accessibilityIdentity) (accessibilitySnapshot, error)
}

func validateAccessibilityIdentity(identity accessibilityIdentity, foreground uintptr) error {
	if !identity.PasswordKnown {
		return errors.New("UI Automation password status is unavailable")
	}
	if identity.Password {
		return errPasswordTarget
	}
	if foreground == 0 || identity.Foreground != foreground || identity.ProcessID == 0 || identity.RuntimeID == "" || identity.RootRuntimeID == "" || !identity.Enabled || !identity.Focused {
		return errors.New("UI Automation focus identity is unavailable or changed")
	}
	return nil
}

func readAccessibilityTarget(reader accessibilityReader, foreground uintptr) (string, error) {
	var originalIdentity accessibilityIdentity
	var originalSnapshot accessibilitySnapshot
	for iteration := 0; iteration < 2; iteration++ {
		identity, err := reader.inspect()
		if err != nil {
			return "", err
		}
		if err := validateAccessibilityIdentity(identity, foreground); err != nil {
			return "", err
		}
		if iteration != 0 && identity != originalIdentity {
			return "", errors.New("UI Automation focused element changed during capture")
		}
		snapshot, err := reader.read(identity)
		if err != nil {
			return "", err
		}
		confirmed, err := reader.inspect()
		if err != nil {
			return "", err
		}
		if confirmed != identity {
			return "", errors.New("UI Automation focus changed during text access")
		}
		if snapshot.SelectionStart < 0 || snapshot.SelectionEnd < snapshot.SelectionStart || snapshot.SelectionEnd > snapshot.DocumentLength || snapshot.DocumentLength > maximumTargetCharacters {
			return "", errors.New("UI Automation selection endpoints are invalid")
		}
		if iteration == 0 {
			originalIdentity, originalSnapshot = identity, snapshot
		} else if originalSnapshot != snapshot {
			return "", errors.New("UI Automation text, selection or editability changed during capture")
		}
	}
	if !originalSnapshot.Editable {
		return "", errors.New("UI Automation target is not positively editable")
	}
	// Length-delimited runtime IDs (hex-encoded by the native reader) and fixed
	// digests make this token deterministic without retaining surrounding text.
	target := fmt.Sprintf("uia:%x:%x:%s:%s:%d:%x:%d:%x:%d:%x:%x",
		originalIdentity.Foreground, originalIdentity.ProcessID, originalIdentity.RootRuntimeID, originalIdentity.RuntimeID,
		originalSnapshot.DocumentLength, originalSnapshot.DocumentDigest,
		originalSnapshot.SelectionStart, originalSnapshot.StartDigest,
		originalSnapshot.SelectionEnd, originalSnapshot.EndDigest, originalSnapshot.SelectedDigest)
	return target, nil
}

func inspectWindowsAccessibilityTarget(foreground uintptr) (string, error) {
	return withWindowsAccessibility(foreground)
}

func validateAccessibilityText(characters []uint16) error {
	if len(characters) > maximumTargetCharacters {
		return errors.New("UI Automation text exceeds the safe inspection limit")
	}
	for index := 0; index < len(characters); index++ {
		character := characters[index]
		switch {
		case character == 0:
			return errors.New("UI Automation text contains NUL")
		case character >= 0xd800 && character <= 0xdbff:
			if index+1 >= len(characters) || characters[index+1] < 0xdc00 || characters[index+1] > 0xdfff {
				return errors.New("UI Automation text contains incomplete Unicode")
			}
			index++
		case character >= 0xdc00 && character <= 0xdfff:
			return errors.New("UI Automation text contains incomplete Unicode")
		}
	}
	return nil
}

func accessibilityTextDigest(characters []uint16) [32]byte {
	encoded := make([]byte, len(characters)*2)
	defer clear(encoded)
	for index, character := range characters {
		binary.LittleEndian.PutUint16(encoded[index*2:], character)
	}
	return sha256.Sum256(encoded)
}

// Prefixes come from cloned document ranges with their END moved to the
// selection's START/END. Never locate a selection by searching for its text.
func assembleAccessibilitySnapshot(document, selected, startPrefix, endPrefix []uint16, editable bool) (accessibilitySnapshot, error) {
	for _, characters := range [][]uint16{document, selected, startPrefix, endPrefix} {
		if err := validateAccessibilityText(characters); err != nil {
			return accessibilitySnapshot{}, err
		}
	}
	selectionStart, selectionEnd := len(startPrefix), len(endPrefix)
	if selectionStart > selectionEnd || selectionEnd > len(document) ||
		!slices.Equal(startPrefix, document[:selectionStart]) ||
		!slices.Equal(endPrefix, document[:selectionEnd]) ||
		!slices.Equal(selected, document[selectionStart:selectionEnd]) {
		return accessibilitySnapshot{}, errors.New("UI Automation document and range endpoints disagree")
	}
	return accessibilitySnapshot{
		DocumentDigest: accessibilityTextDigest(document), DocumentLength: len(document),
		StartDigest: accessibilityTextDigest(startPrefix), EndDigest: accessibilityTextDigest(endPrefix),
		SelectionStart: selectionStart, SelectionEnd: selectionEnd,
		SelectedDigest: accessibilityTextDigest(selected), Editable: editable,
	}, nil
}
