// interpreter.go
//
// The walk: one execution token moving over the flowchart. It runs the node the
// token is on, asks that node which output to leave by, and moves the token
// along the matching edge. A run ends when the token has nowhere to go or when
// the context is cancelled, and those are the only two ways.
//
// There is deliberately no third. An iteration budget used to stop a run after
// 100000 node executions, on the theory that a loop with no way out had to end
// somehow; Phase 4 removed it, because a loop with no way out is now a feature.
// A macro that clicks once a second forever is the canonical thing this app is
// for, and it would have hit that budget in a few hours and died with an error
// having done nothing wrong. What bounds a runaway loop instead is four things
// that do not punish a correct one:
//
//   - the Loop Start node's minimum per-iteration yield caps the *rate* of a
//     cycle that goes through one (minLoopIterationYield in actions_loop.go);
//   - the walk driver's wall-clock bound caps the rate of every other cycle,
//     including the two the loop node cannot govern - a cycle drawn with an
//     ordinary back-pointing edge, and one through a Loop Start that always
//     leaves by `done` (maxWalkWithoutYield in execution.go);
//   - the toggle-shaped RunMacro gives the user an exit;
//   - and the cancellation check between every node makes that exit prompt.
//
// The first two are one rule with two enforcers, and the second is the one that
// makes the argument complete: "the yield bounds it" is only true of a cycle
// that reaches a yield point at all.
//
// This replaced a dependency scheduler, which is why "run this node once every
// one of its prerequisites has finished" appears nowhere here. A node runs on
// arrival, as many times as the token arrives at it - which is what makes a
// loop an ordinary back-edge rather than a construct the engine has to know
// about.

package backend

import (
	"context"
	"log"
	"sort"
	"time"
)

// runState is a run's scratchpad: a map of name to a string or a number,
// created when a run starts and discarded when it ends.
//
// It is written by the "store result as" field on the nodes that produce a
// result (storeResult in actions_branch.go) and read by a Branch node's
// condition, and by nothing else. It lives on the run rather than on the App,
// which is the decision worth making before there is anything in it: a stopped
// macro's leftovers must not be visible to the next one, which is the hazard
// commit 33797cb was about. On the App it would need clearing, and a clear is
// something that can be forgotten.
//
// Numbers are stored as float64 - the type a JSON payload's numbers already
// decode to - so the comparison table has one numeric type to reason about
// rather than two that agree until they do not.
//
// It carries no lock, deliberately. A run is one goroutine walking one graph,
// so there is no second writer for a mutex to exclude; adding one would be
// guarding against a caller that does not exist and would suggest to the next
// reader that one does.
type runState struct {
	values map[string]any

	// held are the keys a Hold action has pushed down and no Release has let
	// back up. The type and the three methods that maintain it are in
	// actions_keyboard.go, next to the KeyDown and KeyUp calls they mirror.
	//
	// It is here rather than on the App for exactly the reason values is: this
	// is what a *particular* run left behind, and the next run must not inherit
	// it. It is here rather than in values because it is not a name a macro can
	// read - it is bookkeeping the run does about the real keyboard.
	held []heldKey
}

func newRunState() *runState {
	return &runState{values: make(map[string]any)}
}

// set records a value under name, replacing whatever was there.
//
// A nil *runState stores nothing rather than panicking. The walk always builds
// one, so nil means a handler was called by something that is not a run - a
// test driving one handler, and nothing else today - and a node that cannot
// store its result should still do its work.
func (s *runState) set(name string, value any) {
	if s == nil {
		return
	}
	s.values[name] = value
}

// value reads a name back. The second result is false when nothing was ever
// stored under it, which is not the same as a value that is empty - a
// distinction the isSet operator is built on, and the reason a node that stored
// nothing must leave its name absent rather than write "" into it.
//
// Nil-tolerant for the same reason set is: nothing was stored, so nothing is
// set.
func (s *runState) value(name string) (any, bool) {
	if s == nil {
		return nil, false
	}
	v, ok := s.values[name]
	return v, ok
}

// endPathHandle is what a handler returns when it has *not* chosen an output
// and the token must therefore go nowhere.
//
// It exists because "" is overloaded. "" means "the only output" and takes every
// outgoing edge, which is what makes a Sequence node work and what keeps every
// macro drawn before handles were named running (see follow). It was also what
// every failing handler returned - and on a node with more than one output those
// two meanings are opposites. A Branch that could not evaluate its condition
// returned "" and the walk ran *both* branches; a panic recovered in executeTask
// did the same for whatever node it happened to be in.
//
// executeLoopStartTask is the one handler that spotted this and routed even a
// malformed payload to `done` rather than "". This is that reasoning made
// available to every handler, instead of each one having to rediscover it.
//
// The NUL is deliberate and is what makes it a sentinel rather than a guess: an
// output handle is an id a Svelte component declares, so it is always a plain
// identifier, and no edge can carry this string. follow needs no special case -
// nothing matches it, so the path ends exactly as it does for a handle with no
// edge drawn from it.
//
// It does not stop the run. Anything else already on the stack still runs, which
// is the distinction between "this node chose nowhere to go" and "abandon the
// macro" - the walk has no way to say the second and deliberately never needed
// one.
const endPathHandle = "\x00end-path"

// runOutcome is how a run ended.
type runOutcome int

const (
	// runInProgress is the zero value: the run has not ended yet.
	runInProgress runOutcome = iota

	// runFinished means the token ran out of edges to follow. That is how a
	// macro ends normally, not a failure.
	runFinished

	// runCancelled means the context went away - the user pressed stop, or the
	// app is shutting down.
	runCancelled

	// runPanicked means the walk itself panicked and App.walk's recover caught
	// it. It is not one of the two ways a macro ends - it is a bug in this
	// package - but it is an ending, and it has to be one finishRun knows about:
	// a run that stops without being asked to must let go of whatever the macro
	// left held, exactly as a cancelled one does.
	runPanicked
)

// loopFrame is one Loop Start node's state for one entry into it: how many
// times its body has been started, and when the last of those started.
//
// One frame per *Loop Start*, keyed by that node's id. The Loop End it is
// paired with contributes nothing here - it runs nothing, and the edge from its
// `back` handle is what brings the token round to be counted. The backend
// therefore never needs to know which end belongs to which start; that pairing
// exists only so the editor can create, move and delete the two together.
//
// **Why a stack of these rather than an entry in the run state map keyed by
// node id.** Two things a flat map cannot say.
//
// A nested loop's counter has to die with the loop. An inner loop keyed by its
// own node id would keep counting where it left off when the outer loop came
// round again, so an inner "click 3 times" inside an outer "do this 5 times"
// would click three times in total rather than fifteen. A frame that is popped
// when the inner loop leaves by `done` is zero again on the next entry, by
// construction and without anything having to remember to clear it.
//
// And a flat worklist cannot tell a loop's first arrival from its re-entry. A
// frame can: a frame for this node already on the stack *is* the fact that the
// token has been here before in this iteration of whatever encloses it.
//
// The unwind on arrival - pop every frame above this node's - is what handles
// control leaving an inner loop's body by a route other than its own `done`
// output. A Branch inside the body wired to something outside it is a perfectly
// ordinary macro, and it leaves the inner frame on the stack with nothing to
// pop it; without the unwind, a later arrival at that inner loop would find a
// frame carrying the previous excursion's count and run the wrong number of
// times. Unwinding to the frame that was found makes re-entry mean what it
// looks like on the canvas.
//
// The node pair narrows that problem without removing it: the back edge is the
// editor's now, so no user draws a cycle by accident, but a Branch wired from
// inside one loop's body to somewhere outside it is still a macro someone will
// draw on purpose. The unwind stays.
//
// **An iteration is a scope, and the frame is what says where it began.** That
// is the second thing the pair earns: because a frame marks the top of the
// stack at the moment the loop was entered, the token coming round the back edge
// can throw away whatever that iteration left pending, exactly as a call frame's
// locals die when the call returns. stackDepth is that mark, and enterLoop is
// where it is used.
type loopFrame struct {
	// nodeID is the Loop Start node this frame belongs to. It is the identity the
	// stack is searched by.
	nodeID string

	// stackDepth is how deep run.stack was when this frame was pushed - the base
	// of one iteration's scope, recorded after the Loop Start itself was popped
	// and so describing everything *outside* the loop. The token arriving back at
	// this Loop Start truncates the stack to it. See enterLoop.
	stackDepth int

	// iterations is how many times the body has been started, which is what a
	// count is compared against.
	iterations int

	// lastIteration is when the body was last started, and the zero value means
	// it has not been yet. The minimum per-iteration yield is measured from it.
	lastIteration time.Time
}

// run is one execution of a macro: everything the walk needs, as data rather
// than as local variables in the goroutine that drives it.
//
// Three reasons it is a value.
//
//   - A test can build one and call step, asserting the order the nodes ran in
//     synchronously - no goroutine, no polling, and no reading events back out
//     of the log. That is what testdata/walk does.
//   - The cancellation check has one obvious home, between steps in the driver,
//     which is exactly what "checked between every node, not only inside
//     handlers" asks for.
//   - The Loop Start node needs somewhere to keep "iterations so far", and the
//     answer is the frames field below - a stack of loopFrames that die with
//     their loop. Adding a field here was an addition; unpicking a goroutine's
//     local variables would not have been.
type run struct {
	// nodes is every node in the flowchart, by id - not only the ones a run
	// reaches. Reachability is no longer something the engine prunes by: a node
	// the token never arrives at never runs, which is the same answer arrived
	// at for free.
	nodes map[string]Node

	// successors are each node's outgoing edges, in the order the token takes
	// them. See sortedSuccessors.
	successors map[string][]Edge

	// stack holds the nodes waiting for the token, top last. Depth-first falls
	// straight out of it: a node's successors are pushed in reverse, so the
	// first of them is on top and its whole subtree is walked before the second
	// is started.
	//
	// It is bounded by the graph rather than by the run's length, and the loop
	// frames are what keep it so: an iteration that ends leaves nothing of itself
	// pending, because enterLoop truncates back to the depth the iteration began
	// at. See loopFrame.stackDepth.
	stack []string

	// state is this run's scratchpad. Created here, discarded with the run.
	state *runState

	// frames are the Loop Start nodes the token is currently inside, outermost
	// first.
	// See loopFrame for why a stack and not a map.
	frames []*loopFrame

	// steps counts the nodes executed so far. Nothing is bounded by it - it is
	// what the run reports having done when it ends.
	steps int

	// exec runs one node and reports which output the token leaves it by. It is
	// App.executeNode for a real run; the fixture tests supply their own, which
	// is how a walk can be driven without executing anything or standing up an
	// App.
	//
	// The run state is an argument rather than something a handler reaches for,
	// which is the same argument that moved ctx to a parameter in Phase 1: a
	// handler that is handed its state can be tested with a prepared one, and
	// there is no second path by which a handler could find the state of a run
	// that is not the one it belongs to.
	exec func(ctx context.Context, task Task, state *runState) (string, error)

	// outcome is how the run ended.
	outcome runOutcome
}

// newRun prepares a run of flow starting at startID.
//
// It takes the whole flowchart rather than a pruned copy of it, for the reason
// on the nodes field.
func newRun(flow FlowData, startID string, exec func(ctx context.Context, task Task, state *runState) (string, error)) *run {
	nodes := make(map[string]Node, len(flow.Nodes))
	for _, node := range flow.Nodes {
		nodes[node.ID] = node
	}

	return &run{
		nodes:      nodes,
		successors: sortedSuccessors(flow, nodes),
		stack:      []string{startID},
		state:      newRunState(),
		exec:       exec,
	}
}

// sortedSuccessors groups the flow's edges by their source, in the order the
// token takes them: by SourceHandle, ascending.
//
// Handle order rather than canvas position or target id. An output handle takes
// exactly one edge (validateOutputHandles refuses a file where it does not), so
// fan-out is drawn as an explicit Sequence node whose handles are out-1, out-2,
// out-3 - and running them in handle order runs them in the order the user sees
// printed on the node. Nothing has to be inferred from geometry, and there is no
// tie-break to argue about. This is the rule Unreal Blueprints uses, and the
// reason execution order is unambiguous there.
//
// Edges pointing at a node the flowchart does not have are dropped here rather
// than handled in the walk: an edge can outlive its node in a hand-edited file,
// and a run must not invent a node from one.
//
// Two edges sharing one handle are ordered by their target id. validateOutputHandles
// refuses that graph before a run starts, so this only decides what a fixture
// driven straight into the walk does - but it has to be decided *here*, and this
// way, because nodeLabels.ts implements this same walk for the status panel and
// sorts by handle then target. Ordering by the order the file happened to list
// them in would leave the two implementations disagreeing on a case the shared
// fixtures in testdata/walk can express, which is the one thing those fixtures
// exist to catch. Sorting by content rather than by array position also means the
// answer does not depend on how the edges reached this function.
func sortedSuccessors(flow FlowData, nodes map[string]Node) map[string][]Edge {
	successors := make(map[string][]Edge)
	for _, edge := range flow.Edges {
		if _, ok := nodes[edge.Source]; !ok {
			continue
		}
		if _, ok := nodes[edge.Target]; !ok {
			continue
		}
		successors[edge.Source] = append(successors[edge.Source], edge)
	}

	for source := range successors {
		edges := successors[source]
		sort.SliceStable(edges, func(i, j int) bool {
			if edges[i].SourceHandle != edges[j].SourceHandle {
				return edges[i].SourceHandle < edges[j].SourceHandle
			}
			return edges[i].Target < edges[j].Target
		})
	}
	return successors
}

// task is the node the token is on, in the shape a handler takes it: its
// payload, plus the output handles the graph has wired to it.
//
// The handles come from here rather than from the node because the node does
// not know them - an output is wired by an edge, and a node's payload says
// nothing about what is drawn from it. A handler needs them when one of its
// outputs is conditional on the graph: Wait For Color reports a timeout as a
// failure unless the macro drew a `timeout` edge to say where a timeout should
// go. It is a list of handles rather than a flag named after that one output so
// that the next node with a graph-conditional output needs nothing added here.
//
// The successors are already in ascending handle order (sortedSuccessors), so
// the list comes out sorted for free; consecutive repeats are dropped, which
// validateOutputHandles has already refused for a real run but a fixture driven
// straight into the walk can still carry.
func (r *run) task(node Node) Task {
	edges := r.successors[node.ID]
	handles := make([]string, 0, len(edges))
	for _, edge := range edges {
		if len(handles) > 0 && handles[len(handles)-1] == edge.SourceHandle {
			continue
		}
		handles = append(handles, edge.SourceHandle)
	}

	return Task{
		ID:           node.ID,
		Type:         node.Type,
		Data:         node.Data,
		WiredOutputs: handles,
	}
}

// step runs the node the token is on and moves the token on. It reports the id
// of the node it ran, and whether the run continues.
//
// Everything that ends a run is decided here or by the driver's cancellation
// check, and each sets outcome before returning false.
func (r *run) step(ctx context.Context) (string, bool) {
	if len(r.stack) == 0 {
		// The token has nowhere left to go. That is how a macro finishes.
		r.outcome = runFinished
		return "", false
	}

	current := r.stack[len(r.stack)-1]
	r.stack = r.stack[:len(r.stack)-1]
	r.steps++

	node := r.nodes[current]
	task := r.task(node)

	// A Loop Start's counter belongs to the walk, not to the node: it is a fact
	// about where the token has been, which is the one thing a handler cannot
	// see. The frame is entered here, on arrival, and handed to the handler on
	// the task the same way the wired output handles are.
	//
	// Keyed by the Loop Start's node id, and by nothing else. The editor pairs
	// each start with a Loop End and may keep a partner id on both payloads; the
	// walk neither reads it nor needs it, because the Loop End is an ordinary
	// node whose ordinary outgoing edge happens to point backwards. That is why
	// the pair cost this file nothing.
	if node.Type == loopStartNodeType {
		task.Loop = r.enterLoop(node.ID)
	}

	next, err := r.exec(ctx, task, r.state)

	// Any exit from a loop that is not its body ends this entry into it, so the
	// frame goes. Written as "not the body" rather than "the done output" on
	// purpose: a Loop Start with a payload it cannot mean reports the error and
	// leaves by `done`, and a broken loop must not be the one that keeps its
	// frame alive.
	if node.Type == loopStartNodeType && next != loopBodyHandle {
		r.leaveLoop(node.ID)
	}

	// A handler that failed does not stop the run, and that is deliberate
	// rather than an oversight. It is what the dependency scheduler did - a
	// failed task still let its dependents run - and the design lists "error
	// handling should be an output" as a later feature: an optional "on error"
	// edge from any node, which is a thing to follow rather than a thing to
	// abort on. The handler has already emitted task-error, so the status panel
	// knows; this line is what puts the failure in the log next to the node it
	// belongs to.
	if err != nil {
		log.Printf("Task %s failed: %v", current, err)
	}

	// Checked the moment the handler returns and before any edge is followed,
	// so a loop of instantaneous nodes stops promptly rather than at whatever
	// point a handler next happens to consult its context.
	if ctx.Err() != nil {
		r.outcome = runCancelled
		return current, false
	}

	r.push(r.follow(current, next))
	return current, true
}

// follow returns the nodes the token moves to after nodeID left by next.
//
// next == "" is "the only output" and takes *every* outgoing edge, whatever its
// SourceHandle says. It emphatically does not mean "the edges whose
// sourceHandle is empty": every edge this app draws carries sourceHandle
// "right", so a handle-equality rule would strand every macro in the tree at
// the Start node. It is also what makes a Sequence node work - three handles,
// no handler logic, the walk's own rule doing all of it.
//
// A named next takes the one edge on that handle. No matching edge - including
// a next naming a handle nothing is wired to, and endPathHandle, which is named
// so that nothing can ever be wired to it - ends that path normally: it is how a
// macro finishes, not an error, and nothing here panics over it.
func (r *run) follow(nodeID, next string) []string {
	edges := r.successors[nodeID]
	targets := make([]string, 0, len(edges))
	for _, edge := range edges {
		if next != "" && edge.SourceHandle != next {
			continue
		}
		targets = append(targets, edge.Target)
	}
	return targets
}

// push puts targets on the stack in reverse, so the first of them is on top and
// its whole subtree runs before the second is started.
func (r *run) push(targets []string) {
	for i := len(targets) - 1; i >= 0; i-- {
		r.stack = append(r.stack, targets[i])
	}
}

// enterLoop is the token arriving at a Loop Start node, and returns the frame
// that arrival belongs to.
//
// A frame already on the stack for this node means the token has come round the
// back-edge, so that frame is the one to carry on counting with - and every
// frame above it belongs to a loop the token has now left, whatever route it
// left by, so those are unwound. A node with no frame is a fresh entry and gets
// a fresh one, counting from zero.
//
// **The re-entry also ends the iteration, and ending it truncates the stack.**
// An iteration is a scope: anything still pending from it is abandoned when it
// ends, exactly as a call frame's locals die when the call returns. Without
// that, a Sequence inside a body whose Loop End is on the *earlier* handle
// leaks - and leaks silently, because the canvas cannot show it:
//
//	LoopStart --body--> Sequence --out-1--> LoopEnd --back--> LoopStart
//	                             --out-2--> MouseClick
//
// out-1 is taken first and its whole subtree - the rest of the loop, forever -
// is supposed to run before out-2 is started, so every turn appends another
// pending MouseClick that nothing ever pops. The stack grows without bound at
// the rate the loop turns (~100 entries a second at minLoopIterationYield), and
// then, when the loop finally leaves by `done`, the whole backlog runs: N clicks
// back to back, which nobody drew. Swapping the two handles makes the same
// picture behave correctly, which is precisely why this cannot be left to the
// user to notice.
//
// Two things this must not do, and both fall out of doing it here rather than in
// leaveLoop:
//
//   - **The first arrival records the depth and truncates nothing.** It is not a
//     re-entry; the stack it finds belongs to whatever enclosed the loop, and
//     dropping any of it would strand the rest of the macro.
//   - **Leaving by `done` is unaffected.** The truncation happens before the
//     handler picks a handle, so `body` and `done` are both followed from the
//     same depth the iteration began at - the exit pushes its target onto
//     exactly the stack the loop was entered with.
//
// The truncation is skipped when the stack is already shorter than the mark.
// That is not defensive coding: it means the token has already popped its way
// out past where this iteration began - control left the body sideways and came
// back to this Loop Start by another route, the case the unwind above exists for
// - so there is nothing of the iteration left pending to abandon, and the
// entries that are there belong to something that encloses it.
func (r *run) enterLoop(nodeID string) *loopFrame {
	for i := len(r.frames) - 1; i >= 0; i-- {
		if r.frames[i].nodeID != nodeID {
			continue
		}
		r.frames = r.frames[:i+1]
		frame := r.frames[i]
		if frame.stackDepth < len(r.stack) {
			r.stack = r.stack[:frame.stackDepth]
		}
		return frame
	}

	frame := &loopFrame{nodeID: nodeID, stackDepth: len(r.stack)}
	r.frames = append(r.frames, frame)
	return frame
}

// leaveLoop pops a Loop Start node's frame, so that a later arrival counts from
// zero.
//
// It pops only when the top of the stack really is this node's frame. It always
// is, because enterLoop unwound everything above it a moment ago and a handler
// cannot push one; the check is what keeps that an assertion rather than an
// assumption, and it means a stack that has somehow got out of step loses
// nothing it still needs.
func (r *run) leaveLoop(nodeID string) {
	if n := len(r.frames); n > 0 && r.frames[n-1].nodeID == nodeID {
		r.frames = r.frames[:n-1]
	}
}
