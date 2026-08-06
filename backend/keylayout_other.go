//go:build !windows

// keylayout_other.go
//
// Stand-in for keylayout_windows.go on the platforms this app does not ship
// for. The Windows build asks the OS which key types a given character on the
// layout in force; macOS and X11 both have an equivalent (TISGetInputSource /
// XkbLookupKeySym), but neither is written here, so this file resolves only the
// characters that need no layout knowledge at all and honestly declines the
// rest. Declining is not a dead end: the caller falls back to typing the
// character directly, which is layout-independent by construction.

package backend

import (
	"fmt"
	"unicode"

	"github.com/go-vgo/robotgo"
)

// resolveCharacterKey reports the key name and modifiers that type character,
// or ok=false plus a reason fit to show the user.
//
// Only ASCII letters, digits and the unshifted punctuation robotgo can name are
// resolved, and the mapping assumed for them is the one every Latin layout
// agrees on: the key is named by the character itself, and an upper-case letter
// is that key plus shift. Anything else - a shifted symbol, an accented letter,
// a character robotgo would rewrite through its US-layout table - is declined,
// because getting it right needs the layout query this file does not make.
func resolveCharacterKey(character rune) (key string, modifiers []string, ok bool, reason string) {
	declined := fmt.Sprintf(
		"typing %q as a key needs a keyboard-layout query that is only implemented on Windows",
		string(character))

	if character < 0x20 || character > 0x7E {
		return "", nil, false, declined
	}

	// Within printable ASCII the only characters unicode.ToLower changes are
	// A-Z, and "that key plus shift" is what an upper-case letter means on every
	// Latin layout.
	unshifted := unicode.ToLower(character)
	if _, rewritten := robotgo.Special[string(unshifted)]; rewritten {
		return "", nil, false, declined
	}
	if unshifted != character {
		return string(unshifted), []string{"shift"}, true, ""
	}

	return string(unshifted), nil, true, ""
}
