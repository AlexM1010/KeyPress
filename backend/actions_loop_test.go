// actions_loop_test.go
//
// The loop pair: what a payload means, how many times the body runs, what the
// Loop End does (nothing), and the minimum per-iteration yield.
//
// Both halves are safe to really run in a test - one reads a payload, counts
// and waits, the other logs and returns - so unlike every other node type these
// tests drive the actual handlers. The graph around them is loopFlow in
// execution_test.go: Start, the Loop Start, one node of body, the Loop End, and
// one node on `done`. The back edge in it is the editor's, not a user's, and
// neither half carries a partner id, because the backend would ignore one.
//
// The yield is on a testing/synctest fake clock, so "three iterations cost two
// yields" is a fact about the program rather than twenty real milliseconds and
// a tolerance. See newBubbleTestApp for the bubble's constraints.

package backend

import (
	"context"
	"math"
	"reflect"
	"testing"
	"testing/synctest"
	"time"
)

// walkLoopFlow drives loopFlow with the Loop Start node's payload as given, and
// reports the nodes the token ran, in order and with repeats.
//
// Both of the pair's real handlers run; every other node is inert except that
// onBody, when given, is called on each arrival at the body node. That is how a
// test arranges for the run state a condition reads to change while the loop is
// going, which is the only way "until the count reaches three" can be written
// without a real Mouse Move node to store it.
func walkLoopFlow(t *testing.T, app *App, data map[string]interface{}, onBody func(state *runState)) []string {
	t.Helper()

	r := newRun(loopFlow(data), "start", func(ctx context.Context, task Task, state *runState) (string, error) {
		if task.Type == loopStartNodeType {
			return executeLoopStartTask(ctx, task, state, app)
		}
		if task.Type == loopEndNodeType {
			return executeLoopEndTask(ctx, task, state, app)
		}
		if task.ID == "body" && onBody != nil {
			onBody(state)
		}
		return "", nil
	})
	return walkAll(context.Background(), r)
}

// countIterations records how many times the body has run, under the name a
// condition can then compare against.
func countIterations(name string) func(*runState) {
	return func(state *runState) {
		count, _ := state.value(name)
		number, _ := asNumber(count)
		state.set(name, number+1)
	}
}

// =============================================== the payload ===============================================

// The count is optional, and "no count" has to be told from a count of zero:
// one loops forever and the other runs the body no times at all. Absent, null
// and an emptied text field are the three ways a saved node says it has none.
func TestParseLoopCountTellsNoCountFromZero(t *testing.T) {
	none := []interface{}{nil, "", "   "}
	for _, raw := range none {
		count, has, err := parseLoopCount(raw)
		if err != nil {
			t.Errorf("parseLoopCount(%#v): %v", raw, err)
		}
		if has {
			t.Errorf("parseLoopCount(%#v) = %v, true; want no count at all", raw, count)
		}
	}

	for _, raw := range []interface{}{float64(0), "0"} {
		count, has, err := parseLoopCount(raw)
		if err != nil || !has || count != 0 {
			t.Errorf("parseLoopCount(%#v) = %v, %v, %v; want a count of exactly zero", raw, count, has, err)
		}
	}

	for _, raw := range []interface{}{float64(5), "5", " 5 "} {
		count, has, err := parseLoopCount(raw)
		if err != nil || !has || count != 5 {
			t.Errorf("parseLoopCount(%#v) = %v, %v, %v; want a count of five", raw, count, has, err)
		}
	}
}

// A count a loop cannot have is a malformed payload rather than a number to
// repair. NaN is the one worth its own case: it compares false against
// everything, so a loop that let it through would claim a count and then run
// like one that has none.
func TestParseLoopCountRefusesWhatACountCannotBe(t *testing.T) {
	refused := []interface{}{
		float64(-1),
		"-1",
		math.NaN(),
		math.Inf(1),
		"NaN",
		"Inf",
		"three",
		true,
		[]interface{}{float64(3)},
	}
	for _, raw := range refused {
		if _, _, err := parseLoopCount(raw); err == nil {
			t.Errorf("parseLoopCount(%#v) accepted a count no loop can have", raw)
		}
	}
}

// The condition is optional too, and an empty operator is how the node's own
// select says "none" - as opposed to an operator nobody recognises, which is a
// file that could not have been drawn.
func TestParseLoopConfigTreatsAnEmptyOperatorAsNoCondition(t *testing.T) {
	for _, raw := range []interface{}{nil, "", "  "} {
		cfg, err := parseLoopConfig(map[string]interface{}{"operator": raw})
		if err != nil {
			t.Fatalf("parseLoopConfig(operator=%#v): %v", raw, err)
		}
		if cfg.hasCondition {
			t.Errorf("parseLoopConfig(operator=%#v) found a condition", raw)
		}
	}

	cfg, err := parseLoopConfig(map[string]interface{}{
		"variable": " tries ", "operator": opGreaterThan, "value": float64(2),
	})
	if err != nil {
		t.Fatalf("parseLoopConfig: %v", err)
	}
	if !cfg.hasCondition || cfg.operator != opGreaterThan || cfg.value != float64(2) {
		t.Errorf("parseLoopConfig = %+v, want the condition it was given", cfg)
	}
	// Not trimmed here: conditionHolds trims the variable, so that a name typed
	// with a trailing space finds what it stored, and there is one place doing
	// it rather than two that could disagree.
	if cfg.variable != " tries " {
		t.Errorf("variable = %q, want the payload's own spelling", cfg.variable)
	}
}

// =============================================== the four combinations ===============================================

// Count only: the body runs exactly that many times, and the token leaves by
// `done`.
func TestALoopWithOnlyACountRunsItsBodyThatManyTimes(t *testing.T) {
	app := newTestApp(t)

	got := walkLoopFlow(t, app, map[string]interface{}{"count": float64(3)}, nil)
	want := []string{
		"start",
		"loop", "body", "end",
		"loop", "body", "end",
		"loop", "body", "end",
		"loop", "after",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("visits = %v, want %v", got, want)
	}
}

// A count of zero runs the body no times at all and leaves by `done`. It is a
// real count, not a way of spelling "no count" - see parseLoopCount.
func TestALoopWithACountOfZeroRunsItsBodyNoTimes(t *testing.T) {
	app := newTestApp(t)

	got := walkLoopFlow(t, app, map[string]interface{}{"count": float64(0)}, nil)
	want := []string{"start", "loop", "after"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("visits = %v, want the loop to have been stepped straight over: %v", got, want)
	}
}

// Condition only: the body runs until the condition holds. It is checked
// *before* each iteration, so the iteration that makes it true is the last one
// to run.
func TestALoopWithOnlyAConditionRunsUntilItHolds(t *testing.T) {
	app := newTestApp(t)

	got := walkLoopFlow(t, app, map[string]interface{}{
		"variable": "tries", "operator": opEquals, "value": float64(2),
	}, countIterations("tries"))

	want := []string{"start", "loop", "body", "end", "loop", "body", "end", "loop", "after"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("visits = %v, want %v", got, want)
	}
}

// The other half of "checked before each iteration": a condition that already
// holds when the token first arrives runs the body zero times. A while loop,
// not a do-while - which is what makes "retry until the pixel is green" do
// nothing when the pixel is already green.
func TestALoopWhoseConditionAlreadyHoldsRunsItsBodyNoTimes(t *testing.T) {
	app := newTestApp(t)

	r := newRun(loopFlow(map[string]interface{}{
		"variable": "colour", "operator": opEquals, "value": "00ff00",
	}), "start", func(ctx context.Context, task Task, state *runState) (string, error) {
		if task.Type == loopStartNodeType {
			return executeLoopStartTask(ctx, task, state, app)
		}
		return "", nil
	})
	r.state.set("colour", "00ff00")

	got := walkAll(context.Background(), r)
	want := []string{"start", "loop", "after"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("visits = %v, want the body never to have run: %v", got, want)
	}
}

// Both, in the order where the count is what stops it: "retry up to five times
// until it works", where it never works.
func TestALoopWithBothStopsOnTheCountWhenThatComesFirst(t *testing.T) {
	app := newTestApp(t)

	got := walkLoopFlow(t, app, map[string]interface{}{
		"count": float64(2), "variable": "tries", "operator": opEquals, "value": float64(9),
	}, countIterations("tries"))

	want := []string{"start", "loop", "body", "end", "loop", "body", "end", "loop", "after"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("visits = %v, want the count to have ended it: %v", got, want)
	}
}

// Both, the other way round: the same node, and this time it works on the
// second try, so the count of five is never reached.
func TestALoopWithBothStopsOnTheConditionWhenThatComesFirst(t *testing.T) {
	app := newTestApp(t)

	got := walkLoopFlow(t, app, map[string]interface{}{
		"count": float64(5), "variable": "tries", "operator": opEquals, "value": float64(2),
	}, countIterations("tries"))

	want := []string{"start", "loop", "body", "end", "loop", "body", "end", "loop", "after"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("visits = %v, want the condition to have ended it: %v", got, want)
	}
}

// Neither: forever. The design once had the editor refuse this; it is the
// canonical macro instead, and what ends it is the user pressing the hotkey
// again - so this test ends it the way the toggle does, by cancelling.
func TestALoopWithNeitherRunsForever(t *testing.T) {
	app := newTestApp(t)

	r := newRun(loopFlow(map[string]interface{}{}), "start", func(ctx context.Context, task Task, state *runState) (string, error) {
		if task.Type == loopStartNodeType {
			return executeLoopStartTask(ctx, task, state, app)
		}
		return "", nil
	})

	// Far more turns than any count in this file, so a loop that quietly
	// acquired a limit of its own would be caught here rather than looking like
	// a slow test.
	bodies := 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for {
		id, ok := r.step(ctx)
		if !ok {
			t.Fatalf("the loop ended by itself after %d turn(s), with outcome %v", bodies, r.outcome)
		}
		if id != "body" {
			continue
		}
		if bodies++; bodies == 20 {
			break
		}
	}

	if visited := len(r.frames); visited != 1 {
		t.Errorf("the run holds %d loop frame(s) mid-loop, want the one it is inside", visited)
	}

	// And it stops promptly when the toggle cancels it.
	cancel()
	if _, ok := r.step(ctx); ok {
		t.Error("the loop carried on after cancellation")
	}
	if r.outcome != runCancelled {
		t.Errorf("outcome = %v, want the run cancelled", r.outcome)
	}
}

// =============================================== what a broken loop does ===============================================

// A payload this node cannot mean leaves by `done`, never by "". "" is "the
// only output", which takes every outgoing edge - so a Loop Start returning it
// would run the body and the exit at once and then come round the back-edge
// again. A loop nobody could have drawn must not be the one that runs forever.
func TestABrokenLoopLeavesByDoneRatherThanEverything(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)

	broken := []map[string]interface{}{
		{"count": "three"},
		{"count": float64(-1)},
		{"operator": "isNearlyEqualTo", "variable": "x", "value": float64(1)},
	}
	for _, data := range broken {
		next, err := executeLoopStartTask(context.Background(), Task{
			ID: "loop", Type: loopStartNodeType, Data: data, Loop: &loopFrame{nodeID: "loop"},
		}, newRunState(), app)

		if err == nil {
			t.Errorf("payload %#v was accepted", data)
		}
		if next != loopDoneHandle {
			t.Errorf("payload %#v left by %q, want %q", data, next, loopDoneHandle)
		}
	}
	if got := logs.count(emittedEvent("task-error")); got != len(broken) {
		t.Errorf("%d task-error event(s) for %d broken payloads:\n%s", got, len(broken), logs)
	}
}

// The frame is the walk's, handed over on the task. A Loop Start reached
// without one was not reached by a walk at all, and a loop with nowhere to count would
// never reach its count - so it is refused rather than run.
func TestALoopWithNoFrameIsRefused(t *testing.T) {
	app := newTestApp(t)

	next, err := executeLoopStartTask(context.Background(), Task{
		ID: "loop", Type: loopStartNodeType, Data: map[string]interface{}{"count": float64(3)},
	}, newRunState(), app)

	if err == nil {
		t.Error("a Loop node with no iteration frame ran")
	}
	if next != loopDoneHandle {
		t.Errorf("next = %q, want %q", next, loopDoneHandle)
	}
}

// =============================================== the Loop End, and half a pair ===============================================

// The Loop End does nothing and leaves by "", which is what makes the pair cost
// the walk nothing: "" is "the only output", and the only output is the edge the
// editor drew from `back` to the partner Loop Start.
//
// It is also where the pairing is *not*. The payload here carries the loopId a
// frontend is expected to keep so it can move and delete the two together, and
// the handler neither reads it nor cares that its partner does not exist.
func TestALoopEndRunsNothingAndLeavesByItsOnlyOutput(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)

	state := newRunState()
	next, err := executeLoopEndTask(context.Background(), Task{
		ID:   "end",
		Type: loopEndNodeType,
		Data: map[string]interface{}{"loopId": "a-partner-the-backend-never-looks-up"},
	}, state, app)

	if err != nil {
		t.Errorf("a Loop End reported an error: %v", err)
	}
	if next != "" {
		t.Errorf("next = %q, want \"\" - the only output, which is the back edge", next)
	}
	if len(state.values) != 0 {
		t.Errorf("a Loop End wrote %d value(s) into the run state, want none", len(state.values))
	}
	if !logs.contains(emittedEvent("task-success")) {
		t.Errorf("a Loop End reported nothing to the status panel:\n%s", logs)
	}
}

// Half a pair is a macro the editor refuses to leave behind and a file can hold
// anyway. Neither half of it is an error: a Loop End with no back edge ends that
// path, exactly like any other unwired output.
func TestALoopEndWithNoBackEdgeEndsThePath(t *testing.T) {
	app := newTestApp(t)

	flow := withoutEdge(loopFlow(map[string]interface{}{"count": float64(3)}), "e4")
	r := newRun(flow, "start", func(ctx context.Context, task Task, state *runState) (string, error) {
		return executeTask(ctx, task, state, app)
	})

	got := walkAll(context.Background(), r)
	want := []string{"start", "loop", "body", "end"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("visits = %v, want the token to have stopped where the back edge was: %v", got, want)
	}
	if r.outcome != runFinished {
		t.Errorf("outcome = %v, want a run that simply ran out of edges", r.outcome)
	}
}

// The other half: a Loop Start whose body is unwired leaves by `done`, so the
// loop does nothing and the macro carries on. Returning `body` would end the run
// in the middle of the loop instead, with everything after `done` never run.
func TestALoopStartWithAnUnwiredBodyLeavesByDone(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)

	flow := withoutEdge(loopFlow(map[string]interface{}{"count": float64(3)}), "e2")
	r := newRun(flow, "start", func(ctx context.Context, task Task, state *runState) (string, error) {
		return executeTask(ctx, task, state, app)
	})

	got := walkAll(context.Background(), r)
	want := []string{"start", "loop", "after"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("visits = %v, want the loop stepped over and the macro carrying on: %v", got, want)
	}
	if len(r.frames) != 0 {
		t.Errorf("the run holds %d loop frame(s), want the empty loop to have been left", len(r.frames))
	}
	if logs.contains(emittedEvent("task-error")) {
		t.Errorf("an unwired body was reported as an error:\n%s", logs)
	}
}

// =============================================== the frame stack ===============================================

// Arriving at a loop already on the stack is the token coming round the
// back-edge, and every frame above it belongs to a loop that has now been left
// - whatever route it left by. Unwinding there is what makes a Branch wired out
// of an inner loop's body leave that loop rather than leak its count into the
// next entry.
func TestEnteringALoopUnwindsTheFramesAboveIt(t *testing.T) {
	r := newRun(FlowData{}, "start", inertExec)

	outer := r.enterLoop("outer")
	inner := r.enterLoop("inner")
	outer.iterations, inner.iterations = 1, 1

	if again := r.enterLoop("outer"); again != outer {
		t.Error("re-entering the outer loop did not find the frame it already had")
	}
	if len(r.frames) != 1 {
		t.Errorf("the stack holds %d frame(s), want the inner one to have been unwound", len(r.frames))
	}
	if fresh := r.enterLoop("inner"); fresh == inner || fresh.iterations != 0 {
		t.Error("the inner loop was re-entered on its old frame rather than a fresh one")
	}
}

// Leaving by `done` pops, so the next entry counts from zero. Popping a loop
// that is not on top does nothing, which is what keeps the invariant an
// assertion rather than an assumption.
func TestLeavingALoopPopsOnlyItsOwnFrame(t *testing.T) {
	r := newRun(FlowData{}, "start", inertExec)

	r.enterLoop("outer")
	r.enterLoop("inner")

	r.leaveLoop("outer")
	if len(r.frames) != 2 {
		t.Errorf("leaving a loop that is not on top popped something: %d frame(s) left", len(r.frames))
	}

	r.leaveLoop("inner")
	r.leaveLoop("outer")
	if len(r.frames) != 0 {
		t.Errorf("the stack holds %d frame(s), want both loops left", len(r.frames))
	}
}

// An iteration is a scope: the token coming round the back edge abandons
// whatever that turn left pending. The first arrival records the depth and
// truncates nothing, and a stack that has already been popped below the mark is
// left alone - control left the body sideways, so there is nothing of the
// iteration left to abandon.
func TestReEnteringALoopDropsWhatTheIterationLeftPending(t *testing.T) {
	r := newRun(FlowData{}, "start", inertExec)
	r.stack = []string{"outside-the-loop"}

	frame := r.enterLoop("loop")
	if frame.stackDepth != 1 {
		t.Errorf("stackDepth = %d, want the depth the loop was entered at", frame.stackDepth)
	}
	if !reflect.DeepEqual(r.stack, []string{"outside-the-loop"}) {
		t.Errorf("the first arrival truncated the stack to %v", r.stack)
	}

	// One iteration's worth of pending branches, and then the back edge.
	r.stack = append(r.stack, "pending-a", "pending-b")
	if again := r.enterLoop("loop"); again != frame {
		t.Error("coming round the back edge did not find the frame the loop already had")
	}
	if !reflect.DeepEqual(r.stack, []string{"outside-the-loop"}) {
		t.Errorf("stack = %v, want the iteration's pending nodes abandoned and nothing else", r.stack)
	}

	// And the same frame found with the stack already shorter than its mark.
	r.stack = nil
	if again := r.enterLoop("loop"); again != frame {
		t.Error("the frame was not found the third time")
	}
	if len(r.stack) != 0 {
		t.Errorf("stack = %v, want an arrival from outside the iteration to have changed nothing", r.stack)
	}
}

// loopWithASequenceInItsBody is the graph the leak was found in: a Sequence
// inside the body whose *first* handle carries on round the loop, so the second
// one is a branch that only runs once the loop is over.
//
// It is drawn this way round on purpose. Swap the two handles - Loop End on
// out-2 - and the same picture behaves perfectly, because the branch's whole
// subtree finishes before the token goes round again. Nothing on the canvas
// distinguishes the two, which is why this cannot be left to the user to notice.
func loopWithASequenceInItsBody(count float64) FlowData {
	node := func(id, nodeType string, payload map[string]interface{}) Node {
		return Node{ID: id, Type: nodeType, Data: payload, Position: map[string]float64{"x": 0, "y": 0}}
	}
	edge := func(id, source, target, handle string) Edge {
		return Edge{ID: id, Source: source, Target: target, SourceHandle: handle}
	}

	return FlowData{
		Nodes: []Node{
			node("start", "StartNode", map[string]interface{}{}),
			node("loop", loopStartNodeType, map[string]interface{}{"count": count}),
			node("seq", "SequenceNode", map[string]interface{}{}),
			node("end", loopEndNodeType, map[string]interface{}{}),
			// The branch that must not run while the loop is going. A StartNode
			// like every other inert node in the suite - if a regression does run
			// it, it must fail this test rather than click a real mouse.
			node("later", "StartNode", map[string]interface{}{}),
			node("after", "StartNode", map[string]interface{}{}),
		},
		Edges: []Edge{
			edge("e1", "start", "loop", "right"),
			edge("e2", "loop", "seq", loopBodyHandle),
			edge("e3", "seq", "end", "out-1"),
			edge("e4", "seq", "later", "out-2"),
			edge("e5", "end", "loop", loopBackHandle),
			edge("e6", "loop", "after", loopDoneHandle),
		},
	}
}

// The leak, end to end. Without the truncation this graph appends a pending
// `later` on every turn - a stack that grows at a hundred entries a second for
// as long as the loop runs - and then runs the whole backlog back to back when
// the loop leaves by `done`, which is not what anybody drew.
//
// Both halves are asserted: `later` does not run at all, and the stack is the
// same depth at every arrival at the Loop Start, the last one included, so
// leaving by `done` is unaffected by the truncation the iterations do.
//
// On the fake clock, because the loop's own per-iteration yield is real time
// otherwise. Nothing here depends on the duration - only on not spending it.
func TestALoopDoesNotAccumulateTheBranchesItsIterationsLeftBehind(t *testing.T) {
	verifyNoLeaks(t)

	synctest.Test(t, func(t *testing.T) {
		app := newBubbleTestApp(t)

		r := newRun(loopWithASequenceInItsBody(3), "start", func(ctx context.Context, task Task, state *runState) (string, error) {
			if task.Type == loopStartNodeType {
				return executeLoopStartTask(ctx, task, state, app)
			}
			return "", nil
		})

		visits := []string{}
		depths := []int{}
		for {
			id, ok := r.step(context.Background())
			if !ok {
				break
			}
			visits = append(visits, id)
			if id == "loop" {
				depths = append(depths, len(r.stack))
			}
		}

		want := []string{"start", "loop", "seq", "end", "loop", "seq", "end", "loop", "seq", "end", "loop", "after"}
		if !reflect.DeepEqual(visits, want) {
			t.Errorf("visits = %v, want the branch on out-2 left unrun: %v", visits, want)
		}
		for i, depth := range depths {
			if depth != depths[0] {
				t.Errorf("arrival %d at the loop left %d node(s) waiting, want the %d of the first: %v",
					i+1, depth, depths[0], depths)
				break
			}
		}
		if r.outcome != runFinished {
			t.Errorf("outcome = %v, want the run to have finished normally", r.outcome)
		}
		if len(r.frames) != 0 {
			t.Errorf("the run holds %d loop frame(s), want the loop to have been left", len(r.frames))
		}
	})
}

// =============================================== the minimum yield ===============================================

// A loop with no Delay node in it cannot run faster than one iteration per
// minLoopIterationYield. Three iterations means two gaps - the first has no
// previous iteration to be too close to - and on the fake clock that is exactly
// two, not "about two".
func TestALoopYieldsBetweenIterations(t *testing.T) {
	verifyNoLeaks(t)

	synctest.Test(t, func(t *testing.T) {
		app := newBubbleTestApp(t)

		started := time.Now()
		walkLoopFlow(t, app, map[string]interface{}{"count": float64(3)}, nil)

		if elapsed := time.Since(started); elapsed != 2*minLoopIterationYield {
			t.Errorf("three iterations took %v, want the two yields of %v they are worth", elapsed, minLoopIterationYield)
		}
	})
}

// The yield is a minimum, not a delay added to every turn: a body that already
// takes longer than it has spent it, so a loop with a Delay node in it waits
// for nothing here.
func TestALoopWhoseBodyIsSlowEnoughYieldsNothing(t *testing.T) {
	verifyNoLeaks(t)

	synctest.Test(t, func(t *testing.T) {
		app := newBubbleTestApp(t)
		body := 5 * minLoopIterationYield

		started := time.Now()
		walkLoopFlow(t, app, map[string]interface{}{"count": float64(3)}, func(*runState) {
			time.Sleep(body)
		})

		if elapsed := time.Since(started); elapsed != 3*body {
			t.Errorf("three iterations of a %v body took %v, want no yield on top of them", body, elapsed)
		}
	})
}

// The yield observes cancellation, because it is waitInterruptible and not a
// sleep of its own. Otherwise the stop the toggle sends would be up to a whole
// yield late on every loop in the macro.
func TestTheYieldIsCutShortByCancellation(t *testing.T) {
	verifyNoLeaks(t)

	synctest.Test(t, func(t *testing.T) {
		app := newBubbleTestApp(t)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		// A frame in mid-loop: one iteration already run, just now, so the whole
		// yield is still owing.
		frame := &loopFrame{nodeID: "loop", iterations: 1, lastIteration: time.Now()}
		started := time.Now()
		next, err := executeLoopStartTask(ctx, Task{
			ID: "loop", Type: loopStartNodeType, Data: map[string]interface{}{}, Loop: frame,
		}, newRunState(), app)

		if elapsed := time.Since(started); elapsed != 0 {
			t.Errorf("the cancelled yield still waited %v", elapsed)
		}
		if err != nil {
			t.Errorf("a cancelled yield reported an error: %v", err)
		}
		// Cancellation is the run ending rather than this node failing, so no
		// task-error and no task-success; the walk checks the context the moment
		// this returns, so the handle is never actually followed.
		if next != loopDoneHandle {
			t.Errorf("next = %q, want %q", next, loopDoneHandle)
		}
	})
}
