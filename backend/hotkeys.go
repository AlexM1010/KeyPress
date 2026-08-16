// hotkeys.go
//
// System-wide shortcuts that run a saved macro.
//
// A hotkey is what makes "Keypress stays resident" worth anything: it fires
// whatever has focus, so a macro can be triggered in the middle of the thing it
// is meant to automate, with the Keypress window closed and nothing to click.
//
// The hotkey is not configured here and has no storage of its own. It is the
// Start node's recorded macro - `data.macroKeys` on the graph, set with the
// Record button in the workspace - which is where the user has always set it
// and where it travels with the macro it belongs to. This file is only the part
// that was missing: reading those keys off the saved graphs and actually
// claiming them from the OS.

package backend

import (
	"fmt"
	"log"
	"sort"
	"strings"
)

// macroKeysField is where the Start node keeps the recorded trigger, as an
// array of raw KeyboardEvent.key strings in the order they went down.
const macroKeysField = "macroKeys"

// modifierNames maps what the Start node records for a modifier to the name
// Wails' accelerator parser uses.
//
// "Option" is macOS's name for Alt and is offered by the node's own key picker,
// so it is accepted here even though a Windows recording never produces it.
var modifierNames = map[string]string{
	"Control": "Ctrl",
	"Ctrl":    "Ctrl",
	"Alt":     "Alt",
	"Option":  "Alt",
	"Shift":   "Shift",
	"Meta":    "Super",
	"Super":   "Super",
}

// namedKeys maps the KeyboardEvent.key values that differ from what Wails
// calls the same key. Anything not in here has to be a single character.
var namedKeys = map[string]string{
	" ":          "Space",
	"Spacebar":   "Space",
	"ArrowUp":    "Up",
	"ArrowDown":  "Down",
	"ArrowLeft":  "Left",
	"ArrowRight": "Right",
	"Esc":        "Escape",
	"Escape":     "Escape",
	"Enter":      "Enter",
	"Return":     "Enter",
	"Tab":        "Tab",
	"Backspace":  "Backspace",
	"Delete":     "Delete",
	"Del":        "Delete",
	"Home":       "Home",
	"End":        "End",
	"PageUp":     "Page Up",
	"PageDown":   "Page Down",
	"NumLock":    "NumLock",
}

// acceleratorFromMacroKeys turns a Start node's recorded keys into the
// accelerator string Wails registers, e.g. ["Shift","Control","J"] becomes
// "Ctrl+Shift+J".
//
// The recording is in the order the keys went down, which is whatever order the
// user's fingers arrived in, so the modifiers are collected and sorted rather
// than taken as given - two recordings of the same chord must not produce two
// different accelerators.
//
// A recording with no modifier is refused. The shortcut is registered
// system-wide, so a bare "J" would take that key away from every other
// application on the machine; that is a footgun, not a hotkey.
func acceleratorFromMacroKeys(keys []string) (string, error) {
	var modifiers []string
	var mainKey string

	for _, raw := range keys {
		key := strings.TrimSpace(raw)
		// Deliberately not trimming a recorded " " to nothing: space is a key.
		if raw == " " {
			key = raw
		}
		if key == "" {
			continue
		}

		if name, isModifier := modifierNames[key]; isModifier {
			if !contains(modifiers, name) {
				modifiers = append(modifiers, name)
			}
			continue
		}

		name, err := keyName(key)
		if err != nil {
			return "", err
		}
		if mainKey != "" && mainKey != name {
			return "", fmt.Errorf("a hotkey can only have one key besides its modifiers, but %q and %q were both recorded", mainKey, name)
		}
		mainKey = name
	}

	if mainKey == "" {
		return "", fmt.Errorf("no key was recorded besides modifiers")
	}
	if len(modifiers) == 0 {
		return "", fmt.Errorf("a global hotkey needs at least one of Ctrl, Alt, Shift or Win, or it would take %q from every other application", mainKey)
	}

	sort.Strings(modifiers)
	return strings.Join(append(modifiers, mainKey), "+"), nil
}

// keyName is the Wails name for a recorded key.
func keyName(key string) (string, error) {
	if name, found := namedKeys[key]; found {
		return name, nil
	}
	if isFunctionKey(key) {
		return strings.ToUpper(key), nil
	}
	// One character covers letters, digits and punctuation. Upper-cased for
	// display; Wails lower-cases it again before parsing.
	if len([]rune(key)) == 1 {
		return strings.ToUpper(key), nil
	}
	return "", fmt.Errorf("%q cannot be used as a hotkey", key)
}

// isFunctionKey reports whether key is F1 through F24.
func isFunctionKey(key string) bool {
	if len(key) < 2 || (key[0] != 'F' && key[0] != 'f') {
		return false
	}
	number := 0
	for _, r := range key[1:] {
		if r < '0' || r > '9' {
			return false
		}
		number = number*10 + int(r-'0')
	}
	return number >= 1 && number <= 24
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// macroAccelerator reads the hotkey a saved macro asks for, empty when it does
// not ask for one.
//
// A graph with no Start node, or a Start node with nothing recorded, simply has
// no hotkey - that is the ordinary case and not worth a word. A recording that
// cannot be turned into an accelerator is different: the user pressed something
// they expect to work, so it is logged with the reason.
func (a *App) macroAccelerator(flow *FlowData, id string) string {
	keys := startNodeMacroKeys(flow)
	if len(keys) == 0 {
		return ""
	}

	accelerator, err := acceleratorFromMacroKeys(keys)
	if err != nil {
		log.Printf("Macro %q has a recorded hotkey that cannot be used: %v", id, err)
		return ""
	}
	return accelerator
}

// startNodeMacroKeys digs the recorded keys out of a graph's Start node.
//
// The node data is a free-form map decoded from the macro's JSON, so every step
// down into it is checked: a hand-edited file, or one written by an older
// version, must not panic the app on startup.
func startNodeMacroKeys(flow *FlowData) []string {
	if flow == nil {
		return nil
	}

	for _, node := range flow.Nodes {
		if node.Type != "StartNode" {
			continue
		}
		raw, found := node.Data[macroKeysField]
		if !found {
			continue
		}
		entries, isSlice := raw.([]interface{})
		if !isSlice {
			continue
		}

		keys := make([]string, 0, len(entries))
		for _, entry := range entries {
			if key, isString := entry.(string); isString {
				keys = append(keys, key)
			}
		}
		return keys
	}
	return nil
}

// syncHotkeys makes the registered shortcuts match what the saved macros ask
// for. It runs at startup and again after every save, so recording a hotkey in
// the workspace and saving is all it takes for the key to start working.
//
// Nothing here is fatal. A hotkey the OS refuses - most often because another
// application already owns that combination - costs the user that one shortcut
// and is logged; refusing to start Keypress over it would be far worse. The
// recording stays in the macro, so it starts working again once whatever
// claimed it goes away.
//
// This is the function that makes hotkeyMux's invariant what it is: Register and
// Unregister below both marshal to the main thread and block until it answers,
// and both are called with the lock held. Nothing on the main thread may
// therefore wait for hotkeyMux - see its declaration in app.go.
func (a *App) syncHotkeys() {
	if a.wails == nil {
		return
	}

	// Read the macros before taking the lock: loading them goes through
	// ListProjects, which reads the registered hotkeys back for its summaries
	// and would deadlock against a lock held across it.
	desired := a.desiredHotkeys()

	a.hotkeyMux.Lock()
	defer a.hotkeyMux.Unlock()

	// Release first, so that a macro handing its accelerator over to another
	// one - or simply changing its own - is not rejected as a conflict with the
	// registration it is replacing.
	for id, current := range a.hotkeys {
		if desired[id] == current {
			continue
		}
		if err := a.wails.GlobalShortcut.Unregister(current); err != nil {
			log.Printf("syncHotkeys: could not release %q from macro %q: %v", current, id, err)
		}
		delete(a.hotkeys, id)
	}

	// In a fixed order, so that two macros recorded with the same chord - which
	// nothing stops the user doing - always resolve the same way instead of
	// depending on map iteration.
	ids := make([]string, 0, len(desired))
	for id := range desired {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		accelerator := desired[id]
		if a.hotkeys[id] == accelerator {
			continue
		}
		if err := a.wails.GlobalShortcut.Register(accelerator, a.hotkeyCallback(id)); err != nil {
			log.Printf("syncHotkeys: macro %q keeps no hotkey: %v", id, err)
			continue
		}
		a.hotkeys[id] = accelerator
		log.Printf("Registered hotkey %q for macro %q", accelerator, id)
	}
}

// desiredHotkeys reads every saved macro and reports the accelerator each one
// asks for, leaving out those that ask for none.
func (a *App) desiredHotkeys() map[string]string {
	summaries, err := a.ListProjects()
	if err != nil {
		log.Printf("desiredHotkeys: cannot read the macros: %v", err)
		return nil
	}

	desired := make(map[string]string, len(summaries))
	for _, summary := range summaries {
		flow, err := a.LoadProject(summary.ID)
		if err != nil {
			log.Printf("desiredHotkeys: skipping %q: %v", summary.ID, err)
			continue
		}
		if accelerator := a.macroAccelerator(flow, summary.ID); accelerator != "" {
			desired[summary.ID] = accelerator
		}
	}
	return desired
}

// hotkeyCallback is what a registered shortcut actually does. It is a function
// of the macro id alone, so a macro keeps working after the menu around it has
// been rebuilt.
func (a *App) hotkeyCallback(id string) func() {
	return func() {
		log.Printf("Hotkey fired for macro %q", id)
		a.runFromTray(id)
	}
}

// hotkeyFor reports the accelerator currently registered for a macro, empty
// when it has none.
//
// Deliberately what is registered rather than what the macro asked for: this
// feeds the macro list, and a card promising a hotkey the OS refused would be
// worse than one that shows nothing.
func (a *App) hotkeyFor(id string) string {
	a.hotkeyMux.Lock()
	defer a.hotkeyMux.Unlock()
	return a.hotkeys[id]
}
