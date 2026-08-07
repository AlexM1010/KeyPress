// hotkeys_test.go
//
// Covers reading a macro's global hotkey out of its Start node. Registration
// itself needs a running Wails application to claim the keys from the OS, so
// that is not tested here; the translation is, because it is where a hotkey the
// user recorded silently turns into no hotkey at all.

package backend

import (
	"testing"
)

func TestAcceleratorFromMacroKeys(t *testing.T) {
	tests := []struct {
		name string
		keys []string
		want string
	}{
		{
			// Exactly what the Start node records for Ctrl+Shift+J, in the
			// order the keys went down.
			name: "modifiers in press order",
			keys: []string{"Shift", "Control", "J"},
			want: "Ctrl+Shift+J",
		},
		{
			// The same chord pressed the other way round has to give the same
			// accelerator, or the two recordings would register separately.
			name: "modifier order does not matter",
			keys: []string{"Control", "Shift", "J"},
			want: "Ctrl+Shift+J",
		},
		{
			name: "single modifier",
			keys: []string{"Alt", "F5"},
			want: "Alt+F5",
		},
		{
			name: "meta is Wails' Super",
			keys: []string{"Meta", "K"},
			want: "Super+K",
		},
		{
			// macOS's name for Alt, offered by the node's own key picker.
			name: "option is alt",
			keys: []string{"Option", "P"},
			want: "Alt+P",
		},
		{
			name: "named key",
			keys: []string{"Control", "ArrowUp"},
			want: "Ctrl+Up",
		},
		{
			name: "space",
			keys: []string{"Control", " "},
			want: "Ctrl+Space",
		},
		{
			name: "lower case letter is upper cased",
			keys: []string{"Control", "j"},
			want: "Ctrl+J",
		},
		{
			name: "a duplicated modifier is recorded once",
			keys: []string{"Control", "Ctrl", "J"},
			want: "Ctrl+J",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := acceleratorFromMacroKeys(test.keys)
			if err != nil {
				t.Fatalf("acceleratorFromMacroKeys(%v): %v", test.keys, err)
			}
			if got != test.want {
				t.Errorf("acceleratorFromMacroKeys(%v) = %q, want %q", test.keys, got, test.want)
			}
		})
	}
}

func TestAcceleratorFromMacroKeysRejects(t *testing.T) {
	tests := []struct {
		name string
		keys []string
	}{
		// Registered system-wide, so this would take the key from every other
		// application on the machine.
		{name: "no modifier", keys: []string{"J"}},
		{name: "only modifiers", keys: []string{"Control", "Shift"}},
		{name: "nothing recorded", keys: nil},
		{name: "two keys", keys: []string{"Control", "J", "K"}},
		{name: "a key with no Wails name", keys: []string{"Control", "AudioVolumeUp"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := acceleratorFromMacroKeys(test.keys)
			if err == nil {
				t.Fatalf("acceleratorFromMacroKeys(%v) = %q, want an error", test.keys, got)
			}
		})
	}
}

func TestStartNodeMacroKeys(t *testing.T) {
	flow := sampleFlow()
	keys := startNodeMacroKeys(&flow)

	if len(keys) != 2 || keys[0] != "ctrl" || keys[1] != "j" {
		t.Fatalf("startNodeMacroKeys = %v, want [ctrl j]", keys)
	}
}

// The node data is a free-form map decoded from the macro's JSON, so a
// hand-edited file must not take the app down on startup.
func TestStartNodeMacroKeysToleratesRubbish(t *testing.T) {
	tests := []struct {
		name string
		data map[string]interface{}
	}{
		{name: "no macroKeys at all", data: map[string]interface{}{}},
		{name: "not a list", data: map[string]interface{}{macroKeysField: "Ctrl+J"}},
		{name: "list of non-strings", data: map[string]interface{}{macroKeysField: []interface{}{1, true}}},
		{name: "nil data", data: nil},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			flow := FlowData{Nodes: []Node{{ID: "start", Type: "StartNode", Data: test.data}}}
			if keys := startNodeMacroKeys(&flow); len(keys) != 0 {
				t.Fatalf("startNodeMacroKeys = %v, want none", keys)
			}
		})
	}
}

func TestStartNodeMacroKeysWithoutAStartNode(t *testing.T) {
	flow := FlowData{Nodes: []Node{{ID: "delay-1", Type: "DelayNode", Data: map[string]interface{}{}}}}
	if keys := startNodeMacroKeys(&flow); len(keys) != 0 {
		t.Fatalf("startNodeMacroKeys = %v, want none", keys)
	}
}

func TestStartNodeMacroKeysOnANilFlow(t *testing.T) {
	if keys := startNodeMacroKeys(nil); len(keys) != 0 {
		t.Fatalf("startNodeMacroKeys = %v, want none", keys)
	}
}

// ListProjects reports each macro's hotkey, and it reads it under the same lock
// syncHotkeys takes. This is the regression test for that being a deadlock: it
// hangs rather than fails if the two are ever nested again.
func TestListProjectsReportsHotkeys(t *testing.T) {
	useTempAppDirs(t)

	if _, _, err := saveMacro(sampleFlow(), "Mouse Jiggler", ""); err != nil {
		t.Fatalf("saveMacro: %v", err)
	}

	app := NewApp()
	app.hotkeys["mouse-jiggler"] = "Ctrl+J"

	summaries, err := app.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("got %d macros, want 1", len(summaries))
	}
	if summaries[0].Hotkey != "Ctrl+J" {
		t.Errorf("Hotkey = %q, want %q", summaries[0].Hotkey, "Ctrl+J")
	}
}

// The end of the chain that was broken: a macro saved with a recorded trigger
// must resolve to the accelerator Wails is asked to register.
func TestMacroAcceleratorFromASavedMacro(t *testing.T) {
	useTempAppDirs(t)

	flow := sampleFlow()
	flow.Nodes[0].Data[macroKeysField] = []interface{}{"Shift", "Control", "J"}
	if _, _, err := saveMacro(flow, "Test", ""); err != nil {
		t.Fatalf("saveMacro: %v", err)
	}

	app := NewApp()
	loaded, err := app.LoadProject("test")
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}

	if got := app.macroAccelerator(loaded, "test"); got != "Ctrl+Shift+J" {
		t.Fatalf("macroAccelerator = %q, want %q", got, "Ctrl+Shift+J")
	}
}
