// tasks.go
//
// Dispatch of the node the token is on to the handler for its type. The
// individual handlers live in the actions_*.go files.

package backend

import (
	"context"
	"errors"
	"fmt"
	"log"
)

// executeTask performs the action based on the task type.
//
// next is the sourceHandle the run should leave this node by, and "" means "the
// only output" - which takes every outgoing edge whatever its handle is named,
// and is what all but three handlers return. A Branch node returns "true" or
// "false"; a Loop Start node returns "body" or "done"; Wait For Color returns
// "timeout" when the macro drew a timeout edge for it to take. A Loop End is
// not one of the three: it returns "" like an ordinary node, and the one edge
// that "" takes is the back edge. The walk
// (run.follow) is what turns either into the edge the token moves along.
//
// The error is the second half of a report, not the whole of one. Handlers still
// emit task-error themselves - that event is what the frontend's status panel and
// run marks listen for, and none of it changes here - and return the same value
// they put in it, so the two cannot drift.
//
// ctx is the context of the run this task belongs to, passed down rather than
// fetched. See the comment on the ctx parameter of executeDelayTask for why that
// distinction matters. state is the run's scratchpad, passed for the same
// reason: a handler that is given its state can be tested with a prepared one,
// and cannot reach a run's state that is not its own.
func executeTask(ctx context.Context, task Task, state *runState, app *App) (next string, err error) {
	log.Printf("Starting execution of task ID: %s, Type: %s", task.ID, task.Type)
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Recovered from panic in task %s: %v", task.ID, r)
			message := fmt.Sprintf("panic: %v", r)
			app.emitEvent("task-error", map[string]interface{}{
				"taskID": task.ID,
				"error":  message,
			})
			// Reported through both channels, like every other failure here: the
			// event for the status panel, the error for the caller. Assigning the
			// named returns is the only way a deferred function can say anything,
			// and without it a node that panicked would return whatever the
			// half-finished handler left behind - "" and nil, which reads as an
			// ordinary success.
			//
			// endPathHandle rather than "". A handler that panicked chose no
			// output, and "" is not "no output" - it is "the only output", which
			// takes every outgoing edge. A panic inside a Loop Start returning ""
			// took `body` and `done` at once, and because that is not `body` the
			// walk also popped the frame, so the next arrival counted from zero
			// and a loop with a count of 3 ran forever with its exit path already
			// taken. The token stops where the panic happened instead.
			next, err = endPathHandle, errors.New(message)
		}
		log.Printf("Completed execution of task ID: %s", task.ID)
	}()

	switch task.Type {
	case "StartNode":
		return executeStartNodeTask(ctx, task, state, app)

	case "SequenceNode":
		return executeSequenceTask(ctx, task, state, app)

	// "BranchNode" is the key BranchNode is registered under in
	// frontend/src/lib/components/Workspace/customNodes/nodeTypes.ts. It is the
	// first node type that picks between outputs; its two handles are "true"
	// and "false".
	case "BranchNode":
		return executeBranchTask(ctx, task, state, app)

	// "LoopStartNode" and "LoopEndNode" are the keys the two halves of a loop
	// are registered under in
	// frontend/src/lib/components/Workspace/customNodes/nodeTypes.ts. The start
	// decides, with handles "body" and "done", and the walk has already entered
	// its iteration frame by the time this is reached; the end does nothing and
	// returns "", which takes the one edge the editor drew from its "back"
	// handle. See loopStartNodeType.
	case loopStartNodeType:
		return executeLoopStartTask(ctx, task, state, app)

	case loopEndNodeType:
		return executeLoopEndTask(ctx, task, state, app)

	case "MouseMoveNode":
		return executeMouseMoveTask(ctx, task, state, app)

	case "MouseClickNode":
		return executeMouseClickTask(ctx, task, state, app)

	// "KeyPressNode" is the key KeyPressNode is registered under in
	// frontend/src/lib/components/Workspace/customNodes/nodeTypes.ts, which is
	// what ends up in the saved node's `type`. It replaced the "TypeString" and
	// "KeyTap" cases, which no node ever emitted.
	case "KeyPressNode":
		return executeKeyPressTask(ctx, task, state, app)

	case "DelayNode":
		return executeDelayTask(ctx, task, state, app)

	// "ColorPickerNode" is the key ColorPickerNode is registered under in
	// frontend/src/lib/components/Workspace/customNodes/nodeTypes.ts, which is
	// what ends up in the saved node's `type`.
	case "ColorPickerNode":
		return executeColorPickerTask(ctx, task, state, app)

	default:
		// Formatted once and used three times, so the log line, the event and
		// the returned error say the same thing by construction.
		message := fmt.Sprintf("Unknown task type: %s", task.Type)
		log.Printf("%s for task %s", message, task.ID)
		app.emitEvent("task-error", map[string]interface{}{
			"taskID": task.ID,
			"error":  message,
		})
		return "", errors.New(message)
	}
}

// executeStartNodeTask marks the beginning of a flow.
//
// The only handler that takes no context: it logs, emits and returns, with
// nothing in between for a cancellation to interrupt. The parameter is still in
// the signature because every handler has to have one shape for executeTask to
// dispatch to.
func executeStartNodeTask(_ context.Context, task Task, _ *runState, app *App) (string, error) {
	log.Printf("Flow Started - Task ID: %s", task.ID)
	app.emitEvent("task-success", map[string]interface{}{
		"taskID": task.ID,
		"type":   "StartNode",
	})
	return "", nil
}

// executeSequenceTask is how a macro fans out.
//
// It does nothing at all, and that is the whole design. An output handle takes
// exactly one edge (validateOutputHandles), so a node cannot fan out by having
// two wires drawn from it; a Sequence node has several output handles instead,
// and returning "" - "the only output" - makes the walk take every one of them
// in handle order, depth-first. All the behaviour is in run.follow, which means
// adding an output to a Sequence node is a frontend change and nothing else.
//
// No context, for the same reason as the Start node: it logs, emits and
// returns, with nothing in between for a cancellation to interrupt.
func executeSequenceTask(_ context.Context, task Task, _ *runState, app *App) (string, error) {
	log.Printf("Sequence - Task ID: %s", task.ID)
	app.emitEvent("task-success", map[string]interface{}{
		"taskID": task.ID,
		"type":   "SequenceNode",
	})
	return "", nil
}
