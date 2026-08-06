//go:build windows

// keylayout_windows.go
//
// Resolution of a character to the keystroke that types it, against the
// keyboard layout in force *right now*.
//
// A KeyPressNode stores what the user meant - the character "!" - rather than a
// key plus a shift state, because the key that carries "!" is not the same key
// everywhere. Resolving at run time rather than at save time is what makes a
// flow portable: nothing has to be detected, nothing is stored that can go
// stale, and the same file works after the user switches layout mid-session or
// sends the flow to someone on another keyboard.
//
// Windows exposes exactly the primitive needed. VkKeyScanEx answers "which
// virtual key and which modifiers type this character on that layout", which is
// the forward direction; MapVirtualKeyEx with MAPVK_VK_TO_CHAR answers "which
// character is printed on the unshifted face of this virtual key", which is the
// direction needed to hand the key back to robotgo, whose API names keys by
// their unshifted character.

package backend

import (
	"fmt"
	"syscall"
	"unicode"

	"github.com/go-vgo/robotgo"
)

var (
	user32 = syscall.NewLazyDLL("user32.dll")

	procVkKeyScanExW             = user32.NewProc("VkKeyScanExW")
	procMapVirtualKeyExW         = user32.NewProc("MapVirtualKeyExW")
	procGetKeyboardLayout        = user32.NewProc("GetKeyboardLayout")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
)

// mapvkVKToChar is MAPVK_VK_TO_CHAR: translate a virtual key into the
// *unshifted* character printed on it. A dead key is flagged in the top bit.
const mapvkVKToChar = 2

// Modifier bits VkKeyScanEx packs into the high byte of its result.
const (
	vkModShift = 0x01
	vkModCtrl  = 0x02
	vkModAlt   = 0x04
)

// callerKeyboardLayout is the layout of *this* thread - the one robotgo will
// use when it turns a key name back into a virtual key, because its
// keyCodeForChar calls the plain VkKeyScan, which is implicitly per-thread.
func callerKeyboardLayout() uintptr {
	layout, _, _ := procGetKeyboardLayout.Call(0)
	return layout
}

// targetKeyboardLayout is the layout of the window the keystroke is about to
// land in - the one that will decide what the virtual key we send actually
// types. Windows keeps a layout per thread, so a user who switches layout
// switches it for the focused window, not for this app.
//
// Falls back to this thread's layout if there is no foreground window to ask.
func targetKeyboardLayout() uintptr {
	window, _, _ := procGetForegroundWindow.Call()
	if window != 0 {
		thread, _, _ := procGetWindowThreadProcessID.Call(window, 0)
		if thread != 0 {
			if layout, _, _ := procGetKeyboardLayout.Call(thread); layout != 0 {
				return layout
			}
		}
	}
	return callerKeyboardLayout()
}

// vkKeyScanEx returns the virtual key and modifier state that type character on
// layout, or -1 when no single keystroke does.
func vkKeyScanEx(character rune, layout uintptr) int {
	result, _, _ := procVkKeyScanExW.Call(uintptr(uint16(character)), layout)
	return int(int16(uint16(result)))
}

// mapVirtualKeyEx translates virtualKey per mapType on layout.
func mapVirtualKeyEx(virtualKey uint32, mapType uint32, layout uintptr) uint32 {
	result, _, _ := procMapVirtualKeyExW.Call(uintptr(virtualKey), uintptr(mapType), layout)
	return uint32(result)
}

// resolveCharacterKey reports the key name and modifiers that type character on
// the active keyboard layout, or ok=false plus a reason fit to show the user.
//
// The two layouts are deliberately different. The character is resolved to a
// virtual key against the *target* layout, because that is what will interpret
// the virtual key we send. The virtual key is then turned back into a key name
// against the *caller* layout, because robotgo re-derives the virtual key from
// that name using this thread's layout - so this is the name that makes robotgo
// emit the virtual key we actually resolved. On the overwhelmingly common
// single-layout machine the two are the same value and this is a no-op.
//
// Everything here is a lookup. Nothing below sends input.
func resolveCharacterKey(character rune) (key string, modifiers []string, ok bool, reason string) {
	target := targetKeyboardLayout()
	caller := callerKeyboardLayout()

	scan := vkKeyScanEx(character, target)
	if scan == -1 {
		return "", nil, false, fmt.Sprintf(
			"the keyboard layout in use has no key that types %q", string(character))
	}

	virtualKey := uint32(scan & 0xFF)
	state := (scan >> 8) & 0xFF
	if state&^(vkModShift|vkModCtrl|vkModAlt) != 0 {
		// Bit 3 upwards are the Hankaku / Reserved / Kana states of the Far
		// Eastern layouts, which are not modifier keys that can simply be held.
		return "", nil, false, fmt.Sprintf(
			"typing %q on the keyboard layout in use needs a keyboard state this app cannot hold down",
			string(character))
	}

	// Ctrl+Alt is how Windows reports AltGr: the AltGr key generates a
	// left-Ctrl plus a right-Alt, and layouts describe their AltGr characters
	// that way. Sending left-Ctrl plus left-Alt is what robotgo can express and
	// is what the overwhelming majority of applications accept as AltGr, but it
	// is not bit-for-bit the same event, so an application that insists on the
	// right-hand Alt will not see it.
	present := map[string]bool{}
	if state&vkModCtrl != 0 {
		present["ctrl"] = true
	}
	if state&vkModAlt != 0 {
		present["alt"] = true
	}
	if state&vkModShift != 0 {
		present["shift"] = true
	}

	// MAPVK_VK_TO_CHAR flags a dead key in the top bit. The key itself is still
	// the right key to press, so the bit is masked off rather than refused -
	// whether a glyph appears immediately or waits for the next keystroke is the
	// receiving application's business, not ours.
	baseChar := rune(mapVirtualKeyEx(virtualKey, mapvkVKToChar, caller) & 0x7FFFFFFF)
	if baseChar == 0 {
		return "", nil, false, fmt.Sprintf(
			"typing %q needs a key with no character of its own, which cannot be named", string(character))
	}
	// MAPVK_VK_TO_CHAR reports letters in upper case. robotgo reads an
	// upper-case name as "that key plus shift", which would be a second,
	// implicit shift on top of the one already resolved above, so fold the case
	// out here and let the resolved modifier set carry it.
	baseChar = unicode.ToLower(baseChar)

	if baseChar < 0x20 || baseChar > 0x7E {
		// robotgo names a key by a single *byte* (its checkKeyCodes passes the
		// first byte of the name to the ANSI VkKeyScan), so a key whose face is
		// not printable ASCII cannot be named to it at all.
		return "", nil, false, fmt.Sprintf(
			"typing %q needs a key this app has no way to name", string(character))
	}
	if _, rewritten := robotgo.Special[string(baseChar)]; rewritten {
		// robotgo rewrites these names through a hard-coded US table before it
		// looks anything up, so passing this name would press a different key
		// entirely. It only happens on a layout that puts one of the US shifted
		// symbols on an unshifted key - "#" on a UK keyboard, for instance.
		return "", nil, false, fmt.Sprintf(
			"typing %q means pressing the %q key, which cannot be driven as a key on this keyboard layout",
			string(character), string(baseChar))
	}

	// Confirm the round trip: robotgo will turn this name back into a virtual
	// key with the caller layout, and that has to be the key resolved above,
	// unshifted. If it is not, the two layouts disagree in a way that would
	// press the wrong key, so say so rather than guess.
	if back := vkKeyScanEx(baseChar, caller); back == -1 || uint32(back&0xFF) != virtualKey || (back>>8)&0xFF != 0 {
		return "", nil, false, fmt.Sprintf(
			"this app and the window it is typing into disagree about where %q lives on the keyboard",
			string(character))
	}

	return string(baseChar), canonicalModifiers(present), true, ""
}
