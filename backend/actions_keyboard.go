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
			"%q is a control character, not one that can be typed: set this node's Key Type to Special and pick "+
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

// =============================================== what a Hold leaves behind ===============================================

// keyUp is robotgo.KeyUp, reached through a variable rather than called
// directly, so the suite can prove a stopped run releases what it held without
// pressing anything on the machine the tests are running on.
//
// **Every release goes through it, including sendKeystroke's.** That branch used
// to call robotgo.KeyUp directly, which meant the one test that drives
// sendKeystroke - TestSendKeystrokeRecordsAHoldAndForgetsItOnRelease, which
// releases twice - pressed two real keys on whoever's machine ran the suite,
// stubbed seam or not. The stray keystroke is the small half of that. The larger
// half is that main_test.go gives "every task in the suite is a StartNode, the
// one node type that does not reach robotgo" as the reason its goleak ignore
// list can stay empty, and a robotgo call from a test makes that reasoning
// false: if robotgo's cgo path ever parks a goroutine the runtime can see, it
// surfaces as an unrelated-looking leak and invites exactly the blanket ignore
// entry that file forbids.
//
// KeyDown has no seam, and needs none for now: nothing on a run's exit path
// presses a key, and no test calls a branch that does.
var keyUp = robotgo.KeyUp

// heldKey is one keystroke a Hold action pushed down and no Release has let
// back up.
//
// combo is the identity - describeCombo of the same key and modifiers, so
// "ctrl+shift+a" is one held thing and not three - and key and modifiers are
// what has to be handed back to robotgo to let it up again.
type heldKey struct {
	combo     string
	key       string
	modifiers []string
}

// holdKey records a keystroke that has been left down. Holding the same
// combination twice records it once: a key is down or it is not, and there is
// no way to press it twice.
func (s *runState) holdKey(key string, modifiers []string) {
	if s == nil {
		return
	}

	combo := describeCombo(key, modifiers)
	for _, held := range s.held {
		if held.combo == combo {
			return
		}
	}
	s.held = append(s.held, heldKey{combo: combo, key: key, modifiers: modifiers})
}

// releaseKey forgets a keystroke a Release node has let back up.
//
// The match is on the key **and** its modifiers together, which is how the pair
// of nodes is drawn: a Release names the same combination its Hold did. A
// Release naming a different one - ctrl+a held, plain a released - leaves the
// record in place, so a stop still releases it. That direction is the safe one.
// robotgo.KeyUp on a key that is already up is a no-op, where a modifier
// forgotten because something that looked like its release went past is a Ctrl
// left down over every window the user then touches.
func (s *runState) releaseKey(key string, modifiers []string) {
	if s == nil {
		return
	}

	combo := describeCombo(key, modifiers)
	for i, held := range s.held {
		if held.combo != combo {
			continue
		}
		s.held = append(s.held[:i], s.held[i+1:]...)
		return
	}
}

// releaseHeldKeys lets go of everything the run left down, and is what a
// cancelled run calls on its way out (finishRun).
//
// This is the guarantee sendKeystroke could not make on its own. A Hold node
// deliberately leaves a key down for a later Release node, and "the Release
// never runs" was simply the documented hazard - a handler has no idea which
// other nodes exist. The run does, and it matters far more now that RunMacro is
// a toggle: the keystroke that stops a macro arrives while the macro may be
// holding modifiers down, and a stuck Ctrl or Shift is not a problem confined
// to the macro that caused it. It is the same guarantee the deferred drag
// release in actions_mouse.go gives for the mouse button.
//
// Scoped to cancellation on purpose. A macro that ends normally having
// deliberately left a key held is what this app has always done, and changing
// that is a separate decision.
//
// Released in reverse order - last held, first let up - so a modifier held
// before the key it modifies is the last thing to go. Failures are logged and
// the rest still released: a key that will not come up is exactly when the
// others matter most.
func (s *runState) releaseHeldKeys() {
	if s == nil || len(s.held) == 0 {
		return
	}

	for i := len(s.held) - 1; i >= 0; i-- {
		held := s.held[i]
		log.Printf("Releasing %s, which the run left held", held.combo)
		if err := keyUp(held.key, modifierArgs(held.modifiers)...); err != nil {
			log.Printf("Could not release %s: %v", held.combo, err)
		}
	}
	s.held = nil
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
// nothing passed between them. What the engine cannot promise is that the
// Release ever runs: hit the idle timeout, or wire a flow that simply forgets
// the Release, and the key stays physically down after the macro ends. Press
// carries no such hazard - it releases even when interrupted.
//
// Stop is no longer on that list, and that is what state is for here. A Hold
// records the combination on the run and a Release un-records it, so a
// cancelled run can let go of whatever is left (releaseHeldKeys above). The
// comment this replaces said nothing here could undo a Hold "because a handler
// has no idea which other nodes exist", which was true when a handler was all
// there was; the run knows, and it is handed to every handler.
//
// Reports whether the run was canceled part-way, and any error worth surfacing.
func sendKeystroke(ctx context.Context, state *runState, action, key string, modifiers []string, duration time.Duration) (canceled bool, err error) {
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
		// Recorded only once the key is really down, so a KeyDown that failed
		// does not leave the run trying to release something it never pressed.
		state.holdKey(key, modifiers)

	case keyActionRelease:
		log.Printf("Releasing %s", describeCombo(key, modifiers))
		// Through the keyUp seam, like releaseHeldKeys - the two are the same
		// act, and a Release node that went straight to robotgo would be the one
		// keystroke a test could not stub. See the comment on keyUp.
		if err := keyUp(key, mods...); err != nil {
			// Un-recorded only on success, for the mirror of the reason above: a
			// release that failed leaves the key down, and the record is what
			// gets a stop to try again.
			return false, fmt.Errorf("could not release key %q: %w", key, err)
		}
		state.releaseKey(key, modifiers)
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
//
// ctx is the context of the run this task belongs to, so Stop's cancel cuts a
// hold short and Stop's wg.Wait is never held up by it. It is a parameter rather
// than something read out of app.runner - see executeDelayTask for why the
// difference matters.
func executeKeyPressTask(ctx context.Context, task Task, state *runState, app *App) (string, error) {
	log.Printf("KeyPressNode task starting - Data: %+v", task.Data)

	// Reported to the frontend as well as returned, and by the same value, so the
	// status panel and the caller cannot be told different things.
	fail := func(err error) error {
		log.Printf("KeyPressNode error: %s for task %s", err, task.ID)
		app.emitEvent("task-error", map[string]interface{}{
			"taskID": task.ID,
			"error":  err.Error(),
		})
		return err
	}

	cfg, err := parseKeyPressConfig(task.Data)
	if err != nil {
		return "", fail(err)
	}

	succeed := func() {
		app.emitEvent("task-success", map[string]interface{}{
			"taskID": task.ID,
			"type":   "KeyPressNode",
		})
	}

	key := cfg.namedKey
	modifiers := cfg.modifiers

	if cfg.kind == keyKindCharacter {
		resolvedKey, layoutModifiers, ok, reason := resolveCharacterKey(cfg.character)
		if !ok {
			if cfg.action != keyActionPress || len(cfg.modifiers) > 0 {
				return "", fail(fmt.Errorf(
					"cannot send %q as a keystroke: %s. A plain Press with no modifiers can still type it",
					string(cfg.character), reason))
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
			return "", nil
		}

		key = resolvedKey
		modifiers = mergeModifiers(layoutModifiers, cfg.modifiers)
		log.Printf("Character %q resolves to %s on the keyboard layout in use",
			string(cfg.character), describeCombo(resolvedKey, layoutModifiers))
	}

	canceled, err := sendKeystroke(ctx, state, cfg.action, key, modifiers, cfg.duration)
	if err != nil {
		return "", fail(err)
	}
	if canceled {
		// The user pressed Stop (or the run was torn down). Return without an
		// error event: the run reports its own ending once it stops walking,
		// and the walk must be free to return. And without an error - cancellation is the run
		// ending rather than this node failing, and the caller holds the context
		// that says so.
		log.Printf("KeyPressNode hold canceled for task %s", task.ID)
		return "", nil
	}

	succeed()
	return "", nil
}
