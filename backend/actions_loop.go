// actions_loop.go
//
// The loop: a matched pair of nodes, a count, an until-condition, both, or
// neither.
//
// A loop is not a construct the engine knows about - it is an edge pointing
// backwards, which the walk has followed since Phase 2. What this file adds is
// the decision at the top of that edge (Loop Start: run the body again, or
// leave by `done`) and the node the edge leaves from (Loop End, which does
// nothing at all).
//
// **The user does not draw the back edge; the editor does.** Creating a Loop
// Start creates its Loop End and an ordinary edge from that end's `back` handle
// to the start. That is the one thing this differs from Phase 4's first draft
// in, and the reason is the survey in docs/control-flow-next-steps.md: with a
// user-drawn back edge there is no syntactic enclosing loop, so "which loop
// does Break exit" has no answer and overlapping cycles have no innermost.
// Blueprints hides the back edge inside a Macro, Automa pairs two blocks by a
// shared id, LabVIEW/UiPath/OpenRPA/Scratch use containment, Blender chose a
// paired-node zone; n8n is the one mainstream tool that makes the user wire it,
// and Loop Over Items is reliably its most confusing node.
//
// **The pairing is an editor construct and the backend ignores it.** Nothing
// here reads a `loopId`, a partner id or anything else the frontend may keep in
// either node's `data` - see the comment on executeLoopEndTask. Frames are
// keyed by the Loop Start's node id, and the walk follows an ordinary edge on
// an ordinary handle, so the interpreter needed no change to gain a pair. The
// alternative - Automa's, where the second block names its partner and the walk
// jumps to a node rather than following a handle - would have widened the
// handler contract from `next string` to something that can also name a node,
// and that contract is shipped code.
//
// The counting lives in a frame on the run (loopFrame in interpreter.go),
// handed here on the task. It cannot live in this file or in the run state map:
// an inner loop's count has to die when the inner loop does, and only the walk
// knows when that is. What this file decides is what to do with the count once
// it has it.
//
// The condition is the Branch node's, unchanged - the same variable / operator
// / value payload and the same conditionHolds - because a macro should not have
// to learn two condition languages, and two evaluators would be two sets of
// answers to "what does greaterThan mean on a string".

package backend

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"time"
)

// The node `type`s a saved loop's two halves carry.
//
// loopStartNodeType is the one the walk matches on, to enter and leave that
// node's iteration frame - which is why it is a constant rather than a literal
// in two files. The walk does not match on loopEndNodeType at all: a Loop End
// is an ordinary node with an ordinary outgoing edge, and the walk needs to
// know nothing about it.
const (
	loopStartNodeType = "LoopStartNode"
	loopEndNodeType   = "LoopEndNode"
)

// The pair's output handles: two on the start, one on the end.
//
//   - body runs the loop's body, whose far end is the Loop End node. An
//     unwired `body` is a loop with nothing in it, and the node leaves by
//     `done` instead - the one place the loop consults Task.WiredOutputs, and
//     the reason is at the check itself in executeLoopStartTask.
//   - done leaves the loop. Like every other output, an unwired `done` simply
//     ends that path - which is how "loop ten times and stop" is drawn.
//   - back is the Loop End's only output, and the editor wires it to the
//     partner Loop Start. That edge is the loop. A Loop End whose `back` edge
//     has gone - a partner deleted, a hand-edited file - ends that path like
//     any other unwired output; the editor refuses to leave half a pair behind,
//     and this side does not depend on it having succeeded.
//
// Both `body` and `done` are on the Loop Start, which is where this differs
// from the survey's sketch in docs/control-flow-interpreter.md (it put `done`
// on the Loop End). Loop Start is where the condition is evaluated, so it is
// where the branch belongs - and it is what Blueprints does, whose For Loop
// carries Loop Body and Completed as pins on one node. Putting `done` on the
// Loop End would force a second editor-owned edge, from the start to the end,
// purely to route the exit past the body.
const (
	loopBodyHandle = "body"
	loopDoneHandle = "done"
	loopBackHandle = "back"
)

// minLoopIterationYield is the least time one turn of a loop may take.
//
// A loop with no Delay node in it is otherwise as fast as the machine: the
// walk is a for-loop over instantaneous handlers, so a two-node loop of clicks
// runs tens of thousands of iterations a second. Nothing downstream can absorb
// that. The OS input queue is drained by the target application at its own
// pace, so what the macro produces is not "clicking very fast" but a backlog
// the user watches drain for minutes after they stopped it, on a core pinned at
// 100%.
//
// Ten milliseconds is a hundred iterations a second. That is above any rate a
// person can distinguish from "instant" and above what a click-per-frame macro
// needs (a 60Hz application redraws every 16ms), and it is low enough to be
// invisible next to the delays real macros carry - a loop with a 100ms Delay
// node in it never reaches this at all, because the delay has already spent it.
//
// This carries some of the load the iteration budget used to, and it is worth
// being exact about how much. The budget bounded the number of node executions
// in a run and had to be removed for infinite loops to be legal; this bounds the
// *rate* instead, which is the half of the problem that actually hurt. A loop
// that is meant to run forever is no longer stopped after several hours by a
// limit it did nothing to deserve, and a loop that has run away can no longer
// flood the input queue faster than the user can reach the hotkey.
//
// **It only covers cycles that come through this node.** A cycle drawn with an
// ordinary back-pointing edge never reaches here at all, and neither does one
// through a Loop Start that leaves by `done` every time - the frame is popped on
// the way out, so the next arrival has no previous iteration to be too close to.
// Both used to spin at full speed. What covers them is the walk driver's
// wall-clock bound, maxWalkWithoutYield in execution.go, which is the general
// form of this rule; this one stays because it is the declared yield point for
// the construct the user actually draws, and because it fires at 100 iterations
// a second rather than waiting out a 500ms window.
const minLoopIterationYield = 10 * time.Millisecond

// loopConfig is the validated form of a Loop Start node's data payload.
//
// The count and the condition live on the start rather than on the pair,
// because the start is the node that decides. Anything else the editor keeps in
// that payload to remember its partner by is not read here and never will be -
// see the file comment.
//
// Both halves are optional and all four combinations are legal, including
// neither. A loop with no count and no condition runs forever, and that is the
// canonical macro this app exists for - "click until I say stop" - paired with
// the toggle-shaped RunMacro that says stop. An earlier draft of the design had
// the editor refuse it; the AutoHotkey threads that design cites are almost
// entirely about writing exactly this, so it is supported rather than refused.
type loopConfig struct {
	// count is how many times the body may run, and hasCount is whether the
	// payload asked for a count at all. The two are separate because zero is a
	// perfectly good count - it runs the body no times - and so cannot double as
	// "no count given".
	count    float64
	hasCount bool

	// variable, operator and value are the until-condition, in the Branch
	// node's shape. hasCondition is whether the payload asked for one, which is
	// an operator that is not empty: an empty operator is the "no condition"
	// the node's own select offers, where an unrecognised one is a malformed
	// payload.
	variable     string
	operator     string
	value        any
	hasCondition bool
}

// parseLoopCount reads the optional count.
//
// Absent, null, an empty string or a string of spaces all mean "no count" -
// the field is optional, and an emptied number input is how a user turns it
// off. A number, or a string that parses as one, is a count. Everything else is
// a payload this node cannot mean.
//
// A negative count is refused rather than treated as zero. There is no reading
// of "run this body -3 times", and a file saying so has been edited into a
// state the editor cannot produce; quietly rounding it up to zero would hide
// that.
func parseLoopCount(raw any) (float64, bool, error) {
	switch value := raw.(type) {
	case nil:
		return 0, false, nil
	case string:
		text := strings.TrimSpace(value)
		if text == "" {
			return 0, false, nil
		}
		count, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return 0, false, fmt.Errorf("invalid loop count %q: it must be a whole number of iterations", value)
		}
		return checkedLoopCount(count)
	case float64:
		return checkedLoopCount(value)
	default:
		return 0, false, fmt.Errorf("invalid loop count value: %v", raw)
	}
}

// checkedLoopCount refuses the numbers a count cannot be.
//
// NaN and the infinities cannot come out of encoding/json - JSON has no
// spelling for either - but a string field can carry "NaN" or "Inf", and both
// parse. NaN in particular compares false against everything, so it would slip
// past the negative check and then make `iterations >= count` false forever: a
// loop that says it has a count and runs like one that does not.
func checkedLoopCount(count float64) (float64, bool, error) {
	if math.IsNaN(count) || math.IsInf(count, 0) {
		return 0, false, fmt.Errorf("invalid loop count value: %v", count)
	}
	if count < 0 {
		return 0, false, fmt.Errorf("a loop count cannot be negative: %v", count)
	}
	return count, true, nil
}

// parseLoopConfig validates a task payload.
//
// It takes no App and no run state, so what a payload means can be settled in a
// test without a run; whether the condition *holds* is conditionHolds' job and
// needs the state.
func parseLoopConfig(data map[string]interface{}) (loopConfig, error) {
	count, hasCount, err := parseLoopCount(data["count"])
	if err != nil {
		return loopConfig{}, err
	}

	variable, _ := data["variable"].(string)
	operator, _ := data["operator"].(string)
	operator = strings.TrimSpace(operator)

	return loopConfig{
		count:    count,
		hasCount: hasCount,

		variable:     variable,
		operator:     operator,
		value:        data["value"],
		hasCondition: operator != "",
	}, nil
}

// executeLoopStartTask decides whether the token runs the loop's body again,
// and reports "body" or "done". It never returns "".
//
// "" would be wrong rather than merely vague: it means "the only output" and
// takes *every* outgoing edge, so a Loop Start returning it would run the body
// and the exit at once, and the run would go round the back edge again anyway.
// That is why even a malformed payload leaves by `done` - a loop nobody can
// have meant must not be the one that runs forever.
//
// The order of the two checks is the condition first, then the count. Both stop
// the loop and "whichever comes first" is what a loop carrying both means, so
// the order changes no macro that is written correctly; it decides only that a
// condition the node cannot evaluate is reported whether or not the count
// happened to have run out on the same iteration.
//
// **The condition is checked before each iteration**, so a condition that
// already holds when the token first arrives runs the body zero times. That is
// a while loop, not a do-while, and it is the reading that makes "retry until
// the pixel turns green" do nothing when the pixel is already green.
func executeLoopStartTask(ctx context.Context, task Task, state *runState, app *App) (string, error) {
	// Reported to the frontend as well as returned, and by the same value, like
	// every other handler here. The handle is the exit either way.
	fail := func(err error) (string, error) {
		log.Printf("%s error: %s for task %s", loopStartNodeType, err, task.ID)
		app.emitEvent("task-error", map[string]interface{}{
			"taskID": task.ID,
			"error":  err.Error(),
		})
		return loopDoneHandle, err
	}

	cfg, err := parseLoopConfig(task.Data)
	if err != nil {
		return fail(err)
	}

	// The walk fills this in on arrival (run.step). A nil frame means this
	// handler was called by something that is not a run, which no macro can
	// arrange - and a loop with nowhere to count would run forever without ever
	// reaching its count, so it is refused rather than guessed at.
	frame := task.Loop
	if frame == nil {
		return fail(errors.New("a Loop Start node has no iteration frame: it was run outside a walk"))
	}

	if cfg.hasCondition {
		holds, err := conditionHolds(state, cfg.variable, cfg.operator, cfg.value)
		if err != nil {
			return fail(err)
		}
		if holds {
			return loopLeaves(task, app, fmt.Sprintf("its condition %q %s %v holds", cfg.variable, cfg.operator, cfg.value), frame)
		}
	}

	if cfg.hasCount && float64(frame.iterations) >= cfg.count {
		return loopLeaves(task, app, fmt.Sprintf("it has run its body %v time(s)", cfg.count), frame)
	}

	// A loop with nothing in it leaves rather than entering a body that is not
	// there. The editor creates the Loop End with the start and wires the two
	// together, so this is a half-built or hand-edited macro - and the pair is
	// no reason to trust that it is intact.
	//
	// Returning `body` here would not hang: an output with no edge ends that
	// path, exactly as it does everywhere else. It would end the *run*, though,
	// silently and in the middle of the loop, with everything wired after `done`
	// never running. Leaving by `done` instead makes an empty loop a loop that
	// did nothing, which is the only reading of it that keeps the rest of the
	// macro going. It is checked last so that a payload the node cannot mean is
	// still reported before this quietly steps over it.
	if !task.hasWiredOutput(loopBodyHandle) {
		return loopLeaves(task, app, fmt.Sprintf("nothing is wired to its %q output", loopBodyHandle), frame)
	}

	frame.iterations++

	// The minimum yield, measured from the start of the previous iteration
	// rather than from the end of it, so a loop whose body already takes longer
	// than the minimum waits for nothing at all. A frame's first iteration has
	// no previous one and waits for nothing either.
	//
	// waitInterruptible rather than a second sleep of its own: it watches the
	// run's context, so the yield cannot be the thing that delays a stop.
	if !frame.lastIteration.IsZero() {
		if remaining := minLoopIterationYield - time.Since(frame.lastIteration); remaining > 0 {
			if !waitInterruptible(ctx, remaining) {
				// The user pressed Stop (or the run was torn down). No error and
				// no event, like every other interruptible handler: cancellation
				// is the run ending rather than this node failing, and the walk
				// checks the context the moment this returns, so the handle
				// below is never actually followed.
				log.Printf("%s %s: the per-iteration yield was cancelled", loopStartNodeType, task.ID)
				return loopDoneHandle, nil
			}
		}
	}
	frame.lastIteration = time.Now()

	log.Printf("%s %s: starting iteration %d", loopStartNodeType, task.ID, frame.iterations)
	app.emitEvent("task-success", map[string]interface{}{
		"taskID": task.ID,
		"type":   loopStartNodeType,
	})
	return loopBodyHandle, nil
}

// loopLeaves reports a loop ending and returns its `done` output.
//
// Its own function so that the two ways out - the condition holding and the
// count running out - report themselves identically, including the count of
// iterations actually run, which is the number a user comparing what happened
// with what they drew wants.
func loopLeaves(task Task, app *App, because string, frame *loopFrame) (string, error) {
	log.Printf("%s %s: leaving by %q after %d iteration(s), because %s",
		loopStartNodeType, task.ID, loopDoneHandle, frame.iterations, because)
	app.emitEvent("task-success", map[string]interface{}{
		"taskID": task.ID,
		"type":   loopStartNodeType,
	})
	return loopDoneHandle, nil
}

// executeLoopEndTask is the bottom of the loop, and it does nothing.
//
// That is the whole design, and it is the Sequence node's argument again. The
// node has exactly one output, `back`, and the editor has drawn an ordinary
// edge from it to the partner Loop Start; returning "" means "the only output",
// which takes every outgoing edge whatever its handle is named, so the token
// goes back round. There is no case for `back` anywhere in the walk, no pairing
// for the walk to resolve, and no jump to a named node - which is precisely
// what keeps interpreter.go free of any of this.
//
// **Nothing reads task.Data.** The frontend is expected to keep a `loopId` or a
// partner id on both halves so it can create, move and delete them together,
// and the backend neither validates it nor cares whether it is there. A future
// reader will otherwise assume the pair is checked somewhere: it is not, and
// that is the property that lets the pair be an editor construct. The
// degradations follow from it - a Loop End whose partner has been deleted ends
// its path here, and a Loop Start with no Loop End is a loop whose body simply
// runs out of edges.
//
// No context, for the same reason as the Start and Sequence nodes: it logs,
// emits and returns, with nothing in between for a cancellation to interrupt.
func executeLoopEndTask(_ context.Context, task Task, _ *runState, app *App) (string, error) {
	log.Printf("%s %s: back to the top of the loop", loopEndNodeType, task.ID)
	app.emitEvent("task-success", map[string]interface{}{
		"taskID": task.ID,
		"type":   loopEndNodeType,
	})
	return "", nil
}
