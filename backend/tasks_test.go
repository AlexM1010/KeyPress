// tasks_test.go
//
// Tests for what a node handler answers with: the (next, err) pair every
// handler now returns, and the events it still emits alongside it.
//
// The constraints are execution_test.go's, and for the same reasons. Events are
// observed through the log, because a test App has no Wails application to emit
// to (captureLogs and emittedEvent, over there). And a handler that reaches
// robotgo moves the real mouse of whatever machine the suite is on, so the only
// handlers called here are called down paths that return before the first
// robotgo call - which is exactly where the (next, err) contract is worth
// pinning anyway, since those are the paths a saved macro actually goes wrong
// on.

package backend

import (
	"context"
	"strings"
	"testing"
)

// runningContext is a context in the state a handler normally sees: live, and
// belonging to a run nobody has stopped.
func runningContext(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return ctx
}

// cancelledContext is a context in the state a handler sees after Stop.
//
// Being able to build one and hand it to a handler is the point of the context
// being a parameter: were handlers to read app.runner.Context() for themselves,
// no test could put one into this state without standing up and stopping a whole
// run, and none of the cancellation paths below had a test at all.
func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// =============================================== executeTask ===============================================

func TestExecuteTaskOnAStartNodeTakesTheOnlyOutputAndSucceeds(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)

	next, err := executeTask(runningContext(t), Task{ID: "start-1", Type: "StartNode"}, newRunState(), app)

	if err != nil {
		t.Errorf("executeTask returned %v, want no error", err)
	}
	// "" is "leave by the only output". Every handler says this today; a node
	// that picks between outputs is a later phase.
	if next != "" {
		t.Errorf("next = %q, want %q", next, "")
	}
	if !logs.contains(emittedEvent("task-success")) {
		t.Errorf("no task-success event was emitted:\n%s", logs)
	}
	if logs.contains(emittedEvent("task-error")) {
		t.Errorf("a Start node reported an error:\n%s", logs)
	}
}

// A Sequence node does nothing and says so with "", the only output. That is
// what makes the walk take every one of its handles, in handle order, and it is
// the whole of how a macro fans out - so a Sequence node that ever returned
// anything else would silently take one branch instead of all of them.
func TestExecuteTaskOnASequenceNodeTakesTheOnlyOutput(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)

	next, err := executeTask(runningContext(t), Task{ID: "seq-1", Type: "SequenceNode"}, newRunState(), app)

	if err != nil {
		t.Errorf("executeTask returned %v, want no error", err)
	}
	if next != "" {
		t.Errorf("next = %q, want %q - a named output would take one branch instead of all of them", next, "")
	}
	if !logs.contains(emittedEvent("task-success")) {
		t.Errorf("no task-success event was emitted:\n%s", logs)
	}
}

// The one failure executeTask itself is responsible for. It has to keep telling
// the frontend in the words it always used, and now also hand the same thing
// back to its caller.
func TestExecuteTaskOnAnUnknownTypeReturnsTheErrorItEmitted(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)

	next, err := executeTask(runningContext(t), Task{ID: "mystery-1", Type: "NotANodeType"}, newRunState(), app)

	if err == nil {
		t.Fatal("executeTask accepted an unknown task type")
	}
	if want := "Unknown task type: NotANodeType"; err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
	}
	if next != "" {
		t.Errorf("next = %q, want %q", next, "")
	}
	if !logs.contains(emittedEvent("task-error")) {
		t.Errorf("no task-error event was emitted:\n%s", logs)
	}
	if !logs.contains("Unknown task type: NotANodeType for task mystery-1") {
		t.Errorf("the log did not name the unknown type and the task:\n%s", logs)
	}
	if logs.contains(emittedEvent("task-success")) {
		t.Errorf("an unknown task type also reported success:\n%s", logs)
	}
}

// A panicking node must report through both channels, and the returned error is
// the half that had nowhere to come from before: the recover happens in a
// deferred function, so setting the named return is the only way it can say
// anything at all. Without it a node that panicked would hand back whatever the
// half-finished handler left - "" and nil, which reads as an ordinary success.
//
// Inducing the panic takes some doing, and how is worth stating. No node type
// panics on purpose and none should be added; nothing in a payload can make a
// handler panic either, since the last bare type assertions became reported
// failures (see readMouseMoveSettings). What is left is the one argument a real
// caller cannot get wrong: a nil context. App.executeNode always has its
// generation's context in hand, so nothing in the app can pass this - but a
// handler that consults it panics on the spot, in production code, without a
// mouse anywhere near it.
func TestExecuteTaskReportsAPanicAsAnErrorAsWellAsAnEvent(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)

	// Declared rather than written inline as `nil`, which staticcheck refuses
	// (SA1012) - and rightly, everywhere that is not this test.
	var noContext context.Context

	next, err := executeTask(noContext, Task{
		ID:   "panicking-1",
		Type: "DelayNode",
		// A wait long enough that the handler reaches the context rather than
		// returning from waitInterruptible's non-positive shortcut. Nothing
		// waits: the panic happens on the first look at the context.
		Data: map[string]interface{}{"delayType": "Fixed", "time": float64(60000)},
	}, newRunState(), app)

	if err == nil {
		t.Fatal("a panicking task reported no error")
	}
	if !strings.HasPrefix(err.Error(), "panic: ") {
		t.Errorf("error = %q, want it to say it was a panic", err)
	}
	// endPathHandle, not "". A panicking handler chose no output, and "" is not
	// "no output" - it is "the only output", which takes every outgoing edge, so
	// a panic inside a Branch or a Loop Start used to take all of them at once.
	// See endPathHandle.
	if next != endPathHandle {
		t.Errorf("next = %q, want %q", next, endPathHandle)
	}
	if !logs.contains(emittedEvent("task-error")) {
		t.Errorf("no task-error event was emitted:\n%s", logs)
	}
	if !logs.contains("Recovered from panic in task panicking-1") {
		t.Errorf("the recovery was not logged against the task:\n%s", logs)
	}
	if !logs.contains("Completed execution of task ID: panicking-1") {
		t.Errorf("the completion line the lifecycle tests watch for was not written:\n%s", logs)
	}
}

// executeTask hands its context to the handler rather than the handler fetching
// one, so a run that is already over is one a handler can see. Through
// executeTask rather than straight to the handler, because the threading is the
// part being tested.
func TestExecuteTaskPassesItsContextToTheHandler(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)

	next, err := executeTask(cancelledContext(), Task{
		ID:   "delay-1",
		Type: "DelayNode",
		// A minute, so a handler that did not see the cancellation would hang
		// the test rather than pass it.
		Data: map[string]interface{}{"delayType": "Fixed", "time": float64(60000)},
	}, newRunState(), app)

	// Cancellation is not an error. The caller cancelled the context and so
	// already knows; a sentinel would be a second mechanism for the same fact,
	// and one every caller would have to remember to tell apart from a failure.
	if err != nil {
		t.Errorf("a cancelled run reported %v, want no error", err)
	}
	if next != "" {
		t.Errorf("next = %q, want %q", next, "")
	}
	if !logs.contains("Delay canceled for task delay-1") {
		t.Errorf("the delay did not observe the cancelled context:\n%s", logs)
	}
	if logs.contains(emittedEvent("task-success")) {
		t.Errorf("a delay cut short by a stop reported success:\n%s", logs)
	}
	if logs.contains(emittedEvent("task-error")) {
		t.Errorf("a stopped run was reported as a failed node:\n%s", logs)
	}
}

// =============================================== the handlers ===============================================

// The click handler's own top-of-handler check, which is what stands between a
// stopped run and a success event on the configurations that reach neither
// clickLoop's per-click check nor the scroll loop's per-direction one - a node
// with nothing to scroll, which is most of them.
func TestExecuteMouseClickTaskStopsOnAContextThatIsAlreadyCancelled(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)

	next, err := executeMouseClickTask(cancelledContext(), Task{
		ID:   "click-1",
		Type: "MouseClickNode",
		Data: map[string]interface{}{
			"buttonType":     "left",
			"numberOfClicks": float64(3),
			"clickDelay":     float64(100),
		},
	}, newRunState(), app)

	if err != nil {
		t.Errorf("a cancelled run reported %v, want no error", err)
	}
	if next != "" {
		t.Errorf("next = %q, want %q", next, "")
	}
	if !logs.contains("Click canceled before clicking for task click-1") {
		t.Errorf("the click did not stop before touching the mouse:\n%s", logs)
	}
	if logs.contains(emittedEvent("task-success")) {
		t.Errorf("a click node on a stopped run reported success:\n%s", logs)
	}
}

// Only the position configurations are damaged, and deliberately: it is the one
// mouse-move failure that is decided before robotgo.Location is called, so this
// runs on a machine with a mouse without touching it. The settings the handler
// reads further down are checked directly in actions_mouse_test.go, which is why
// readMouseMoveSettings exists as a function of its own.
func TestExecuteMouseMoveTaskReturnsTheErrorItEmitted(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)

	next, err := executeMouseMoveTask(runningContext(t), Task{
		ID:   "move-1",
		Type: "MouseMoveNode",
		Data: map[string]interface{}{"startPosition": "somewhere"},
	}, newRunState(), app)

	if err == nil {
		t.Fatal("executeMouseMoveTask accepted a node with no usable positions")
	}
	if want := "Invalid or missing position configurations"; err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
	}
	if next != "" {
		t.Errorf("next = %q, want %q", next, "")
	}
	if !logs.contains(emittedEvent("task-error")) {
		t.Errorf("no task-error event was emitted:\n%s", logs)
	}
	if !logs.contains("MoveMouse error: Invalid or missing position configurations for task move-1") {
		t.Errorf("the log did not name the reason and the task:\n%s", logs)
	}
}

// The colour handler validates its whole payload before it samples a pixel, so
// its parse failure is reachable without a screen.
func TestExecuteColorPickerTaskReturnsTheErrorItEmitted(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)

	next, err := executeColorPickerTask(runningContext(t), Task{
		ID:   "color-1",
		Type: "ColorPickerNode",
		Data: map[string]interface{}{"color": "not a colour", "x": float64(10), "y": float64(20)},
	}, newRunState(), app)

	if err == nil {
		t.Fatal("executeColorPickerTask accepted a colour it cannot parse")
	}
	if !strings.Contains(err.Error(), "invalid hex color") {
		t.Errorf("error = %q, want it to name the unreadable colour", err)
	}
	if next != "" {
		t.Errorf("next = %q, want %q", next, "")
	}
	if !logs.contains(emittedEvent("task-error")) {
		t.Errorf("no task-error event was emitted:\n%s", logs)
	}
	if logs.contains(emittedEvent("task-success")) {
		t.Errorf("a node that could not be read reported success:\n%s", logs)
	}
}
