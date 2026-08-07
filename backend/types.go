// types.go
//
// Data structures exchanged with the frontend (flowchart nodes/edges) and the
// internal task representation derived from them.

package backend

// Node represents a single node in the flowchart.
type Node struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Data     map[string]interface{} `json:"data"`
	Position map[string]float64     `json:"position"`
}

// Edge represents a connection between two nodes.
type Edge struct {
	ID           string `json:"id"`
	Source       string `json:"source"`
	Target       string `json:"target"`
	SourceHandle string `json:"sourceHandle,omitempty"`
	TargetHandle string `json:"targetHandle,omitempty"`
	Type         string `json:"type,omitempty"`
}

// FlowData represents the complete flow chart data structure.
//
// Name is the display name the user gave the macro. It is omitted rather than
// written empty so that flow files saved before macros were nameable still
// round-trip unchanged; readers fall back to a name derived from the filename.
//
// ID is the macro's id on disk - its bare filename, no extension. The loaders
// fill it in so the frontend knows which macro it has open and can save back
// over that file instead of being told the name is already taken. It is never
// written into the file: the filename IS the id, and a second copy inside would
// only give the two a way to disagree after a rename. SaveFile clears it before
// encoding, and the omitempty keeps it out of the JSON either way.
type FlowData struct {
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// ProjectSummary describes one saved macro without loading its whole graph.
// It is what the projects listing in the frontend renders.
type ProjectSummary struct {
	// ID is the bare filename (no extension) of the macro on disk, and the
	// only handle the frontend may pass back to LoadProject.
	ID   string `json:"id"`
	Name string `json:"name"`

	NodeCount int `json:"nodeCount"`
	EdgeCount int `json:"edgeCount"`

	// ModifiedAt is the file's modification time as an RFC 3339 string.
	// It is deliberately not a time.Time: Wails maps that to an untyped
	// field in the generated models, and the frontend bans `any`.
	ModifiedAt string `json:"modifiedAt"`

	// Hotkey is the system-wide shortcut that runs this macro, empty when it
	// has none. It is stored in the settings file rather than in the macro, so
	// it is filled in here from there - see App.hotkeyFor.
	Hotkey string `json:"hotkey"`
}

// MacroRun identifies the macro behind a run that the workspace did not start
// itself. It is the payload of the "macro-started" event, which the tray and
// the global hotkeys emit so the frontend can tell whose task events these are
// - it may have a different macro open, or no window on screen at all.
type MacroRun struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Task represents a single executable task.
type Task struct {
	ID   string
	Type string
	Data map[string]interface{}
}
