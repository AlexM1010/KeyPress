// actions_keyboard.go
//
// Handler for the keyboard node type. There is exactly one: KeyPressNode, which
// sends a single keystroke with any combination of modifiers.
//
// The node stores what the user *meant*, not the keystroke that expresses it.
// A character node stores the character - "!", "?", "a", "£" - and the keystroke
// is worked out at run time against the keyboard layout in force at that moment
// (keylayout_windows.go). That is what makes a saved flow portable: "!" is
// Shift+1 on a UK and a US keyboard but plain 1 on a French one, and "@" is
// Shift+2 on a US keyboard and Shift+apostrophe on a UK one, so any keystroke
// written into the save file would be wrong on somebody's machine. Nothing is
// detected, nothing is stored that can go stale, and switching layout mid-session
// needs no re-save.
//
// Keys with no character of their own - Enter, F5, the arrows - are already the
// same everywhere, so those are stored by name and kept clearly separate.

package backend

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-vgo/robotgo"
)

// defaultPressDurationMs is the fallback when a payload carries no usable
// pressDuration. It matches the frontend's DEFAULT_DATA.
const defaultPressDurationMs = 50

// maxPressDurationMs mirrors the ceiling of the node's Press Duration slider.
// A hand-edited save file can carry anything, and this is a key being physically
// held down rather than a wait, so an unbounded value would pin it down for as
// long as the file says with no way to tell that apart from a wedged task.
const maxPressDurationMs = 1000

// Key kinds understood by the handler, matching the node's Key Type select.
const (
	// keyKindCharacter stores a character and resolves it to a keystroke at run
	// time, so the node types that character on any keyboard layout.
	keyKindCharacter = "character"
	// keyKindNamed stores the name of a key that prints no character, which is
	// the same key on every layout and so needs no resolution.
	keyKindNamed = "named"
)

// Key actions understood by the handler, matching the node's Key Action select.
const (
	keyActionPress   = "press"
	keyActionHold    = "hold"
	keyActionRelease = "release"
)

// modifierKeyNames maps the node's modifier choices onto the names robotgo's
// key-flag table understands.
//
// There is no "None" entry: the payload carries a *set* of modifiers, and the
// empty set already says "no modifier". The sentinel the single-slot version
// needed is gone with it.
//
// robotgo's "cmd" is the Meta modifier: the Windows key on Windows and Linux,
// the Command key on macOS. The node labels it "Windows" because that is what
// the key says on the platform this app ships for; the other spellings are
// accepted here so a macro written on either platform still runs on the other.
var modifierKeyNames = map[string]string{
	"shift":   "shift",
	"ctrl":    "ctrl",
	"control": "ctrl",
	"alt":     "alt",
	"windows": "cmd",
	"command": "cmd",
	"cmd":     "cmd",
	"meta":    "cmd",
	"super":   "cmd",
}

// modifierOrder is the order modifiers are emitted in. robotgo ORs them into a
// single flag word (getFlagsFromValue), so the order changes nothing about what
// is sent - it exists so logs and messages name a combination the same way every
// time, and in the order people write it.
var modifierOrder = []string{"ctrl", "alt", "shift", "cmd"}

// keyPressConfig is the validated form of a KeyPressNode's data payload.
type keyPressConfig struct {
	// kind is one of the keyKind* constants.
	kind string

	// character is the character to type, in keyKindCharacter. Guaranteed to be
	// a single non-control rune inside the Basic Multilingual Plane.
	character rune

	// namedKey is a robotgo key name ("enter", "f5", "up", ...) in
	// keyKindNamed. Always lower-case, and never a single character - those are
	// keyKindCharacter's job, and letting one through here would hand robotgo a
	// name that triggers its implicit-shift and US-layout rewrites.
	namedKey string

	// action is one of the keyAction* constants, already lower-cased.
	action string

	// modifiers are the modifiers the *user* asked for, as robotgo names,
	// deduplicated and in modifierOrder. The modifiers a character needs on the
	// current layout are worked out separately at run time and merged in.
	modifiers []string

	// duration is how long Press holds the key down. Unused by Hold/Release.
	duration time.Duration
}

// canonicalModifiers renders a modifier set as a deduplicated slice in
// modifierOrder.
func canonicalModifiers(present map[string]bool) []string {
	modifiers := make([]string, 0, len(present))
	for _, modifier := range modifierOrder {
		if present[modifier] {
			modifiers = append(modifiers, modifier)
		}
	}
	return modifiers
}

// mergeModifiers unions two modifier sets.
//
// Union is the only sensible merge. A modifier is a flag in the event robotgo
// builds, not a count, so a character that already needs Shift plus a user who
// also ticked Shift is one Shift, not two and not none: there is no way to press
// it twice and cancelling it would stop the character being typed at all.
func mergeModifiers(sets ...[]string) []string {
	present := map[string]bool{}
	for _, set := range sets {
		for _, modifier := range set {
			present[modifier] = true
		}
	}
	return canonicalModifiers(present)
}

// parseModifiers reads the node's modifier set. A missing or empty list means no
// modifiers, which is a perfectly ordinary keystroke rather than an error.
func parseModifiers(raw interface{}) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	list, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid modifiers value: %v", raw)
	}

	present := map[string]bool{}
	for _, item := range list {
		name, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("invalid modifier value: %v", item)
		}
		modifier, ok := modifierKeyNames[strings.ToLower(strings.TrimSpace(name))]
		if !ok {
			return nil, fmt.Errorf("unsupported modifier: %s", name)
		}
		present[modifier] = true
	}

	return canonicalModifiers(present), nil
}

// parseKeyKind reads the node's Key Type select.
func parseKeyKind(raw interface{}) (string, error) {
	name, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("invalid keyKind value: %v", raw)
	}
	switch kind := strings.ToLower(strings.TrimSpace(name)); kind {
	case keyKindCharacter, keyKindNamed:
		return kind, nil
	default:
		return "", fmt.Errorf("unsupported keyKind: %s", name)
	}
}

// parseCharacter validates the character a character node types.
//
// Exactly one rune, because this node is one keystroke; a run of text belongs in
// a node of its own. Leading and trailing space is NOT trimmed - a space is a
// perfectly good character to type, and trimming would turn it into "empty".
//
// The rune must fit in 16 bits, because the fallback path that types a character
// directly puts it in a 16-bit field (INPUT.ki.wScan on Windows, UniChar on
// macOS), so anything above U+FFFF would arrive silently truncated into some
// other character. Control characters are refused because they have no key of
// their own to resolve to and no glyph to type; Enter and Tab are named keys.
func parseCharacter(raw interface{}) (rune, error) {
	text, ok := raw.(string)
	if !ok {
		return 0, fmt.Errorf("invalid character value: %v", raw)
	}
	if text == "" {
		return 0, fmt.Errorf("no character chosen: enter the character this node should type")
	}

	character, size := utf8.DecodeRuneInString(text)
	if character == utf8.RuneError && size <= 1 {
		return 0, fmt.Errorf("character %q is not valid text", text)
	}
	if size != len(text) {
		return 0, fmt.Errorf(
			"%q is more than one character: a Keypress node sends one keystroke, so use one node per character",
			text)
	}
	if character > 0xFFFF {
		return 0, fmt.Errorf(
			"%q cannot be sent as a keystroke: only characters up to U+FFFF can be", text)
	}
	if unicode.IsControl(character) {
		return 0, fmt.Errorf(
			"%q is a control character, not one that can be typed: set this node's Key Type to Named and pick "+
				"the key itself (enter, tab, escape) instead", text)
	}

	return character, nil
}

// parseNamedKey validates the name of a key that prints no character.
//
// A single-character name is refused, and that refusal is load-bearing rather
// than fussy. robotgo adds a shift modifier of its OWN to two classes of
// single-character name, and because this node drives KeyDown and KeyUp as two
// separate calls, a robotgo-added shift is not merely redundant - it can be
// added on the way down and dropped on the way up, leaving Shift physically
// held. (Line numbers below are github.com/go-vgo/robotgo v0.110.5, key.go;
// v1.0.2 moves the same code into an appendShift helper without changing either
// rule.)
//
//   - An upper-case first rune. KeyToggle (key.go:546) does
//     `if unicode.IsUpper(...) { args = append(args, "shift") }`, so "A" is
//     *defined* as "a" plus shift.
//
//   - A shifted-symbol name such as "!", ":" or "?". KeyToggle (key.go:551)
//     rewrites it through robotgo.Special to the unshifted key sharing that
//     keycap and appends shift - but only `if len(args) <= 1`, and KeyUp always
//     spends one of those slots on its "up" marker. So KeyDown("!", "ctrl")
//     presses Shift and KeyUp("!", "ctrl") does not release it. robotgo.Special
//     is a hard-coded US table besides: it claims "@" is Shift+2, which is true
//     on a US keyboard and false on a UK one.
//
// Keeping every single-character name out of this field means neither branch can
// ever be reached. Characters go through keyKindCharacter, which resolves them
// against the real layout and hands robotgo a name it takes at face value.
func parseNamedKey(raw interface{}) (string, error) {
	text, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("invalid namedKey value: %v", raw)
	}
	name := strings.ToLower(strings.TrimSpace(text))
	if name == "" {
		return "", fmt.Errorf("no key chosen")
	}
	if utf8.RuneCountInString(name) == 1 {
		return "", fmt.Errorf(
			"%q is a character, not the name of a key: set this node's Key Type to Character and enter %s there, "+
				"which types it whatever keyboard layout is in use", text, text)
	}
	return name, nil
}

// parsePressDuration reads the Press Duration slider's payload field. Numbers
// arrive as float64 because the payload came through encoding/json.
func parsePressDuration(raw interface{}) (time.Duration, error) {
	durationMs := float64(defaultPressDurationMs)
	if value, ok := raw.(float64); ok {
		durationMs = value
	}
	if durationMs < 0 {
		return 0, fmt.Errorf("pressDuration cannot be negative: %v", durationMs)
	}
	if durationMs > maxPressDurationMs {
		durationMs = maxPressDurationMs
	}
	return time.Duration(durationMs) * time.Millisecond, nil
}

// parseKeyPressConfig validates a task payload.
func parseKeyPressConfig(data map[string]interface{}) (keyPressConfig, error) {
	kind, err := parseKeyKind(data["keyKind"])
	if err != nil {
		return keyPressConfig{}, err
	}

	rawAction, ok := data["keyAction"].(string)
	if !ok {
		return keyPressConfig{}, fmt.Errorf("invalid keyAction value: %v", data["keyAction"])
	}
	action := strings.ToLower(strings.TrimSpace(rawAction))
	switch action {
	case keyActionPress, keyActionHold, keyActionRelease:
	default:
		return keyPressConfig{}, fmt.Errorf("unsupported keyAction: %s", rawAction)
	}

	modifiers, err := parseModifiers(data["modifiers"])
	if err != nil {
		return keyPressConfig{}, err
	}

	duration, err := parsePressDuration(data["pressDuration"])
	if err != nil {
		return keyPressConfig{}, err
	}

	cfg := keyPressConfig{
		kind:      kind,
		action:    action,
		modifiers: modifiers,
		duration:  duration,
	}

	switch kind {
	case keyKindCharacter:
		if cfg.character, err = parseCharacter(data["character"]); err != nil {
			return keyPressConfig{}, err
		}
	case keyKindNamed:
		if cfg.namedKey, err = parseNamedKey(data["namedKey"]); err != nil {
			return keyPressConfig{}, err
		}
	}

	return cfg, nil
}

// describeCombo renders a keystroke for a log line, e.g. `ctrl+shift+a`.
func describeCombo(key string, modifiers []string) string {
	return strings.Join(append(append([]string{}, modifiers...), key), "+")
}

// modifierArgs renders a modifier list as robotgo's variadic argument list.
// robotgo turns each entry into a key flag and ORs them together, so passing
// several is how a combination is expressed.
func modifierArgs(modifiers []string) []interface{} {
	if len(modifiers) == 0 {
		return nil
	}
	args := make([]interface{}, 0, len(modifiers))
	for _, modifier := range modifiers {
		args = append(args, modifier)
	}
	return args
}

// sendKeystroke drives key plus modifiers for one of the key actions:
//
//   - Press:   hold the key down for duration, then release it. Entirely
//     self-contained, so this is the action to reach for by default.
//   - Hold:    push the key down and leave it down.
//   - Release: let the key back up.
//
// Hold and Release are two halves of one gesture split across two nodes. That
// works because the "is this key down" state lives in the OS keyboard driver,
// not in this engine, so a Hold on one node and a Release on a later one need
// nothing passed between them. What the engine genuinely cannot promise is that
// the Release ever runs: press Stop, hit the idle timeout, or wire a flow that
// simply forgets the Release, and the key stays physically down after the macro
// ends. Nothing here can undo that, because a handler has no idea which other
// nodes exist. Press carries no such hazard - it releases even when interrupted.
//
// Reports whether the run was canceled part-way, and any error worth surfacing.
func sendKeystroke(ctx context.Context, action, key string, modifiers []string, duration time.Duration) (canceled bool, err error) {
	mods := modifierArgs(modifiers)

	switch action {
	case keyActionPress:
		log.Printf("Holding %s for %v", describeCombo(key, modifiers), duration)
		if err := robotgo.KeyDown(key, mods...); err != nil {
			return false, fmt.Errorf("could not press key %q: %w", key, err)
		}

		completed := waitInterruptible(ctx, duration)

		// Release unconditionally, including when the wait was cut short. A key
		// left down after Stop would keep auto-repeating into whatever window
		// has focus, which is far worse than a macro that ended early. The same
		// modifier list goes back up, so no modifier is left held either.
		if err := robotgo.KeyUp(key, mods...); err != nil {
			return !completed, fmt.Errorf("could not release key %q: %w", key, err)
		}

		return !completed, nil

	case keyActionHold:
		log.Printf("Pressing %s down and leaving it down", describeCombo(key, modifiers))
		if err := robotgo.KeyDown(key, mods...); err != nil {
			return false, fmt.Errorf("could not press key %q: %w", key, err)
		}

	case keyActionRelease:
		log.Printf("Releasing %s", describeCombo(key, modifiers))
		if err := robotgo.KeyUp(key, mods...); err != nil {
			return false, fmt.Errorf("could not release key %q: %w", key, err)
		}
	}

	return false, nil
}

// executeKeyPressTask sends the node's keystroke.
//
// A named key goes straight through. A character is first resolved against the
// keyboard layout in force, which yields the key to press and whatever modifiers
// that layout needs to reach the character; those are merged with the modifiers
// the user asked for and the keystroke is sent the same way.
//
// When the layout cannot express the character as a keystroke robotgo is able to
// drive, a plain Press falls back to typing the character directly - the OS call
// that carries a character rather than a key, which no layout can get wrong. The
// fallback is logged, not silent, and it is only reachable for a plain Press:
// that call is a single indivisible press-and-release, so it can neither hold a
// key nor combine with a modifier, and asking for either reports the reason
// instead.
//
// This deliberately does not take mouseMu. robotgo's key path touches no
// package-level global that a concurrent task writes (the key-code lookup and
// the modifier flags are computed into locals), so keyboard tasks are free to
// run alongside mouse tasks.
func executeKeyPressTask(task Task, app *App) {
	log.Printf("KeyPressNode task starting - Data: %+v", task.Data)

	// Handlers cannot return an error, so every failure is reported the same way
	// the other actions_*.go handlers report theirs.
	fail := func(err error) {
		log.Printf("KeyPressNode error: %s for task %s", err, task.ID)
		app.emitEvent("task-error", map[string]interface{}{
			"taskID": task.ID,
			"error":  err.Error(),
		})
	}

	cfg, err := parseKeyPressConfig(task.Data)
	if err != nil {
		fail(err)
		return
	}

	succeed := func() {
		app.emitEvent("task-success", map[string]interface{}{
			"taskID": task.ID,
			"type":   "KeyPressNode",
		})
	}

	// Capture this run's context before anything that waits: it is the
	// generation the worker running us belongs to, so Stop's cancel cuts the
	// hold short and Stop's wg.Wait is never held up by it.
	ctx := app.taskQueue.Context()

	key := cfg.namedKey
	modifiers := cfg.modifiers

	if cfg.kind == keyKindCharacter {
		resolvedKey, layoutModifiers, ok, reason := resolveCharacterKey(cfg.character)
		if !ok {
			if cfg.action != keyActionPress || len(cfg.modifiers) > 0 {
				fail(fmt.Errorf(
					"cannot send %q as a keystroke: %s. A plain Press with no modifiers can still type it",
					string(cfg.character), reason))
				return
			}

			// robotgo.UnicodeType sends the character itself rather than a key:
			// KEYEVENTF_UNICODE on Windows, CGEventKeyboardSetUnicodeString on
			// macOS. Neither consults the keyboard layout, so the character is
			// always the right one - but it is also not a real key, so an
			// application that reads key codes rather than text (a game, mostly)
			// will not see it.
			log.Printf("Typing %q directly rather than as a key: %s", string(cfg.character), reason)
			robotgo.UnicodeType(uint32(cfg.character))
			succeed()
			return
		}

		key = resolvedKey
		modifiers = mergeModifiers(layoutModifiers, cfg.modifiers)
		log.Printf("Character %q resolves to %s on the keyboard layout in use",
			string(cfg.character), describeCombo(resolvedKey, layoutModifiers))
	}

	canceled, err := sendKeystroke(ctx, cfg.action, key, modifiers, cfg.duration)
	if err != nil {
		fail(err)
		return
	}
	if canceled {
		// The user pressed Stop (or the run was torn down). Return without an
		// error event: StopExecution already reports the stop, and the worker
		// must be free to exit.
		log.Printf("KeyPressNode hold canceled for task %s", task.ID)
		return
	}

	succeed()
}
