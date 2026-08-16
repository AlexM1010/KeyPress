// execution.go
//
// Flow execution engine: turns a flowchart into a dependency graph, drives the
// task queue from it and tracks the overall execution state.

package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
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
func (a *App) RunMacro(id string) error {
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
func (a *App) startFlow(flowData FlowData) error {
	a.execMutex.Lock()
	defer a.execMutex.Unlock()

	if a.isExecuting {
		log.Println("startFlow called but execution is already in progress")
		return errors.New("execution already in progress")
	}
	a.setExecutingLocked(true)
	log.Println("Execution started")
	defer func() {
		if r := recover(); r != nil {
			// execMutex is still held here (this defer runs before the
			// deferred Unlock), so use the locked variant.
			a.setExecutingLocked(false)
			log.Printf("Recovered from panic in startFlow: %v", r)
			a.emitEvent("execution-error", fmt.Sprintf("panic: %v", r))
		}
	}()

	// Validate flowchart
	if len(flowData.Nodes) == 0 {
		a.setExecutingLocked(false)
		log.Println("Flowchart validation failed: no nodes found")
		return errors.New("flowchart must contain at least one node")
	}

	// Build node map for easy access
	allNodes := make(map[string]Node)
	for _, node := range flowData.Nodes {
		allNodes[node.ID] = node
	}

	// The run is defined by the Start node, so locate it before anything is
	// built from the graph.
	startNode, err := a.findStartNode(flowData.Nodes)
	if err != nil {
		a.setExecutingLocked(false)
		log.Printf("startFlow failed: %v", err)
		return err
	}

	// Adjacency over every edge, used only to work out what the run can
	// actually get to.
	allEdges := make(map[string][]string)
	for _, edge := range flowData.Edges {
		allEdges[edge.Source] = append(allEdges[edge.Source], edge.Target)
	}

	// Only nodes reachable from the Start node take part in the run.
	//
	// The engine drives itself forwards: a node is enqueued when one of its
	// prerequisites completes. A node the Start node cannot reach is therefore
	// never enqueued, so counting it towards completion would mean the run can
	// never finish and sits there reporting itself as still in progress.
	// Anything unreachable is left out of the graph entirely instead, and the
	// user is warned about it below.
	reachable := reachableFrom(startNode.ID, allEdges, allNodes)

	nodeMap := make(map[string]Node, len(reachable))
	for id := range reachable {
		nodeMap[id] = allNodes[id]
	}

	// Build dependencies, keeping only the edges between two reachable nodes.
	//
	// Pruning here is what makes a prerequisite that lives on a dead branch
	// count as satisfied: canEnqueue derives a node's prerequisites from this
	// map, so an incoming edge from a node that will never run simply is not
	// one of them, and the live half of the flow proceeds. (An edge whose
	// source is reachable always has a reachable target - that is how the walk
	// found it - so this only ever drops edges coming out of dead branches.)
	dependencies := make(map[string][]string)
	for _, edge := range flowData.Edges {
		if reachable[edge.Source] && reachable[edge.Target] {
			dependencies[edge.Source] = append(dependencies[edge.Source], edge.Target)
		}
	}

	// Publish the graph under graphMux; from here on it is read-only for the
	// duration of the run.
	a.setGraph(nodeMap, dependencies)

	// Initialize completed map
	a.completedMux.Lock()
	a.completed = make(map[string]bool)
	a.completedMux.Unlock()

	// Discard completions left over from a previous run. Safe here: either
	// the previous run finished on its own or StopExecution waited for the
	// workers, so nothing is sending on notifyCh right now.
	a.drainNotifications()

	// Tell the user which wired-up nodes are being left out, so a branch that
	// silently does nothing is visible rather than mysterious.
	a.warnAboutSkippedNodes(flowData, reachable)

	// Bring the worker pool back up if a previous run stopped it. This is a
	// no-op while the pool is already running.
	a.taskQueue.Start(unlimitedWorkers)

	// Capture this run's generation before enqueueing anything: everything this
	// run submits is stamped with the generation it belongs to, and
	// handleCompletions watches that one rather than whatever generation the
	// queue happens to be on later. Taken together in one call so the token and
	// the context cannot describe different generations - see TaskQueue.Current.
	execGen, execCtx := a.taskQueue.Current()

	task := Task{
		ID:   startNode.ID,
		Type: startNode.Type,
		Data: startNode.Data,
	}
	a.taskQueue.Enqueue(execGen, task)

	// Start a goroutine to handle task completions. It starts with one task in
	// flight: the Start node just enqueued above.
	go a.handleCompletions(execCtx, execGen)

	return nil
}

// setGraph installs the node and dependency maps for a new run.
func (a *App) setGraph(nodeMap map[string]Node, dependencies map[string][]string) {
	a.graphMux.Lock()
	defer a.graphMux.Unlock()
	a.nodeMap = nodeMap
	a.dependencies = dependencies
}

// node looks up a node in the current graph.
func (a *App) node(nodeID string) (Node, bool) {
	a.graphMux.RLock()
	defer a.graphMux.RUnlock()
	node, exists := a.nodeMap[nodeID]
	return node, exists
}

// nodeCount reports how many nodes the current graph has.
func (a *App) nodeCount() int {
	a.graphMux.RLock()
	defer a.graphMux.RUnlock()
	return len(a.nodeMap)
}

// dependentsOf returns the IDs of the nodes that run after nodeID. The result
// is a copy, so callers can use it without holding graphMux.
func (a *App) dependentsOf(nodeID string) []string {
	a.graphMux.RLock()
	defer a.graphMux.RUnlock()
	return append([]string(nil), a.dependencies[nodeID]...)
}

// prerequisitesOf returns the IDs of the nodes nodeID depends on.
func (a *App) prerequisitesOf(nodeID string) []string {
	a.graphMux.RLock()
	defer a.graphMux.RUnlock()
	var deps []string
	for source, targets := range a.dependencies {
		for _, target := range targets {
			if target == nodeID {
				deps = append(deps, source)
			}
		}
	}
	return deps
}

// drainNotifications empties notifyCh without blocking.
func (a *App) drainNotifications() {
	for {
		select {
		case taskID := <-a.notifyCh:
			log.Printf("Discarding stale completion notification for task %s", taskID)
		default:
			return
		}
	}
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

// reachableFrom walks the flow forwards from startID and returns the set of
// node IDs execution can get to. Edges pointing at IDs that are not in nodes
// are ignored, and a node is only ever expanded once, so a cycle terminates
// the walk instead of spinning in it.
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

// notifyTaskCompletion is called by TaskQueue workers when a task is completed.
//
// The send blocks rather than dropping the notification: a dropped completion
// would leave every dependent node permanently unqueued and hang the macro.
// ctx is the worker's generation context, so a stopped queue still unblocks
// the send instead of wedging the worker (and with it TaskQueue.Stop).
func (a *App) notifyTaskCompletion(ctx context.Context, taskID string) {
	select {
	case a.notifyCh <- taskID:
		log.Printf("Task %s completion notified", taskID)
	case <-ctx.Done():
		log.Printf("Execution canceled. Dropping completion of task %s", taskID)
	}
}

// handleCompletions listens for completed tasks and enqueues dependent tasks.
// ctx and gen are the task queue context and generation of the run it was
// started for, captured together by startFlow. Every task this goroutine
// submits carries gen, so work belonging to a run that has since been stopped
// is refused by the queue rather than executed by whatever run came next.
//
// It also detects a deadlocked run. The engine only ever enqueues work in
// reaction to a completion, so once nothing is in flight - nothing queued,
// nothing being executed - no further completion can arrive and no further
// task can be enqueued. If the run has not finished at that point it never
// will, and that is a real stall: a cycle, or a node whose prerequisites can
// no longer all be satisfied.
//
// That is deliberately not a timer. Elapsed quiet time cannot tell a wedged
// run from a Delay node that was asked to sleep for half an hour, and the
// timer this replaces would abort the latter. The in-flight count is exact:
// it reports the deadlock the moment it becomes one, and it never interrupts
// a task that is simply taking its time.
//
// inFlight is owned by this goroutine alone - every enqueue for the run
// happens here, apart from the Start node that StartExecution enqueues just
// before starting this goroutine, which is what it counts to begin with - so
// it needs no lock of its own.
func (a *App) handleCompletions(ctx context.Context, gen uint64) {
	inFlight := 1

	for {
		select {
		case taskID := <-a.notifyCh:
			// Both cases of this select can be ready at once and Go picks one
			// at random, so re-check cancellation before acting on the
			// completion - the same guard, for the same reason, as the one in
			// TaskQueue.worker.
			//
			// It is load-bearing rather than merely tidy. This branch runs its
			// whole body without consulting ctx again, and this goroutine is
			// not tracked by the queue's WaitGroup, so Stop does not wait for
			// it: a run that was stopped while one of its completions was in
			// flight would otherwise carry on keeping books for a run that is
			// over - decrementing inFlight, marking nodes completed, and
			// reporting execution-completed for a macro the user stopped.
			//
			// It is not what keeps a stopped run's work from executing. The
			// generation token does that, at the queue itself, and covers the
			// case this cannot: cancellation observed here still leaves a
			// window before the Enqueue below. See TaskQueue.Enqueue.
			if ctx.Err() != nil {
				a.setExecuting(false)
				log.Printf("Execution stopped; discarding completion of task %s", taskID)
				a.emitEvent("execution-stopped", nil)
				return
			}

			// The completion count is compared against the size of this run's
			// graph, so only count tasks that belong to it. Nothing outside
			// the graph is ever enqueued; this just keeps a stray completion
			// from an earlier run from inflating the count and declaring the
			// current one finished early.
			if _, partOfRun := a.node(taskID); !partOfRun {
				log.Printf("Ignoring completion of task %s: not part of the current run", taskID)
				continue
			}
			inFlight--

			a.completedMux.Lock()
			a.completed[taskID] = true
			completedCount := len(a.completed)
			a.completedMux.Unlock()
			log.Printf("Task %s marked as completed", taskID)

			// Find dependent tasks
			dependents := a.dependentsOf(taskID)
			for _, depID := range dependents {
				if a.canEnqueue(depID) {
					node, exists := a.node(depID)
					if !exists {
						log.Printf("Node ID %s not found in nodeMap", depID)
						continue
					}
					task := Task{
						ID:   node.ID,
						Type: node.Type,
						Data: node.Data,
					}
					a.taskQueue.Enqueue(gen, task)
					inFlight++
				}
			}

			// Check if all tasks are completed. Compared with >= rather than
			// ==: an exact match is a single point the run has to hit, and
			// missing it costs a hang.
			if completedCount >= a.nodeCount() {
				a.setExecuting(false)
				a.emitEvent("execution-completed", nil)
				log.Println("All tasks completed. Execution finished.")
				return
			}

			// Nothing left running and nothing left to start, yet the run is
			// not finished: it can never make progress again.
			if inFlight == 0 {
				a.reportStall()
				return
			}
		case <-ctx.Done():
			a.setExecuting(false)
			log.Println("Execution stopped due to task queue cancellation")
			a.emitEvent("execution-stopped", nil)
			return
		}
	}
}

// reportStall ends a run that can no longer make progress and tells the user
// which nodes were left behind.
//
// The queue is deliberately left running: nothing is in flight, so there is
// nothing to shut down, and the next run reuses the same worker generation
// exactly as it does after a run that finished normally.
func (a *App) reportStall() {
	a.setExecuting(false)

	stuck := a.pendingNodeIDs()
	log.Printf("Execution cannot continue: %d node(s) never became runnable: %s",
		len(stuck), strings.Join(stuck, ", "))
	// IDs rather than a sentence, for the same reason as the skipped-node
	// warning: the frontend owns how a node is named on screen.
	a.emitEvent("execution-stalled", stuck)
}

// pendingNodeIDs lists the nodes of the current run that have not completed,
// sorted so the same stall always reports the same way.
func (a *App) pendingNodeIDs() []string {
	// Snapshot the graph and release graphMux before taking completedMux: the
	// two locks are never held at the same time.
	a.graphMux.RLock()
	ids := make([]string, 0, len(a.nodeMap))
	for id := range a.nodeMap {
		ids = append(ids, id)
	}
	a.graphMux.RUnlock()

	a.completedMux.Lock()
	pending := make([]string, 0, len(ids))
	for _, id := range ids {
		if !a.completed[id] {
			pending = append(pending, id)
		}
	}
	a.completedMux.Unlock()

	sort.Strings(pending)
	return pending
}

// canEnqueue checks if all dependencies of a node are met.
func (a *App) canEnqueue(nodeID string) bool {
	// Read the graph first and release graphMux before taking completedMux,
	// so the two locks are never held at the same time.
	deps := a.prerequisitesOf(nodeID)

	a.completedMux.Lock()
	defer a.completedMux.Unlock()
	for _, dep := range deps {
		if !a.completed[dep] {
			return false
		}
	}
	return true
}

// StopExecution stops the ongoing execution.
func (a *App) StopExecution() {
	a.execMutex.Lock()
	defer a.execMutex.Unlock()

	if !a.isExecuting {
		log.Println("StopExecution called but no execution is in progress")
		return
	}

	a.taskQueue.Stop()
	// execMutex is held for the whole function, so use the locked variant.
	a.setExecutingLocked(false)
	a.emitEvent("execution-stopped", nil)
	log.Println("Execution has been stopped by the user")
}

// setExecuting updates the execution state.
func (a *App) setExecuting(state bool) {
	a.execMutex.Lock()
	a.isExecuting = state
	a.execMutex.Unlock()
	log.Printf("Execution state set to: %v", state)
	a.tray.setExecuting(state)
}

// setExecutingLocked updates the execution state and must only be called with
// execMutex already held. sync.Mutex is not reentrant, so callers that already
// hold the lock must use this instead of setExecuting.
func (a *App) setExecutingLocked(state bool) {
	a.isExecuting = state
	log.Printf("Execution state set to: %v", state)
	// Safe under execMutex: the tray only touches its own menu items and never
	// calls back into the execution engine.
	a.tray.setExecuting(state)
}

// GetIsExecuting returns the current execution state.
func (a *App) GetIsExecuting() bool {
	a.execMutex.Lock()
	defer a.execMutex.Unlock()
	return a.isExecuting
}
