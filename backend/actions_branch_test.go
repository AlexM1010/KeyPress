// actions_branch_test.go
//
// The condition language, the Branch node that reads it, and the "store result
// as" field that writes the values it reads.
//
// The comparison table below is the specification. Every row of it is a
// decision someone would otherwise have to reverse-engineer from the
// implementation - what greaterThan means on two words, whether "3" and 3 are
// the same value, what an unset variable answers - so each is written here as a
// claim with a reason attached.
//
// The constraints of tasks_test.go apply: events are observed through the log,
// because a test App has no Wails application to emit to.

package backend

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// =============================================== store result as ===============================================

func TestStoreResultRecordsTheValueUnderTheNameThePayloadAsksFor(t *testing.T) {
	state := newRunState()

	storeResult(state, map[string]interface{}{"storeResultAs": "found"}, "00ff00")

	got, ok := state.value("found")
	if !ok {
		t.Fatal("nothing was stored under the name the node asked for")
	}
	if got != "00ff00" {
		t.Errorf("value(found) = %v, want the colour the node matched", got)
	}
}

// A name with spaces round it finds the value a condition looks for under the
// trimmed name, because the condition trims too. Without both, a trailing space
// in a text input is an invisible reason a macro does nothing.
func TestStoreResultTrimsTheName(t *testing.T) {
	state := newRunState()

	storeResult(state, map[string]interface{}{"storeResultAs": "  found  "}, "00ff00")

	if _, ok := state.value("found"); !ok {
		t.Errorf("the name was not trimmed: %v", state.values)
	}
}

// The distinction the whole feature rests on: a node that was not asked to
// store anything leaves the name unset, rather than writing an empty value into
// it. isSet is the operator that can tell the two apart, and it would answer
// yes to a name that had been written empty.
func TestStoreResultStoresNothingWithoutAName(t *testing.T) {
	for _, payload := range []map[string]interface{}{
		{},
		{"storeResultAs": ""},
		{"storeResultAs": "   "},
		{"storeResultAs": 42.0},
	} {
		state := newRunState()

		storeResult(state, payload, "00ff00")

		if len(state.values) != 0 {
			t.Errorf("payload %v stored %v, want nothing at all", payload, state.values)
		}
	}
}

// Two numbers under one name, by the .x / .y convention, because the map holds
// one value per name and a position is two. Numbers rather than a packed
// string: a comparison can be made of these.
func TestStorePositionWritesXAndYAsNumbers(t *testing.T) {
	state := newRunState()

	storePosition(state, map[string]interface{}{"storeResultAs": "target"}, 800, 600)

	x, okX := state.value("target" + positionSuffixX)
	y, okY := state.value("target" + positionSuffixY)
	if !okX || !okY {
		t.Fatalf("the position was not stored under both names: %v", state.values)
	}
	if x != float64(800) || y != float64(600) {
		t.Errorf("stored (%v, %v), want (800, 600) as numbers", x, y)
	}
}

func TestStorePositionStoresNothingWithoutAName(t *testing.T) {
	state := newRunState()

	storePosition(state, map[string]interface{}{}, 800, 600)

	if len(state.values) != 0 {
		t.Errorf("a node that asked for nothing stored %v", state.values)
	}
}

// A handler called by something that is not a run - a test driving one handler,
// today - stores nothing rather than panicking, so a nil state cannot take the
// app down from inside a mouse move.
func TestStoringWithNoRunStateDoesNothing(t *testing.T) {
	storeResult(nil, map[string]interface{}{"storeResultAs": "found"}, "00ff00")
	storePosition(nil, map[string]interface{}{"storeResultAs": "target"}, 1, 2)

	if _, ok := (*runState)(nil).value("found"); ok {
		t.Error("a nil run state reported a name as set")
	}
}

// =============================================== the comparison table ===============================================

// The whole condition language, row by row. Each row names the rule it pins.
func TestConditionHolds(t *testing.T) {
	cases := []struct {
		name     string
		stored   map[string]any
		variable string
		operator string
		value    any
		want     bool
	}{
		// isSet is how a macro asks the question directly, and the only
		// operator that means anything about a name nothing has written to.
		{"isSet on a name nothing stored", nil, "found", opIsSet, nil, false},
		{"isSet on a stored value", map[string]any{"found": "00ff00"}, "found", opIsSet, nil, true},
		{"isSet on a value stored empty", map[string]any{"found": ""}, "found", opIsSet, nil, true},

		// An unset variable makes every other condition false. It is not an
		// error: "did that click work?" is a question a macro may ask before
		// anything has answered it, and the answer before anything has is no.
		// notEquals included deliberately - the rule is about the variable, not
		// about the comparison.
		{"equals on an unset variable", nil, "found", opEquals, "00ff00", false},
		{"notEquals on an unset variable", nil, "found", opNotEquals, "00ff00", false},
		{"greaterThan on an unset variable", nil, "attempts", opGreaterThan, 0.0, false},
		{"contains on an unset variable", nil, "found", opContains, "ff", false},

		// Strings compare exactly, and case matters.
		{"equals on the same string", map[string]any{"found": "00ff00"}, "found", opEquals, "00ff00", true},
		{"equals is case-sensitive", map[string]any{"found": "00FF00"}, "found", opEquals, "00ff00", false},
		{"notEquals on a different string", map[string]any{"found": "00ff00"}, "found", opNotEquals, "ff0000", true},

		// If both sides parse as numbers they are compared as numbers, which is
		// what makes greaterThan mean what a user expects of a count. "10" and
		// 10 are the same value: one arrives from a payload, the other from a
		// node that stored a number, and a user who typed 10 into a text box
		// did not mean something else by it.
		{"a stored number equals the same number typed as text", map[string]any{"attempts": 3.0}, "attempts", opEquals, "3", true},
		{"a stored numeric string equals the same number", map[string]any{"attempts": "3"}, "attempts", opEquals, 3.0, true},
		{"greaterThan on numbers", map[string]any{"attempts": 10.0}, "attempts", opGreaterThan, 9.0, true},
		{"greaterThan on numeric strings is numeric, not lexicographic", map[string]any{"attempts": "10"}, "attempts", opGreaterThan, "9", true},
		{"lessThan on equal numbers", map[string]any{"attempts": 5.0}, "attempts", opLessThan, 5.0, false},
		{"a stored position compares as a number", map[string]any{"target" + positionSuffixX: 800.0}, "target.x", opGreaterThan, 500.0, true},

		// Two non-numeric strings order lexicographically - Go's own byte-wise
		// comparison, so upper case sorts before lower.
		{"lessThan on two words", map[string]any{"name": "apple"}, "name", opLessThan, "banana", true},
		{"greaterThan on two words", map[string]any{"name": "banana"}, "name", opGreaterThan, "apple", true},
		{"upper case sorts before lower", map[string]any{"name": "Zebra"}, "name", opLessThan, "apple", true},
		{"one side numeric falls back to strings", map[string]any{"name": "abc"}, "name", opGreaterThan, 5.0, true},

		// contains is a substring test on the string forms, always - even when
		// both sides are numbers, because that is the only reading that does
		// not depend on how the value got into the run state.
		{"contains a substring", map[string]any{"found": "00ff00"}, "found", opContains, "ff", true},
		{"contains something that is not there", map[string]any{"found": "00ff00"}, "found", opContains, "abc", false},
		{"contains on the string form of a number", map[string]any{"attempts": 1024.0}, "attempts", opContains, "02", true},
		{"everything contains the empty string", map[string]any{"found": "00ff00"}, "found", opContains, "", true},

		// A stored number's string form is the spelling a user would type, not
		// a float's default one - so a whole number carries no decimal point to
		// find. (equals could not show this: "3.000000" parses as a number, so
		// that comparison is numeric and true.)
		{"a whole number's string form has no decimal point", map[string]any{"attempts": 3.0}, "attempts", opContains, ".", false},
		{"a fractional number keeps its point", map[string]any{"ratio": 1.5}, "ratio", opContains, "1.5", true},

		// A missing value field is the empty string, which is what makes
		// `equals ""` expressible.
		{"a missing value compares as empty", map[string]any{"found": ""}, "found", opEquals, nil, true},

		// The name is trimmed on the way in, matching what storeResult does on
		// the way out.
		{"the variable name is trimmed", map[string]any{"found": "00ff00"}, "  found  ", opIsSet, nil, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := newRunState()
			for name, value := range tc.stored {
				state.set(name, value)
			}

			got, err := conditionHolds(state, tc.variable, tc.operator, tc.value)
			if err != nil {
				t.Fatalf("conditionHolds returned %v, want a decision", err)
			}
			if got != tc.want {
				t.Errorf("conditionHolds(%q %s %v) = %v, want %v", tc.variable, tc.operator, tc.value, got, tc.want)
			}
		})
	}
}

// An operator the node's UI cannot produce is a malformed payload, not a branch
// to guess at. Checked before the variable is read, so it is reported whether
// or not the name happens to be set.
func TestConditionHoldsRefusesAnUnknownOperator(t *testing.T) {
	for _, operator := range []string{"", "matches", "GREATERTHAN", "greaterThanOrEqual"} {
		state := newRunState()
		state.set("attempts", 3.0)

		if _, err := conditionHolds(state, "attempts", operator, 1.0); err == nil {
			t.Errorf("operator %q was accepted, want it refused as a malformed payload", operator)
		}
	}

	if _, err := conditionHolds(newRunState(), "nothing-stored-here", "matches", ""); err == nil {
		t.Error("an unknown operator on an unset variable was accepted, want the payload refused either way")
	}
}

// =============================================== the Branch node ===============================================

func TestExecuteBranchTaskLeavesByTheTrueOutput(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)

	state := newRunState()
	state.set("found", "00ff00")

	next, err := executeBranchTask(runningContext(t), Task{
		ID:   "branch-1",
		Type: "BranchNode",
		Data: map[string]interface{}{"variable": "found", "operator": opIsSet, "value": ""},
	}, state, app)

	if err != nil {
		t.Errorf("executeBranchTask returned %v, want no error", err)
	}
	if next != branchTrue {
		t.Errorf("next = %q, want %q", next, branchTrue)
	}
	if !logs.contains(emittedEvent("task-success")) {
		t.Errorf("no task-success event was emitted:\n%s", logs)
	}
	if logs.contains(emittedEvent("task-error")) {
		t.Errorf("a branch that picked an output reported an error:\n%s", logs)
	}
}

// "false" is an output like any other. A branch never returns "" - that would
// mean "the only output" and take both edges, running both sides of the
// condition.
func TestExecuteBranchTaskLeavesByTheFalseOutput(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)

	next, err := executeBranchTask(runningContext(t), Task{
		ID:   "branch-1",
		Type: "BranchNode",
		Data: map[string]interface{}{"variable": "found", "operator": opIsSet, "value": ""},
	}, newRunState(), app)

	if err != nil {
		t.Errorf("executeBranchTask returned %v, want no error", err)
	}
	if next != branchFalse {
		t.Errorf("next = %q, want %q", next, branchFalse)
	}
	if !logs.contains(emittedEvent("task-success")) {
		t.Errorf("a condition that did not hold is still a node that ran:\n%s", logs)
	}
}

// A malformed condition is reported the way every other handler reports a
// malformed payload - the event for the status panel, the same value back to
// the caller - rather than silently picking a branch.
func TestExecuteBranchTaskReportsAnUnknownOperator(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)

	next, err := executeBranchTask(runningContext(t), Task{
		ID:   "branch-1",
		Type: "BranchNode",
		Data: map[string]interface{}{"variable": "found", "operator": "matches", "value": "ff"},
	}, newRunState(), app)

	if err == nil {
		t.Fatal("executeBranchTask accepted an operator no node can produce")
	}
	if !strings.Contains(err.Error(), "matches") {
		t.Errorf("error = %q, want it to name the operator it could not read", err)
	}
	// "No output taken at all" is endPathHandle. This asserted "", which is the
	// opposite - "the only output", every outgoing edge - so a branch that could
	// not read its payload ran both of its sides. The message was right and the
	// assertion pinned the bug.
	if next != endPathHandle {
		t.Errorf("next = %q, want no output taken at all (%q)", next, endPathHandle)
	}
	if !logs.contains(emittedEvent("task-error")) {
		t.Errorf("no task-error event was emitted:\n%s", logs)
	}
	if logs.contains(emittedEvent("task-success")) {
		t.Errorf("a branch with a payload it could not read also reported success:\n%s", logs)
	}
}

// Through executeTask, because the dispatch is the part being tested: a node
// type the switch does not know reports "Unknown task type".
func TestExecuteTaskDispatchesABranchNode(t *testing.T) {
	app := newTestApp(t)
	captureLogs(t)

	next, err := executeTask(runningContext(t), Task{
		ID:   "branch-1",
		Type: "BranchNode",
		Data: map[string]interface{}{"variable": "attempts", "operator": opGreaterThan, "value": 2.0},
	}, newRunState(), app)

	if err != nil {
		t.Fatalf("executeTask returned %v, want a BranchNode to be dispatched", err)
	}
	if next != branchFalse {
		t.Errorf("next = %q, want %q", next, branchFalse)
	}
}

// =============================================== the Wait For Color timeout ===============================================

// timeoutTestConfig is the parsed payload the two colorTimedOut tests report
// on. The values only have to appear in the message.
func timeoutTestConfig() colorWaitConfig {
	return colorWaitConfig{
		target:    rgbColor{r: 0, g: 255, b: 0},
		x:         100,
		y:         200,
		tolerance: 10,
		timeout:   5 * time.Second,
	}
}

// With an edge drawn from the timeout handle, a timeout is a branch the macro
// asked for: it takes that output and reports success. Emphatically no
// task-error - the colour not appearing is the answer the macro wanted, not a
// failure of the node.
func TestColorTimedOutTakesTheTimeoutOutputWhenTheMacroWiredOne(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)

	task := Task{
		ID:           "wait-1",
		Type:         "ColorPickerNode",
		WiredOutputs: []string{"right", colorTimeoutHandle},
	}

	next, err := colorTimedOut(task, timeoutTestConfig(), app)

	if err != nil {
		t.Errorf("a handled timeout returned %v, want no error", err)
	}
	if next != colorTimeoutHandle {
		t.Errorf("next = %q, want %q", next, colorTimeoutHandle)
	}
	if !logs.contains(emittedEvent("task-success")) {
		t.Errorf("a handled timeout did not report the node as having run:\n%s", logs)
	}
	if logs.contains(emittedEvent("task-error")) {
		t.Errorf("a handled timeout reported the node as failed:\n%s", logs)
	}
}

// With nothing wired to it, the behaviour is what it has always been, down to
// the wording. An unhandled timeout has to stay as loud as it is, or a macro
// whose colour never turns up does nothing and says it worked.
func TestColorTimedOutStaysAnErrorWhenNothingIsWiredToTheTimeout(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)

	task := Task{
		ID:           "wait-1",
		Type:         "ColorPickerNode",
		WiredOutputs: []string{"right"},
	}

	next, err := colorTimedOut(task, timeoutTestConfig(), app)

	if err == nil {
		t.Fatal("an unhandled timeout reported no error")
	}
	want := "Timed out after 5s waiting for pixel (100,200) to match #00ff00 (tolerance 10)"
	if err.Error() != want {
		t.Errorf("error = %q, want the wording this node has always used: %q", err, want)
	}
	// endPathHandle is what "no output taken" actually spells. This asserted ""
	// and read as though it agreed - but "" is "the only output" and takes every
	// outgoing edge, so the one edge this node has, the match edge, was followed:
	// a "wait for green, then click Buy" clicked Buy after never seeing green.
	if next != endPathHandle {
		t.Errorf("next = %q, want no output taken (%q)", next, endPathHandle)
	}
	if !logs.contains(emittedEvent("task-error")) {
		t.Errorf("no task-error event was emitted:\n%s", logs)
	}
	if logs.contains(emittedEvent("task-success")) {
		t.Errorf("an unhandled timeout also reported success:\n%s", logs)
	}
	if !logs.contains("ColorPickerNode error: " + want) {
		t.Errorf("the log did not carry the message it always has:\n%s", logs)
	}
}

// The match output, which is conditional on the graph for a reason worth a test
// of its own: "" takes *every* outgoing edge, so a node with a timeout edge
// drawn would run its fallback after every success.
func TestColorMatchOutput(t *testing.T) {
	cases := []struct {
		name  string
		wired []string
		want  string
	}{
		{
			// Every macro saved before this node had two outputs. "" takes the
			// one edge whatever its handle is called.
			name:  "nothing wired to the timeout",
			wired: []string{"right"},
			want:  "",
		},
		{
			name:  "no outgoing edges at all",
			wired: nil,
			want:  "",
		},
		{
			// Both outputs drawn: the match names its handle, so a success does
			// not also take the fallback.
			name:  "both outputs wired",
			wired: []string{"right", colorTimeoutHandle},
			want:  "right",
		},
		{
			// Only the timeout drawn: nothing is wired to the handle this
			// returns, so a match ends that path - which is what a macro that
			// only said what to do on a timeout asked for.
			name:  "only the timeout wired",
			wired: []string{colorTimeoutHandle},
			want:  colorMatchHandle,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := colorMatchOutput(Task{ID: "wait-1", WiredOutputs: tc.wired})
			if got != tc.want {
				t.Errorf("colorMatchOutput = %q, want %q", got, tc.want)
			}
		})
	}
}

// branchFlow is start -> branch, with both of the branch's outputs wired to a
// node of their own and a fifth node after the true side, so a token that took
// both edges is visible as two subtrees rather than one extra visit.
func branchFlow(data map[string]interface{}) FlowData {
	node := func(id, nodeType string, payload map[string]interface{}) Node {
		return Node{ID: id, Type: nodeType, Data: payload, Position: map[string]float64{"x": 0, "y": 0}}
	}
	edge := func(id, source, target, handle string) Edge {
		return Edge{ID: id, Source: source, Target: target, SourceHandle: handle}
	}

	return FlowData{
		Nodes: []Node{
			node("start", "StartNode", map[string]interface{}{}),
			node("branch", "BranchNode", data),
			node("yes", "StartNode", map[string]interface{}{}),
			node("no", "StartNode", map[string]interface{}{}),
		},
		Edges: []Edge{
			edge("e1", "start", "branch", "right"),
			edge("e2", "branch", "yes", branchTrue),
			edge("e3", "branch", "no", branchFalse),
		},
	}
}

// A Branch whose condition cannot be evaluated must run *neither* side.
//
// It used to run both, which is the opposite of what its own comment promised:
// it returned "" for "I could not decide", and "" means "the only output" and
// takes every outgoing edge. A macro wired true -> "click Buy", false -> "click
// Cancel" clicked both. An operator the node's own UI cannot produce is exactly
// how a hand-edited or newer-than-this-build file arrives.
func TestABranchThatCannotDecideRunsNeitherSide(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)

	state := newRunState()
	state.set("colour", "00ff00")

	flow := branchFlow(map[string]interface{}{
		"variable": "colour",
		"operator": "gt", // not one of the six; the UI cannot produce it
		"value":    "0",
	})
	r := newRun(flow, "start", func(ctx context.Context, task Task, _ *runState) (string, error) {
		return executeTask(ctx, task, state, app)
	})

	got := walkAll(context.Background(), r)
	want := []string{"start", "branch"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("visits = %v, want the token to stop at the branch: %v", got, want)
	}
	if !logs.contains(emittedEvent("task-error")) {
		t.Errorf("an undecidable branch reported no task-error:\n%s", logs)
	}
	if r.outcome != runFinished {
		t.Errorf("outcome = %v, want the run to have ended by running out of edges", r.outcome)
	}
}

// The same node with a condition it *can* evaluate still takes exactly one side,
// so the fix above did not turn a working branch into a dead end.
func TestABranchThatCanDecideStillTakesOneSide(t *testing.T) {
	app := newTestApp(t)

	state := newRunState()
	state.set("colour", "00ff00")

	flow := branchFlow(map[string]interface{}{
		"variable": "colour",
		"operator": opEquals,
		"value":    "00ff00",
	})
	r := newRun(flow, "start", func(ctx context.Context, task Task, _ *runState) (string, error) {
		return executeTask(ctx, task, state, app)
	})

	got := walkAll(context.Background(), r)
	want := []string{"start", "branch", "yes"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("visits = %v, want only the true side: %v", got, want)
	}
}

// endPathHandle ends the path wherever it comes from, and ends only that path.
//
// The generic half of the two fixes above: a handler that names no output must
// leave the token nowhere to go, while anything already pending still runs. That
// second half is what keeps this "the token stopped here" rather than "the run
// was abandoned" - a Sequence whose first branch gives up must not take its
// second branch down with it.
func TestEndPathHandleStopsOnlyThePathThatNamedIt(t *testing.T) {
	node := func(id, nodeType string) Node {
		return Node{ID: id, Type: nodeType, Data: map[string]interface{}{}, Position: map[string]float64{"x": 0, "y": 0}}
	}
	edge := func(id, source, target, handle string) Edge {
		return Edge{ID: id, Source: source, Target: target, SourceHandle: handle}
	}

	flow := FlowData{
		Nodes: []Node{
			node("start", "StartNode"),
			node("seq", "SequenceNode"),
			node("gives-up", "StartNode"),
			node("after-it", "StartNode"),
			node("other-branch", "StartNode"),
		},
		Edges: []Edge{
			edge("e1", "start", "seq", "right"),
			edge("e2", "seq", "gives-up", "out-1"),
			edge("e3", "gives-up", "after-it", "right"),
			edge("e4", "seq", "other-branch", "out-2"),
		},
	}

	r := newRun(flow, "start", func(_ context.Context, task Task, _ *runState) (string, error) {
		if task.ID == "gives-up" {
			return endPathHandle, errors.New("this node could not choose an output")
		}
		return "", nil
	})

	got := walkAll(context.Background(), r)
	want := []string{"start", "seq", "gives-up", "other-branch"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("visits = %v, want the first branch stopped and the second still run: %v", got, want)
	}
}

// The Go walk and the TypeScript one in nodeLabels.ts order two edges sharing a
// handle the same way: by target id.
//
// validateOutputHandles refuses that graph before a run starts, so nothing a
// user can run depends on it - but the walk fixtures in testdata/walk are driven
// straight into run.step, bypassing that check, and both suites read them. Go
// used to keep whatever order the file listed, where the TypeScript sorted by
// target; a fixture with two edges on one handle would have made the two
// implementations disagree, which is the single thing those shared fixtures
// exist to catch.
func TestTwoEdgesOnOneHandleAreOrderedByTargetID(t *testing.T) {
	node := func(id string) Node {
		return Node{ID: id, Type: "StartNode", Data: map[string]interface{}{}, Position: map[string]float64{"x": 0, "y": 0}}
	}

	flow := FlowData{
		Nodes: []Node{node("start"), node("alpha"), node("zulu")},
		Edges: []Edge{
			// Listed with the later target first, so file order and target order
			// disagree and the assertion can tell them apart.
			{ID: "e1", Source: "start", Target: "zulu", SourceHandle: "right"},
			{ID: "e2", Source: "start", Target: "alpha", SourceHandle: "right"},
		},
	}

	got := walkAll(context.Background(), newRun(flow, "start", inertExec))
	want := []string{"start", "alpha", "zulu"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("visits = %v, want them ordered by target id like nodeLabels.ts does: %v", got, want)
	}
}
