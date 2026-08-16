// execution.go
//
// Flow execution: the entry points a run starts from, the checks and lints that
// run before it, the goroutine that drives it, and the state the app reports
// while it is going. The walk itself is in interpreter.go.

package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"
)

// StartExecution receives the flowchart data and starts execution. It is what
// the workspace calls to run the graph the user currently has on the canvas,
// saved or not.
func (a *App) StartExecution(flow string) error {
	var flowData FlowData
	if err := json.Unmarshal([]byte(flow), &flowData); err != nil {
		log.Printf("Failed to unmarshal flowchart: %v", err)
		return fmt.Errorf("invalid flowchart data: %w", err)
	}
	return a.startFlow(flowData)
}

// RunMacro loads a saved macro by id and runs it, without the caller having to
// hold the graph. It is what the tray menu and the global hotkeys use: neither
// has a frontend to get the flowchart from, and both have to work with the
// window closed.
//
// It is bound to the frontend as well, so the projects list can run a macro
// without opening it in the workspace first.
//
// **It is a toggle.** A run in progress is stopped and nothing is started; only
// an idle app starts one. This is AutoHotkey's canonical `Toggle := !Toggle`,
// expressed with the context cancellation this app already has instead of a
// shared flag, and it is what makes an endless loop a usable macro rather than
// a trap: with the iteration budget gone, the hotkey the user pressed to start
// a loop is the way out of it. The re-entrancy hazard those threads keep
// hitting - a second press landing inside the first press's still-running loop
// - is commit 33797cb's bug, and the runner's generation token is what stays to
// make this safe.
//
// **startFlow deliberately keeps its refusal**, and the toggle lives only here.
// The two entry points want different answers. This one is a hotkey or a tray
// item: one chord, no second key, and the user pressing it while a macro runs
// can only mean stop. StartExecution is the workspace's Run button, which sits
// next to a Stop button - a user who wants to stop presses that, and a Run
// press that silently stopped the run would be indistinguishable from one that
// started a fresh one. So the shared start path still reports "execution
// already in progress" and TestStartFlowRefusesASecondRun stays valid.
//
// The stop and the start are not one atomic operation, and cannot be while
// startFlow takes execMutex itself. What is left is the window between finding
// the app idle and startFlow claiming it, in which a second press could load
// its macro and then be refused by startFlow's guard - which is exactly what a
// double-press does today, reported the same way. Nothing new is possible in
// it: the two presses cannot both start a run, because the refusal is under the
// same lock as the claim.
func (a *App) RunMacro(id string) error {
	if a.stopIfRunning() {
		log.Printf("RunMacro %q: a run was in progress, so the press stopped it", id)
		return nil
	}

	flowData, err := a.LoadProject(id)
	if err != nil {
		log.Printf("RunMacro %q: %v", id, err)
		return err
	}

	if err := a.startFlow(*flowData); err != nil {
		log.Printf("RunMacro %q: %v", id, err)
		return err
	}

	// The workspace may be showing a different macro - or nothing at all, if
	// the window is closed - so tell it which macro these task events belong
	// to rather than let it assume they are the graph on the canvas.
	a.emitEvent("macro-started", MacroRun{ID: flowData.ID, Name: flowData.Name})
	return nil
}

// startFlow runs a flowchart. Both entry points funnel through here so the
// execution state, the panic guard and the graph setup have exactly one
// implementation.
//
// execMutex is held for the whole of it, and StopExecution and ServiceShutdown
// take it too: that is what stops a run beginning while the previous one is
// still being torn down. It is safe to hold across Runner.Go because Go only
// launches - the wait is Stop's, and the walk goroutine takes no lock of the
// App's on its way out.
func (a *App) startFlow(flowData FlowData) error {
	a.execMutex.Lock()
	defer a.execMutex.Unlock()

	if a.GetIsExecuting() {
		log.Println("startFlow called but execution is already in progress")
		return errors.New("execution already in progress")
	}

	// Asked here, before the flag below is claimed, rather than only at the
	// launch at the end: setExecuting writes twenty-odd tray menu items each
	// way, and a refusal that arrives after ServiceShutdown has returned would
	// be writing them while the main thread destroys the menu they belong to.
	//
	// The read is authoritative for every caller there is today - it is under
	// execMutex, and the only in-app Close is ServiceShutdown's, which holds
	// execMutex too - so nothing can close the runner between here and the
	// launch below. Go re-checks anyway, and remains the authority, because it
	// is what a future Close caller that does not hold execMutex would be
	// checked by.
	if a.runner.Closed() {
		log.Println("startFlow called while the app is shutting down")
		return errRunnerClosed
	}

	a.setExecuting(true)
	log.Println("Execution started")
	defer func() {
		if r := recover(); r != nil {
			a.setExecuting(false)
			log.Printf("Recovered from panic in startFlow: %v", r)
			a.emitEvent("execution-error", fmt.Sprintf("panic: %v", r))
		}
	}()

	// Everything a run can be refused for is decided here, before a single node
	// has run. A macro that fails halfway has already typed half of itself into
	// whatever the user had focused.
	if len(flowData.Nodes) == 0 {
		return a.refuseRun(errors.New("flowchart must contain at least one node"))
	}

	// The run is defined by the Start node, so locate it before anything is
	// built from the graph.
	startNode, err := a.findStartNode(flowData.Nodes)
	if err != nil {
		return a.refuseRun(err)
	}

	if err := validateOutputHandles(flowData); err != nil {
		return a.refuseRun(err)
	}

	// The lints. Neither decides what runs - the token simply never arrives at
	// an unreachable node - so both are here to tell the user something about
	// the graph they drew, and nothing below reads their answers.
	a.warnAboutSkippedNodes(flowData, reachableFrom(startNode.ID, adjacency(flowData), nodesByIDFrom(flowData)))
	a.warnAboutMultipathNodes(flowData, startNode.ID)

	r := newRun(flowData, startNode.ID, a.executeNode)

	// Captured together in one call so the token and the context cannot
	// describe different generations - see Runner.Current. The token is what
	// makes the launch below refuse to start a run into a generation that has
	// been stopped since.
	//
	// A refusal is returned rather than swallowed, and that is the whole of the
	// shutdown fix at this end: the runner is closed once ServiceShutdown has
	// run, so a hotkey or a tray click arriving during teardown is told that
	// nothing started instead of leaving the caller - and the user - to assume a
	// run is under way. refuseRun clears the executing flag this function set on
	// its way in, so a refused run leaves the app exactly as idle as it found it.
	execGen, execCtx := a.runner.Current()
	if err := a.runner.Go(execGen, func() { a.walk(execCtx, r) }); err != nil {
		return a.refuseRun(err)
	}

	return nil
}

// refuseRun clears the execution state a refused run had already set and
// returns the error to hand back, so every refusal in startFlow is one line and
// none of them can forget to leave the app idle.
func (a *App) refuseRun(err error) error {
	a.setExecuting(false)
	log.Printf("startFlow failed: %v", err)
	return err
}

// nodesByIDFrom indexes a flowchart's nodes by id.
func nodesByIDFrom(flow FlowData) map[string]Node {
	nodes := make(map[string]Node, len(flow.Nodes))
	for _, node := range flow.Nodes {
		nodes[node.ID] = node
	}
	return nodes
}

// adjacency is the flowchart as target lists by source, which is the shape
// reachableFrom reads.
func adjacency(flow FlowData) map[string][]string {
	edges := make(map[string][]string)
	for _, edge := range flow.Edges {
		edges[edge.Source] = append(edges[edge.Source], edge.Target)
	}
	return edges
}

// validateOutputHandles refuses a flowchart that wires two edges to one output
// handle.
//
// An output takes exactly one edge, and fan-out is an explicit Sequence node -
// the rule Unreal Blueprints uses, and what makes execution order unambiguous
// there. The editor will not draw a second wire from a handle, but a file can
// be hand-edited, and the walk is deliberately not made to cope with it: with
// one edge per handle there is no order to invent for a case the user cannot
// see.
//
// It is a refusal rather than a warning because the alternative is discovering
// it halfway through a run, with half a macro already typed into whatever the
// user had focused.
func validateOutputHandles(flow FlowData) error {
	// Counted per source, then reported in flowchart order with the handles of
	// one node sorted, so the same broken file always names the same edge first.
	perNode := make(map[string]map[string]int, len(flow.Nodes))
	for _, edge := range flow.Edges {
		handles, ok := perNode[edge.Source]
		if !ok {
			handles = make(map[string]int)
			perNode[edge.Source] = handles
		}
		handles[edge.SourceHandle]++
	}

	for _, node := range flow.Nodes {
		handles := perNode[node.ID]
		names := make([]string, 0, len(handles))
		for handle := range handles {
			names = append(names, handle)
		}
		sort.Strings(names)

		for _, handle := range names {
			if handles[handle] > 1 {
				return fmt.Errorf(
					"node %s has %d edges leaving output %q: an output takes exactly one edge, so fan out with a Sequence node",
					node.ID, handles[handle], handle)
			}
		}
	}
	return nil
}

// findStartNode locates the StartNode in the flowchart.
func (a *App) findStartNode(nodes []Node) (Node, error) {
	for _, node := range nodes {
		if node.Type == "StartNode" {
			return node, nil
		}
	}
	return Node{}, errors.New("no Start node found in flowchart")
}

// =============================================== the walk ===============================================

// maxWalkWithoutYield is the longest the walk may run without giving anything
// else a turn, and walkYield is the turn it gives.
//
// **This is what genuinely replaces the iteration budget for a cycle the loop
// node does not govern.** The per-iteration yield in actions_loop.go bounds the
// rate of a loop drawn as the Loop Start / Loop End pair, and the header of
// interpreter.go used to claim that plus the toggle and the cancellation check
// covered everything. It does not: nothing yields in a cycle with no Loop Start
// in it - an ordinary edge drawn backwards, which validateOutputHandles does not
// refuse and the multipath lint deliberately excludes as a back-edge - nor in a
// cycle through a Loop Start that always leaves by `done`, because the frame is
// popped on the way out and the next arrival makes a fresh one with nothing to
// measure a yield from. Both spin at whatever rate the machine can manage. The
// toggle still stops them promptly, so neither is an unstoppable run; what is
// unmitigated without this is exactly the harm the yield exists to prevent - a
// core pinned at 100%, and an OS input queue filled faster than the target
// application drains it, which the user then watches empty long after they
// pressed stop.
//
// The shape is Scratch's, as docs/control-flow-next-steps.md recommends under
// "Yield discipline": keep the declared yield points where they are, and add a
// wall-clock bound on running without reaching one. Scratch calls its bound
// WARP_TIME and sets it to 500ms; the same value is right here for the same
// reason - it is short enough that a runaway cycle cannot outrun the input queue
// by much, and long enough that it is never reached by a macro doing anything
// real. A tighter bound would start costing macros that are behaving.
//
// walkYield is minLoopIterationYield, and deliberately the same size of gap for
// the same reason: one turn of ~10ms is above any rate a person can tell from
// instant, and is what the OS input queue needs to be given rather than a number
// tuned here. So a runaway cycle costs 10ms per 500ms - a 2% duty cycle on the
// bound, and the difference between "as fast as the machine" and "a hundred
// nodes a second" for everything downstream.
const (
	maxWalkWithoutYield = 500 * time.Millisecond
	walkYield           = minLoopIterationYield
)

// walk drives a run to its end. It is the goroutine Runner.Go tracks, and it
// is deliberately thin: check the context, take one step, repeat. Everything
// else is on the run.
//
// The cancellation check here is what "checked between every node" means for
// the gap between one node finishing and the next starting; step makes the
// other half of the check, between a handler returning and its edges being
// followed. Together they mean a loop of instantaneous nodes stops promptly.
//
// It takes no lock of the App's, and must not: StopExecution holds execMutex
// across Runner.Stop, which waits for this goroutine to return.
func (a *App) walk(ctx context.Context, r *run) {
	// finishing says the ordinary exit path has already committed to finishRun.
	// It is what lets the recover below tell a panic in the walk from a panic
	// inside finishRun itself, and not answer the second by calling finishRun
	// again: re-entering the function that has just panicked would very likely
	// panic again, and a second panic while a deferred recover is running is the
	// process, not a recovered run.
	finishing := false

	// The same guard startFlow has, on this side of the goroutine boundary,
	// where it now matters more: a panic here would otherwise take down an app
	// that is meant to sit in the tray for weeks. Individual handlers are
	// already guarded inside executeTask, so what this catches is a bug in the
	// walk itself - and it must still leave the app idle rather than stuck
	// reporting a run that is over.
	//
	// **And it must let go of the keyboard.** A panicked run is a run that
	// ended without being asked to, so a Ctrl a Hold node left down is left down
	// over every window the user touches next - which is the exact failure
	// releaseHeldKeys was added to prevent, on the one path that used to skip it.
	// This used to clear the flag and return, reaching neither finishRun nor the
	// release inside it; now it names the outcome and goes through finishRun like
	// every other ending. runPanicked rather than runCancelled because nobody
	// cancelled anything, and the frontend has already been told by the
	// execution-error above - a second ending event would report the run twice.
	defer func() {
		if p := recover(); p != nil {
			log.Printf("Recovered from panic in the walk: %v", p)
			a.emitEvent("execution-error", fmt.Sprintf("panic: %v", p))

			if finishing {
				// The panic came out of finishRun. The release is idempotent, so
				// doing it here costs nothing and covers the case where finishRun
				// died before reaching it. Cleared last, for the reason in
				// finishRun.
				r.state.releaseHeldKeys()
				a.setExecuting(false)
				return
			}

			r.outcome = runPanicked
			a.finishRun(r)
		}
	}()

	// The wall-clock bound above, in the one place that sees every node: what it
	// measures is time spent walking without anything having given way, so it is
	// reset by anything that did. A step that took at least a yield's worth of
	// wall time has already produced the gap the yield exists to create - it
	// blocked in waitInterruptible, in the loop's per-iteration yield or on the
	// colour poll's ticker, or it did that much real work, and in either case
	// forcing another 10ms on top of it would buy nothing. That is what keeps a
	// macro with real delays in it from paying anything at all for this.
	lastYield := time.Now()

	for {
		if ctx.Err() != nil {
			r.outcome = runCancelled
			break
		}

		if time.Since(lastYield) >= maxWalkWithoutYield {
			// waitInterruptible rather than a sleep, for the reason the loop's
			// yield uses it: this must never be the thing that delays a stop.
			if !waitInterruptible(ctx, walkYield) {
				r.outcome = runCancelled
				break
			}
			lastYield = time.Now()
		}

		started := time.Now()
		if _, ok := r.step(ctx); !ok {
			break
		}
		if time.Since(started) >= walkYield {
			lastYield = time.Now()
		}
	}

	finishing = true
	a.finishRun(r)
}

// executeNode runs one node and reports which output the token leaves it by.
// It is the exec a real run is built with.
//
// The task-started and task-completed events are emitted here, around the
// handler: the frontend's status panel and run marks listen for them, and one
// pair per node executed is what they expect.
//
// The task arrives built - the walk assembles it, because the wired output
// handles on it are a property of the graph rather than of the node - and the
// run state is passed straight through to the handler. Neither is fetched from
// the App: the state belongs to one run, and an App that could hand it out
// would be the leftovers hazard the run state was put on the run to avoid.
func (a *App) executeNode(ctx context.Context, task Task, state *runState) (string, error) {
	log.Printf("Processing task %s of type %s", task.ID, task.Type)
	a.emitEvent("task-started", task.ID)

	next, err := executeTask(ctx, task, state, a)

	a.emitEvent("task-completed", task.ID)
	return next, err
}

// finishRun reports how the run ended and then clears the execution state.
//
// This is the only place a run's ending is announced. StopExecution used to
// emit execution-stopped as well, which made every user stop report itself
// twice - and, when the stop landed just after the walk had finished by itself,
// report both a completion and a stop for one run. The walk is the side that
// knows *how* the run ended, so it owns the event.
//
// The order matters and is the opposite of the obvious one. setExecuting is not
// a bare atomic store: it also disables twenty-odd tray menu items, each a Win32
// call. Clearing first would open a window in which startFlow's guard already
// says "idle" - so a hotkey could start run B - while run A's
// execution-completed has not gone out yet, and the frontend would apply A's
// ending to B, wiping B's run marks mid-macro. Announcing first closes it: a run
// that can be started is a run whose predecessor has already been reported.
//
// The run state goes out of scope with r. It is not cleared anywhere, because
// there is nowhere it could have been left behind - with one thing to do first:
// a run that ended without choosing to, cancelled or panicked, lets go of any
// key a Hold node left down, below.
//
// It is reached from both of the walk's exits - the ordinary one and its
// recover - so every ending goes through this switch and none of them can be the
// one that skips the release.
func (a *App) finishRun(r *run) {
	switch r.outcome {
	case runFinished:
		log.Printf("Execution finished after %d node(s)", r.steps)
		a.emitEvent("execution-completed", nil)
	case runCancelled:
		// Before the event and before the flag, because this is physical: the
		// keystroke that stopped the macro arrived while the macro may have had
		// modifiers down, and a Ctrl left held affects every window the user
		// touches next. Only on an ending the macro did not choose - a macro that
		// ends normally having deliberately left a key held is the documented
		// behaviour of the Hold action. See releaseHeldKeys.
		r.state.releaseHeldKeys()
		log.Printf("Execution stopped after %d node(s)", r.steps)
		a.emitEvent("execution-stopped", nil)
	case runPanicked:
		// The same release, for the same physical reason and with more cause: a
		// run that died mid-macro chose nothing about where it stopped, so
		// whatever it was holding is held for no reason at all.
		//
		// No event. App.walk's recover has already emitted execution-error, which
		// is what the frontend clears its run marks on; announcing an ending here
		// as well would report one run twice, which is the duplication finishRun
		// was made the only announcer to end.
		r.state.releaseHeldKeys()
		log.Printf("Execution ended in a panic after %d node(s)", r.steps)
	case runInProgress:
		// Unreachable: walk only calls this once the run has ended, and every
		// exit sets an outcome. Spelled out rather than folded into a default,
		// so that this switch reads as the list of every way a run can end and a
		// new one that nobody reported is visible as a missing case - nothing
		// checks that for us.
		log.Printf("Execution ended without an outcome after %d node(s)", r.steps)
	}

	a.setExecuting(false)
}

// =============================================== the lints ===============================================

// reachableFrom walks the flow forwards from startID and returns the set of
// node IDs execution can get to. Edges pointing at IDs that are not in nodes
// are ignored, and a node is only ever expanded once, so a cycle terminates
// the walk instead of spinning in it.
//
// It no longer decides what runs. The interpreter runs a node when the token
// arrives at it, so a node with no path from Start is never reached and needs
// no pruning. What it still answers is exactly the question the editor wants -
// "is this node wired to anything that leads to it?" - which is what
// warnAboutSkippedNodes reports and what the frontend's own copy of this walk
// (nodeLabels.ts) orders the status panel by.
func reachableFrom(startID string, edges map[string][]string, nodes map[string]Node) map[string]bool {
	reachable := make(map[string]bool)
	if _, exists := nodes[startID]; !exists {
		return reachable
	}

	reachable[startID] = true
	queue := []string{startID}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, next := range edges[current] {
			if reachable[next] {
				continue
			}
			if _, exists := nodes[next]; !exists {
				continue
			}
			reachable[next] = true
			queue = append(queue, next)
		}
	}
	return reachable
}

// warnAboutSkippedNodes tells the frontend which connected nodes the run
// leaves out, so a branch that silently does nothing is visible rather than
// mysterious.
//
// Only nodes that are wired to something are reported: a node in no edge at
// all has always been excluded, and one the user has just dropped on the
// canvas and not connected yet is a normal editing state, not a surprise. A
// node with edges, on the other hand, looks like it should run, so being told
// that it does not is the whole point of the warning.
//
// The payload is node IDs, not text: IDs mean nothing on screen, and the
// frontend already labels them the way the canvas does. Wording it here would
// be a second, disagreeing naming scheme in the same status panel.
func (a *App) warnAboutSkippedNodes(flowData FlowData, reachable map[string]bool) {
	connected := make(map[string]bool)
	for _, edge := range flowData.Edges {
		connected[edge.Source] = true
		connected[edge.Target] = true
	}

	// Reported in flowchart order so the same graph always produces the same
	// message.
	skipped := make([]string, 0, len(flowData.Nodes))
	for _, node := range flowData.Nodes {
		if reachable[node.ID] || !connected[node.ID] {
			continue
		}
		skipped = append(skipped, node.ID)
	}
	if len(skipped) == 0 {
		return
	}

	log.Printf("Skipping %d node(s) not reachable from Start: %s",
		len(skipped), strings.Join(skipped, ", "))
	a.emitEvent("execution-nodes-skipped", skipped)
}

// warnAboutMultipathNodes tells the frontend which nodes the run reaches by
// more than one path, and therefore runs more than once.
//
// Reported in flowchart order, and as ids rather than prose, for the same
// reasons as the skipped-node warning next to it.
func (a *App) warnAboutMultipathNodes(flowData FlowData, startID string) {
	multipath := multipathNodes(flowData, startID)
	if len(multipath) == 0 {
		return
	}

	log.Printf("%d node(s) are reachable from Start by more than one path and will run more than once: %s",
		len(multipath), strings.Join(multipath, ", "))
	a.emitEvent("execution-nodes-multipath", multipath)
}

// multipathNodes lists the nodes reachable from Start by more than one path, in
// flowchart order.
//
// Defined as: a node with two or more incoming edges whose source is reachable
// from Start, counting only edges that are not back-edges.
//
// What it catches is two unrelated branches joining into one node - an easy
// accident, and one whose consequence (the node, and everything after it, runs
// twice) is invisible on the canvas. It is not a warning about diamonds as
// such: an output handle takes one edge, so a diamond cannot be drawn by
// accident at all - it takes a deliberate Sequence node wired to a common
// target, which is a thing a user might well mean.
//
// Back-edges are excluded, and that is not decoration. Phase 4 makes a loop an
// edge pointing backwards, and a loop header has two incoming edges by
// construction - the one that enters the loop and the one that comes round
// again - so without the exclusion every loop a user draws would be reported as
// a mistake.
//
// **A hand-drawn back edge is excluded too, and is not reported anywhere else.**
// The editor owns the loop pair's back edge, but nothing stops a user wiring a
// Branch's `false` output to an earlier node, and that cycle is invisible to
// every lint here. Its rate is bounded (maxWalkWithoutYield, above) and the
// toggle stops it, so it is not dangerous - it is merely undeclared, and a user
// who drew it by accident is told nothing. Reporting it would be a third warning
// beside these two: the classification below already knows which edges are
// back-edges, so what it needs is to keep the ones whose source is not a
// LoopEndNode leaving by `back`, and a frontend listener to name them. It is not
// built because the payload is the open question - these two warnings carry node
// ids, and the frontend labels nodes, while what a user needs pointed at here is
// an *edge*.
//
// They are classified by a depth-first search from Start: an edge whose target
// is on the DFS stack when the edge is examined points at an ancestor of its
// source, which is what a back-edge is. The search follows a node's outgoing
// edges in the order the flowchart lists them, which is what makes the answer
// reproducible - it is also the rule the fixtures in testdata/walk are written
// against.
//
// Edges pointing into the Start node are ignored: it runs unconditionally,
// whatever is wired to it.
func multipathNodes(flow FlowData, startID string) []string {
	nodes := nodesByIDFrom(flow)
	if _, ok := nodes[startID]; !ok {
		return nil
	}

	// Edge indices by source, in flowchart order, so an edge can be identified
	// again once the search has classified it.
	outgoing := make(map[string][]int, len(flow.Edges))
	for i, edge := range flow.Edges {
		if _, ok := nodes[edge.Source]; !ok {
			continue
		}
		if _, ok := nodes[edge.Target]; !ok {
			continue
		}
		outgoing[edge.Source] = append(outgoing[edge.Source], i)
	}

	backEdge := make(map[int]bool)
	visited := make(map[string]bool)
	onStack := make(map[string]bool)

	var search func(id string)
	search = func(id string) {
		visited[id] = true
		onStack[id] = true
		for _, i := range outgoing[id] {
			target := flow.Edges[i].Target
			switch {
			case onStack[target]:
				backEdge[i] = true
			case !visited[target]:
				search(target)
			}
		}
		onStack[id] = false
	}
	search(startID)

	arrivals := make(map[string]int, len(nodes))
	for i, edge := range flow.Edges {
		if backEdge[i] || edge.Target == startID || !visited[edge.Source] {
			continue
		}
		if _, ok := nodes[edge.Target]; !ok {
			continue
		}
		arrivals[edge.Target]++
	}

	multipath := make([]string, 0, len(nodes))
	for _, node := range flow.Nodes {
		if arrivals[node.ID] > 1 {
			multipath = append(multipath, node.ID)
		}
	}
	return multipath
}

// =============================================== execution state ===============================================

// StopExecution stops the ongoing execution.
//
// execMutex is held for the whole of it, including across Runner.Stop's wait
// for the walk goroutine, so that a new run cannot begin while the stopped one
// is still winding down. That is safe only because the walk takes no lock of
// the App's: it emits its last event and clears the execution state through an
// atomic, and neither touches execMutex. A walk that reached for it would
// deadlock against the wait below, which is the shape this whole lifecycle is
// arranged to avoid.
//
// Stop and not Close: this is the user pressing stop, and the next macro they
// start has to work. ServiceShutdown is the caller that means it terminally.
//
// The runner is stopped unconditionally rather than only when a run reports
// itself in progress. "Not executing" is not the same as "no goroutine left":
// the walk clears the flag as its last act, so a stop pressed in that instant
// would otherwise return while the goroutine it is supposed to have joined is
// still alive. Stop with nothing running waits, does not cancel and leaves the
// generation alone, so the unconditional call costs a lock and a satisfied
// WaitGroup.
//
// It emits nothing. The run announces its own ending from finishRun, which is
// the side that knows whether it was stopped or finished by itself; emitting
// here as well reported every stop twice.
//
// A closed runner is declined outright, for the reason startFlow declines one:
// setExecuting writes twenty-odd tray menu items, and doing that once
// ServiceShutdown has returned means writing them while the main thread destroys
// the menu they belong to - systray.destroy() runs immediately after the
// shutdown hook. A stop click already in flight when the user quits is exactly
// how that happens: it races ServiceShutdown for execMutex and may well lose.
// There is nothing left for it to do anyway - the run it meant to stop was
// cancelled and waited for by Runner.Close.
//
// Declining, not Close. This is the user pressing stop, and Close would leave
// the runner refusing their next macro.
func (a *App) StopExecution() {
	a.execMutex.Lock()
	defer a.execMutex.Unlock()

	a.stopLocked()
}

// stopIfRunning is StopExecution for the toggle: it stops a run only if there
// is one, and reports whether it did.
//
// The check and the stop are one critical section, which is the whole reason it
// is not two calls at the call site. Between an unlocked GetIsExecuting and a
// StopExecution that takes the lock for itself, a run could end by itself and
// the next one begin - and the toggle would stop a macro the user had just
// started rather than the one they meant.
//
// A closed runner cannot reach stopLocked's refusal from here, and that is
// worth stating because the two functions read as though it could. ServiceShutdown
// clears the executing flag under execMutex after Runner.Close, so a stop that
// lands after shutdown finds the app idle and returns false at the check above -
// which is what TestRunMacroIsRefusedAfterServiceShutdown asserts, and it is the
// right answer for the caller either way: there is no run left for RunMacro to
// stop, and no run for it to start.
func (a *App) stopIfRunning() bool {
	a.execMutex.Lock()
	defer a.execMutex.Unlock()

	if !a.GetIsExecuting() {
		return false
	}
	a.stopLocked()
	return true
}

// stopLocked is the body of StopExecution. Callers must hold execMutex.
//
// Split out so the toggle can decide and stop under one acquisition of the
// lock; the reasoning for everything it does is on StopExecution above.
//
// Its log lines name no function, and that is deliberate now that there are two
// callers. They used to say "StopExecution called ...", which was true when
// StopExecution was the only way in; a hotkey press that toggles a run off comes
// through stopIfRunning, and a log line naming a function the user did not
// invoke sends the next person reading it looking for a frontend Stop click that
// never happened.
func (a *App) stopLocked() {
	if a.runner.Closed() {
		log.Println("A stop was requested while the app is shutting down")
		return
	}

	wasExecuting := a.GetIsExecuting()

	a.runner.Stop()
	a.setExecuting(false)

	if !wasExecuting {
		log.Println("A stop was requested but no execution is in progress")
		return
	}
	log.Println("Execution has been stopped by the user")
}

// setExecuting updates the execution state.
//
// The flag is atomic rather than guarded by execMutex, and that is load-bearing
// rather than a micro-optimisation: the walk goroutine clears it as it exits,
// and StopExecution waits for that goroutine while holding execMutex. If this
// took the lock, those two would deadlock on each other.
//
// The tray update is safe from any goroutine in the sense that matters here -
// the tray only touches its own menu items and never calls back into the
// execution engine, so there is no lock cycle. It is not synchronised against
// refreshMacros, which writes other fields of the same Wails menu items from
// whichever goroutine saved a project; that race predates the interpreter and
// lives in Wails' own structs rather than in anything this package owns, but it
// is not something this comment should be read as covering.
func (a *App) setExecuting(state bool) {
	a.isExecuting.Store(state)
	log.Printf("Execution state set to: %v", state)
	a.tray.setExecuting(state)
}

// GetIsExecuting returns the current execution state.
func (a *App) GetIsExecuting() bool {
	return a.isExecuting.Load()
}
