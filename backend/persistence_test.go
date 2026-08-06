// persistence_test.go
//
// Tests for the save/load path. Everything here runs against a data directory
// of its own (see useTempAppDirs) - no test may touch the macros of whoever is
// running it.

package backend

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"Keypress/backend/utils"

	"github.com/adrg/xdg"
)

// useTempAppDirs points the app's data and config directories at fresh
// temporary ones for the duration of a test.
//
// xdg resolves those directories once at package init and caches them, so the
// environment change only takes effect after a Reload; the same Reload runs
// again on cleanup so the next test - and anything else in the process - is not
// left looking at a directory that has been deleted. t.Setenv makes the test
// non-parallel, which is exactly right for state this global.
//
// It returns the macro data directory, created and empty.
func useTempAppDirs(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	xdg.Reload()
	t.Cleanup(xdg.Reload)

	dataDir, err := utils.GetAppDataDir()
	if err != nil {
		t.Fatalf("GetAppDataDir: %v", err)
	}
	return dataDir
}

// sampleFlow is a small but complete graph, so that a round trip has something
// to lose.
func sampleFlow() FlowData {
	return FlowData{
		Nodes: []Node{
			{
				ID:       "startnode-1",
				Type:     "StartNode",
				Data:     map[string]interface{}{"macroKeys": []interface{}{"ctrl", "j"}},
				Position: map[string]float64{"x": 100, "y": 200},
			},
			{
				ID:       "delaynode-1",
				Type:     "DelayNode",
				Data:     map[string]interface{}{"delayType": "Fixed", "time": float64(1000)},
				Position: map[string]float64{"x": 400, "y": 200},
			},
		},
		Edges: []Edge{
			{ID: "edge-1", Source: "startnode-1", Target: "delaynode-1", SourceHandle: "right", TargetHandle: "left"},
		},
	}
}

// readMacroFile parses the macro stored under id, failing the test if it is not
// there or does not parse.
func readMacroFile(t *testing.T, dataDir string, id string) FlowData {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(dataDir, id+projectFileExt))
	if err != nil {
		t.Fatalf("reading macro %q: %v", id, err)
	}

	var flow FlowData
	if err := json.Unmarshal(data, &flow); err != nil {
		t.Fatalf("macro %q is not valid JSON: %v", id, err)
	}
	return flow
}

// dirEntryNames lists what is actually in the data directory, which is how the
// temp-file tests catch a scratch file that was left behind.
func dirEntryNames(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %q: %v", dir, err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestSaveMacroWritesTheGraph(t *testing.T) {
	dataDir := useTempAppDirs(t)

	id, stale, err := saveMacro(sampleFlow(), "My Macro", "")
	if err != nil {
		t.Fatalf("saveMacro: %v", err)
	}
	if id != "my-macro" {
		t.Errorf("id = %q, want %q", id, "my-macro")
	}
	if stale != "" {
		t.Errorf("stale file = %q, want none", stale)
	}

	saved := readMacroFile(t, dataDir, id)
	if saved.Name != "My Macro" {
		t.Errorf("saved name = %q, want %q", saved.Name, "My Macro")
	}
	if len(saved.Nodes) != 2 || len(saved.Edges) != 1 {
		t.Errorf("saved %d nodes and %d edges, want 2 and 1", len(saved.Nodes), len(saved.Edges))
	}
	if saved.Nodes[1].Data["delayType"] != "Fixed" {
		t.Errorf("node data did not round trip: %#v", saved.Nodes[1].Data)
	}
	if saved.Nodes[0].Position["x"] != 100 {
		t.Errorf("node position did not round trip: %#v", saved.Nodes[0].Position)
	}
}

// The filename is the id, so a copy of it inside the file would only give the
// two a way to disagree after a rename.
func TestSaveMacroKeepsTheIDOutOfTheFile(t *testing.T) {
	dataDir := useTempAppDirs(t)

	flow := sampleFlow()
	flow.ID = "some-other-macro"
	if _, _, err := saveMacro(flow, "My Macro", ""); err != nil {
		t.Fatalf("saveMacro: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dataDir, "my-macro"+projectFileExt))
	if err != nil {
		t.Fatalf("reading macro: %v", err)
	}
	if strings.Contains(string(raw), "some-other-macro") {
		t.Errorf("the file carries an id:\n%s", raw)
	}
}

// The last-opened pointer is what reopens the macro at startup, and it holds an
// id rather than a path.
func TestSaveMacroRecordsTheMacroAsLastOpened(t *testing.T) {
	useTempAppDirs(t)

	id, _, err := saveMacro(sampleFlow(), "My Macro", "")
	if err != nil {
		t.Fatalf("saveMacro: %v", err)
	}

	if got := utils.LoadConfig().LastOpenedProject; got != id {
		t.Errorf("last opened = %q, want %q", got, id)
	}
}

func TestSaveMacroRejectsBadNames(t *testing.T) {
	tests := []struct {
		name        string
		displayName string
		wantErr     string
	}{
		{"empty", "", "name is required"},
		{"no letters or digits", "!!! ***", "no letters or digits"},
		{"too long", strings.Repeat("a", maxMacroNameLen+1), "too long"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dataDir := useTempAppDirs(t)

			_, _, err := saveMacro(sampleFlow(), test.displayName, "")
			if err == nil {
				t.Fatalf("saveMacro(%q) succeeded, want an error", test.displayName)
			}
			if !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("error = %q, want it to mention %q", err, test.wantErr)
			}
			if names := dirEntryNames(t, dataDir); len(names) != 0 {
				t.Errorf("a rejected save left %v behind", names)
			}
		})
	}
}

// A name exactly at the limit is accepted: the bound is on what is too long,
// not on what is long.
func TestSaveMacroAcceptsANameAtTheLimit(t *testing.T) {
	useTempAppDirs(t)

	if _, _, err := saveMacro(sampleFlow(), strings.Repeat("a", maxMacroNameLen), ""); err != nil {
		t.Fatalf("saveMacro: %v", err)
	}
}

func TestSaveMacroRejectsADuplicateName(t *testing.T) {
	dataDir := useTempAppDirs(t)

	if _, _, err := saveMacro(sampleFlow(), "My Macro", ""); err != nil {
		t.Fatalf("first save: %v", err)
	}

	// A different graph, saved under a name that slugifies to the same id, from
	// a workspace holding a different macro.
	second := sampleFlow()
	second.Nodes = second.Nodes[:1]
	_, _, err := saveMacro(second, "my  macro", "something-else")
	if err == nil {
		t.Fatal("the second save succeeded, want it refused as a duplicate")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %q, want it to mention that the name exists", err)
	}

	// The refusal must not have touched the macro already there.
	if saved := readMacroFile(t, dataDir, "my-macro"); len(saved.Nodes) != 2 {
		t.Errorf("the existing macro now has %d nodes, want the original 2", len(saved.Nodes))
	}
}

func TestSaveMacroOverwritesItsOwnFile(t *testing.T) {
	dataDir := useTempAppDirs(t)

	id, _, err := saveMacro(sampleFlow(), "My Macro", "")
	if err != nil {
		t.Fatalf("first save: %v", err)
	}

	edited := sampleFlow()
	edited.Nodes[1].Data["time"] = float64(2500)
	secondID, stale, err := saveMacro(edited, "My Macro", id)
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	if secondID != id {
		t.Errorf("id changed from %q to %q", id, secondID)
	}
	if stale != "" {
		t.Errorf("stale file = %q, want none - nothing was renamed", stale)
	}

	if saved := readMacroFile(t, dataDir, id); saved.Nodes[1].Data["time"] != float64(2500) {
		t.Errorf("the edit was not saved: %#v", saved.Nodes[1].Data)
	}
	if names := dirEntryNames(t, dataDir); len(names) != 1 {
		t.Errorf("data directory holds %v, want one macro file", names)
	}
}

// Renaming the open macro moves it: one macro, under the new name.
func TestSaveMacroRenameRemovesTheOldFile(t *testing.T) {
	dataDir := useTempAppDirs(t)

	oldID, _, err := saveMacro(sampleFlow(), "First Name", "")
	if err != nil {
		t.Fatalf("first save: %v", err)
	}

	newID, stale, err := saveMacro(sampleFlow(), "Second Name", oldID)
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if newID != "second-name" {
		t.Errorf("id = %q, want %q", newID, "second-name")
	}
	if stale != "" {
		t.Errorf("stale file = %q, want the old file removed", stale)
	}

	if _, err := os.Stat(filepath.Join(dataDir, oldID+projectFileExt)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the old file is still there (stat error: %v)", err)
	}
	readMacroFile(t, dataDir, newID)
}

// A rename onto a name someone else already owns is refused, and refusing it
// must not cost the user either macro.
func TestSaveMacroRenameOntoATakenNameKeepsBothMacros(t *testing.T) {
	dataDir := useTempAppDirs(t)

	if _, _, err := saveMacro(sampleFlow(), "Taken", ""); err != nil {
		t.Fatalf("saving the other macro: %v", err)
	}
	mine, _, err := saveMacro(sampleFlow(), "Mine", "")
	if err != nil {
		t.Fatalf("saving my macro: %v", err)
	}

	if _, _, err := saveMacro(sampleFlow(), "Taken", mine); err == nil {
		t.Fatal("the rename succeeded, want it refused")
	}

	for _, id := range []string{"taken", "mine"} {
		if _, err := os.Stat(filepath.Join(dataDir, id+projectFileExt)); err != nil {
			t.Errorf("macro %q is gone: %v", id, err)
		}
	}
}

// currentID arrives from the frontend and decides which file gets deleted, so
// it goes through the same validation as any other id.
func TestSaveMacroRejectsAnUnsafeCurrentID(t *testing.T) {
	dataDir := useTempAppDirs(t)

	_, _, err := saveMacro(sampleFlow(), "My Macro", filepath.Join("..", "escape"))
	if err == nil {
		t.Fatal("saveMacro accepted a traversing currentID")
	}
	if !strings.Contains(err.Error(), "invalid open macro id") {
		t.Errorf("error = %q, want it to name the open macro id", err)
	}
	if names := dirEntryNames(t, dataDir); len(names) != 0 {
		t.Errorf("the rejected save left %v behind", names)
	}
}

// A graph the user emptied is still a macro. It used to marshal "nodes": null,
// which ListProjects reads as "not a flow file" - so the save worked and the
// macro then vanished from the list.
func TestSaveMacroKeepsAnEmptyGraphListable(t *testing.T) {
	dataDir := useTempAppDirs(t)

	id, _, err := saveMacro(FlowData{}, "Emptied", "")
	if err != nil {
		t.Fatalf("saveMacro: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dataDir, id+projectFileExt))
	if err != nil {
		t.Fatalf("reading macro: %v", err)
	}
	if strings.Contains(string(raw), "null") {
		t.Errorf("the file has a null array:\n%s", raw)
	}

	summaries, err := (&App{}).ListProjects()
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(summaries) != 1 || summaries[0].ID != id {
		t.Errorf("ListProjects = %#v, want just %q", summaries, id)
	}
}

// The scratch file a save is written to must never survive the save, or the
// data directory silently fills with them.
func TestSaveMacroLeavesNoTemporaryFiles(t *testing.T) {
	dataDir := useTempAppDirs(t)

	id, _, err := saveMacro(sampleFlow(), "My Macro", "")
	if err != nil {
		t.Fatalf("first save: %v", err)
	}
	if _, _, err := saveMacro(sampleFlow(), "My Macro", id); err != nil {
		t.Fatalf("second save: %v", err)
	}
	if _, _, err := saveMacro(sampleFlow(), "Renamed", id); err != nil {
		t.Fatalf("rename: %v", err)
	}

	names := dirEntryNames(t, dataDir)
	if len(names) != 1 || names[0] != "renamed"+projectFileExt {
		t.Errorf("data directory holds %v, want just the renamed macro", names)
	}
}

// The whole point of writing through a temporary file: what is already on disk
// survives a save that cannot be completed. The destination is made unwritable
// by putting a directory in its place, which is the one failure that can be
// staged portably.
func TestWriteMacroFileLeavesTheDestinationAloneOnFailure(t *testing.T) {
	dataDir := useTempAppDirs(t)

	// A directory where the macro file should be: the rename cannot replace it.
	fullPath := filepath.Join(dataDir, "blocked"+projectFileExt)
	if err := os.Mkdir(fullPath, 0755); err != nil {
		t.Fatalf("staging the blocked destination: %v", err)
	}

	if err := writeMacroFile(fullPath, []byte("{}\n"), true); err == nil {
		t.Fatal("writeMacroFile succeeded, want the blocked destination to fail it")
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		t.Fatalf("the destination is gone: %v", err)
	}
	if !info.IsDir() {
		t.Error("the destination was replaced")
	}
	if names := dirEntryNames(t, dataDir); len(names) != 1 {
		t.Errorf("the failed write left %v behind", names)
	}
}

func TestListProjectsSkipsWhatIsNotAMacro(t *testing.T) {
	dataDir := useTempAppDirs(t)

	if _, _, err := saveMacro(sampleFlow(), "Real Macro", ""); err != nil {
		t.Fatalf("saveMacro: %v", err)
	}

	write := func(name string, contents string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dataDir, name), []byte(contents), 0644); err != nil {
			t.Fatalf("staging %q: %v", name, err)
		}
	}
	write("broken.json", "{not json")
	write("settings.json", `{"theme":"dark"}`) // parses, but has no nodes
	write("notes.txt", "nothing to do with macros")
	if err := os.Mkdir(filepath.Join(dataDir, "subdir.json"), 0755); err != nil {
		t.Fatalf("staging a directory: %v", err)
	}

	summaries, err := (&App{}).ListProjects()
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("ListProjects = %#v, want just the real macro", summaries)
	}
	if summaries[0].ID != "real-macro" || summaries[0].Name != "Real Macro" {
		t.Errorf("summary = %#v, want the real macro's id and name", summaries[0])
	}
	if summaries[0].NodeCount != 2 || summaries[0].EdgeCount != 1 {
		t.Errorf("counts = %d nodes, %d edges; want 2 and 1", summaries[0].NodeCount, summaries[0].EdgeCount)
	}
	if summaries[0].ModifiedAt == "" {
		t.Error("summary has no modification time")
	}
}

// LoadProject fills in the id, which is not in the file, so the workspace can
// save the macro back over its own file instead of being told the name is taken.
func TestLoadProjectRoundTripsTheMacro(t *testing.T) {
	useTempAppDirs(t)

	id, _, err := saveMacro(sampleFlow(), "My Macro", "")
	if err != nil {
		t.Fatalf("saveMacro: %v", err)
	}

	loaded, err := (&App{}).LoadProject(id)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if loaded.ID != id {
		t.Errorf("loaded id = %q, want %q", loaded.ID, id)
	}
	if loaded.Name != "My Macro" {
		t.Errorf("loaded name = %q, want %q", loaded.Name, "My Macro")
	}
	if len(loaded.Nodes) != 2 || len(loaded.Edges) != 1 {
		t.Errorf("loaded %d nodes and %d edges, want 2 and 1", len(loaded.Nodes), len(loaded.Edges))
	}
}

func TestLoadProjectRejectsAnUnsafeID(t *testing.T) {
	useTempAppDirs(t)

	for _, id := range []string{"", "..", "../escape", `..\escape`, "/etc/passwd", ".hidden"} {
		if _, err := (&App{}).LoadProject(id); err == nil {
			t.Errorf("LoadProject(%q) succeeded, want it refused", id)
		}
	}
}

// A macro recorded as last opened but since deleted is not an error - the app
// opens on an empty canvas.
func TestLoadLastFileTreatsAMissingMacroAsNothingToLoad(t *testing.T) {
	useTempAppDirs(t)

	if err := utils.SetLastOpenedProject("gone"); err != nil {
		t.Fatalf("SetLastOpenedProject: %v", err)
	}

	flow, err := (&App{}).LoadLastFile()
	if err != nil {
		t.Fatalf("LoadLastFile: %v", err)
	}
	if flow != nil {
		t.Errorf("LoadLastFile = %#v, want nil", flow)
	}
}

// The settings file is hand-editable, so an id in it gets the same validation as
// one from the frontend - and a bad one costs the last opened macro, not the app.
func TestLoadLastFileIgnoresAnUnsafeRecordedID(t *testing.T) {
	useTempAppDirs(t)

	if err := utils.SetLastOpenedProject(`..\..\somewhere-else`); err != nil {
		t.Fatalf("SetLastOpenedProject: %v", err)
	}

	flow, err := (&App{}).LoadLastFile()
	if err != nil {
		t.Fatalf("LoadLastFile: %v", err)
	}
	if flow != nil {
		t.Errorf("LoadLastFile = %#v, want nil", flow)
	}
}

func TestLoadLastFileReturnsTheSavedMacro(t *testing.T) {
	useTempAppDirs(t)

	id, _, err := saveMacro(sampleFlow(), "My Macro", "")
	if err != nil {
		t.Fatalf("saveMacro: %v", err)
	}

	flow, err := (&App{}).LoadLastFile()
	if err != nil {
		t.Fatalf("LoadLastFile: %v", err)
	}
	if flow == nil {
		t.Fatal("LoadLastFile = nil, want the macro just saved")
	}
	if flow.ID != id || flow.Name != "My Macro" {
		t.Errorf("loaded %q / %q, want %q / %q", flow.ID, flow.Name, id, "My Macro")
	}
}

func TestSanitizeProjectID(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		want    string
		wantErr bool
	}{
		{"plain", "my-macro", "my-macro", false},
		{"with extension", "my-macro.json", "my-macro", false},
		{"extension in any case", "my-macro.JSON", "my-macro", false},
		{"surrounding space", "  my-macro  ", "my-macro", false},
		{"underscores and digits", "macro_2", "macro_2", false},

		{"empty", "", "", true},
		{"only space", "   ", "", true},
		{"only an extension", ".json", "", true},
		{"parent directory", "..", "", true},
		{"traversal", "../secrets", "", true},
		{"windows traversal", `..\secrets`, "", true},
		{"forward slash", "sub/macro", "", true},
		{"backslash", `sub\macro`, "", true},
		{"volume letter", "C:macro", "", true},
		{"absolute", "/etc/passwd", "", true},
		{"leading dot", ".hidden", "", true},
		{"too long", strings.Repeat("a", maxProjectIDLen+1), "", true},
		{"unsupported character", "macro*", "", true},
		{"newline", "macro\nname", "", true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := sanitizeProjectID(test.id)
			if test.wantErr {
				if err == nil {
					t.Fatalf("sanitizeProjectID(%q) = %q, want an error", test.id, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("sanitizeProjectID(%q): %v", test.id, err)
			}
			if got != test.want {
				t.Errorf("sanitizeProjectID(%q) = %q, want %q", test.id, got, test.want)
			}
		})
	}
}

func TestSlugifyProjectID(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"lowercases", "My Macro", "my-macro"},
		{"collapses runs", "my   macro", "my-macro"},
		{"drops punctuation", "My Macro!!", "my-macro"},
		{"keeps dashes and underscores", "my-macro_2", "my-macro_2"},
		{"trims edges", "  -my macro-  ", "my-macro"},
		{"digits", "macro 42", "macro-42"},
		{"nothing usable", "!!! ***", ""},
		{"non-ascii only", "日本語", ""},
		{"bounded", strings.Repeat("a", maxProjectIDLen+50), strings.Repeat("a", maxProjectIDLen)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := slugifyProjectID(test.in); got != test.want {
				t.Errorf("slugifyProjectID(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}

// Whatever slugifyProjectID produces has to be something sanitizeProjectID will
// take back, or a macro could be saved and never loaded again.
func TestSlugsAreValidProjectIDs(t *testing.T) {
	names := []string{
		"My Macro",
		"macro 42",
		"  spaced  out  ",
		"punctuation!?#$%^&*()",
		"dashes---and___underscores",
		strings.Repeat("long ", 60),
		"日本語 macro",
	}

	for _, name := range names {
		slug := slugifyProjectID(name)
		if slug == "" {
			continue // rejected before it reaches a filename; covered above
		}
		if _, err := sanitizeProjectID(slug); err != nil {
			t.Errorf("slug %q from name %q is not a valid id: %v", slug, name, err)
		}
	}
}

func TestProjectDisplayName(t *testing.T) {
	tests := []struct {
		name   string
		stored string
		id     string
		want   string
	}{
		{"stored name wins", "My Macro", "my-macro", "My Macro"},
		{"blank stored name falls back", "   ", "my-macro", "My Macro"},
		{"underscores too", "", "mouse_jiggler", "Mouse Jiggler"},
		{"single word", "", "macro", "Macro"},
		{"nothing to split", "", "-", "-"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := projectDisplayName(test.stored, test.id); got != test.want {
				t.Errorf("projectDisplayName(%q, %q) = %q, want %q", test.stored, test.id, got, test.want)
			}
		})
	}
}

func TestProjectPathStaysInTheDataDirectory(t *testing.T) {
	dataDir := useTempAppDirs(t)

	fullPath, id, err := projectPath("my-macro")
	if err != nil {
		t.Fatalf("projectPath: %v", err)
	}
	if id != "my-macro" {
		t.Errorf("id = %q, want %q", id, "my-macro")
	}
	if want := filepath.Join(dataDir, "my-macro"+projectFileExt); fullPath != want {
		t.Errorf("path = %q, want %q", fullPath, want)
	}

	if _, _, err := projectPath("../escape"); err == nil {
		t.Error("projectPath accepted a traversing id")
	}
}
