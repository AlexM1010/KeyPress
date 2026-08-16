// execution_test.go
//
// Tests for the flow execution engine: the checks a run is refused by, the
// lints it reports before it starts, the walk itself, and the two ways a run
// can end - finished, or stopped.
//
// Three things shape everything here.
//
// Every node in a test graph is a StartNode. Running any other node type reaches
// robotgo, which moves the real mouse and presses real keys on whatever machine
// the suite happens to be on; a StartNode logs, emits an event and returns.
// Since the walk only ever looks at a node's id and its edges, a graph of
// Start nodes exercises the interpreter exactly as a real macro does. findStartNode
// takes the first one in the list, so the first id in a test graph is where the
// run begins and the rest are ordinary nodes that happen to be inert.
//
// Events are observed through the log. a.wails is nil in a test, so emitEvent
// drops the event and logs that it did - which makes the log the only place an
// event is visible without standing up a real Wails application. captureLogs
// and emittedEvent below wrap that so the coupling lives in one place.
//
// The walk itself needs none of that. A run is a value with a step method, so a
// test builds one, drives it, and reads the order it ran nodes in straight off
// the return values - no goroutine, no polling, and no events to decode. The
// harness above is for the tests that genuinely need the whole startFlow path.

package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// testTimeout bounds every wait in this file. It is generous on purpose: the
// point is that a wedged engine fails the test with a message rather than
// hanging the suite, not that the engine is fast.
const testTimeout = 5 * time.Second

// waitFor polls cond until it holds, and fails the test if it never does.
//
// Polling rather than sleeping a fixed amount: a bare sleep is either too short
// (flaky) or too long (slow), and neither says what it was waiting for when it
// goes wrong.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", timeout, what)
		}
		time.Sleep(time.Millisecond)
	}
}

// logCapture collects what the package logs during one test. Writes come from
// the walk goroutine as well as the test's own, so it is guarded.
type logCapture struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *logCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Write(p)
}

func (c *logCapture) contains(substring string) bool {
	return c.count(substring) > 0
}

func (c *logCapture) count(substring string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Count(c.buf.String(), substring)
}

func (c *logCapture) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

// captureLogs redirects the standard logger into a buffer for the duration of a
// test and puts it back afterwards.
func captureLogs(t *testing.T) *logCapture {
	t.Helper()

	capture := &logCapture{}
	previous := log.Writer()
	log.SetOutput(capture)
	t.Cleanup(func() { log.SetOutput(previous) })
	return capture
}

// emittedEvent is the log line emitEvent writes when there is no application to
// emit to, which is the case in every test. Matching on it is how a test sees
// an event at all.
func emittedEvent(name string) string {
	return fmt.Sprintf("Dropping event %q", name)
}

// newTestApp builds an App with no Wails application, no window and no tray -
// the state every test runs in - and stops any run it left going afterwards, so
// a finished test leaves no goroutine behind.
//
// "Leaves no goroutine behind" is asserted, not assumed: verifyNoLeaks is
// registered first so that, Cleanup being last-in-first-out, it runs after the
// Stop below and fails the test if anything survived it. See main_test.go.
func newTestApp(t *testing.T) *App {
	t.Helper()

	verifyNoLeaks(t)

	app := NewApp()
	t.Cleanup(app.runner.Stop)
	return app
}

// newBubbleTestApp is newTestApp for a test running inside a synctest bubble.
// It takes the bubble's *testing.T, so the Stop below is registered on it and
// runs while the bubble is still up.
//
// Two things differ from newTestApp, and both are forced by how synctest
// decides a goroutine is durably blocked.
//
// The App has to be built *inside* the bubble. NewApp creates the runner's
// context, and only channels created within the bubble count towards durable
// blocking; with an App built outside it, a select on them is an ordinary block
// the fake clock cannot see, and synctest.Wait never returns. NewApp starts no
// goroutines, so building it here costs nothing.
//
// And the leak check stays on the *outer* t, which is why this does not call
// verifyNoLeaks and every caller does. goleak run from inside the bubble sees
// synctest's own machinery - the goroutine parked in synctest.Run and the one
// running the bubbled test - and reports them as leaks. On the outer t it runs
// after the bubble has been torn down, which is the state worth asserting
// about anyway.
func newBubbleTestApp(t *testing.T) *App {
	t.Helper()

	app := NewApp()
	t.Cleanup(app.runner.Stop)
	return app
}

// inertFlow builds a flowchart from node ids and the edges between them. Every
// node is a StartNode; see the file comment for why.
//
// Each edge leaves its source by a handle of its own - out-1, out-2, ... in the
// order given - because an output handle takes exactly one edge and
// validateOutputHandles refuses a graph where it does not. A test graph that
// fans out therefore looks like what a Sequence node really draws.
func inertFlow(nodeIDs []string, edges [][2]string) FlowData {
	flow := FlowData{
		Nodes: make([]Node, 0, len(nodeIDs)),
		Edges: make([]Edge, 0, len(edges)),
	}
	for _, id := range nodeIDs {
		flow.Nodes = append(flow.Nodes, Node{
			ID:       id,
			Type:     "StartNode",
			Data:     map[string]interface{}{},
			Position: map[string]float64{"x": 0, "y": 0},
		})
	}
	outputs := make(map[string]int)
	for i, edge := range edges {
		outputs[edge[0]]++
		flow.Edges = append(flow.Edges, Edge{
			ID:           fmt.Sprintf("edge-%d", i),
			Source:       edge[0],
			Target:       edge[1],
			SourceHandle: fmt.Sprintf("out-%d", outputs[edge[0]]),
		})
	}
	return flow
}

// loopFlow is the loop the editor draws, with the Loop Start node's payload as
// given: a Start node into a Loop Start, one node of body, the paired Loop End
// whose `back` edge closes the cycle, and a node on the `done` output so that
// leaving the loop is visible in the log as well as entering it.
//
// The back edge is here because the *editor* draws it, which is the whole of
// the pair: e4 is an ordinary edge on an ordinary handle, and nothing in the
// walk knows that "end" belongs to "loop". Neither node carries a partner id,
// for the same reason - the backend would ignore one.
//
// Every node but the loop's two halves is a StartNode, for the reason in the
// file comment. Both halves are safe to run for the same reason a Sequence node
// is: one reads a payload, counts and yields, the other does nothing at all.
func loopFlow(data map[string]interface{}) FlowData {
	node := func(id, nodeType string, payload map[string]interface{}) Node {
		return Node{ID: id, Type: nodeType, Data: payload, Position: map[string]float64{"x": 0, "y": 0}}
	}
	edge := func(id, source, target, handle string) Edge {
		return Edge{ID: id, Source: source, Target: target, SourceHandle: handle}
	}

	return FlowData{
		Nodes: []Node{
			node("start", "StartNode", map[string]interface{}{}),
			node("loop", loopStartNodeType, data),
			node("body", "StartNode", map[string]interface{}{}),
			node("end", loopEndNodeType, map[string]interface{}{}),
			node("after", "StartNode", map[string]interface{}{}),
		},
		Edges: []Edge{
			edge("e1", "start", "loop", "right"),
			edge("e2", "loop", "body", loopBodyHandle),
			edge("e3", "body", "end", "right"),
			edge("e4", "end", "loop", loopBackHandle),
			edge("e5", "loop", "after", loopDoneHandle),
		},
	}
}

// withoutEdge is a flowchart with one edge deleted, by id.
//
// It exists for the loop, where half a pair is a real state a file can be in: a
// Loop End whose `back` edge has gone with its partner, or a Loop Start whose
// `body` was never wired. The editor refuses to leave either behind, and the
// backend may not depend on it having succeeded - so these tests take a macro
// the editor really would draw and delete the one edge.
func withoutEdge(flow FlowData, edgeID string) FlowData {
	kept := make([]Edge, 0, len(flow.Edges))
	for _, edge := range flow.Edges {
		if edge.ID != edgeID {
			kept = append(kept, edge)
		}
	}
	flow.Edges = kept
	return flow
}

// nodesByID is the node map shape reachableFrom wants, built from bare ids.
func nodesByID(ids ...string) map[string]Node {
	nodes := make(map[string]Node, len(ids))
	for _, id := range ids {
		nodes[id] = Node{ID: id, Type: "StartNode", Data: map[string]interface{}{}}
	}
	return nodes
}

// inertExec is a run's executor for a test that cares about where the token
// went rather than what the nodes did: every node leaves by its only output,
// and nothing touches the machine the suite is on.
func inertExec(_ context.Context, _ Task, _ *runState) (string, error) {
	return "", nil
}

// walkAll drives a run to its end and reports the nodes it ran, in order and
// with repeats. This is the whole harness a walk test needs.
func walkAll(ctx context.Context, r *run) []string {
	visits := make([]string, 0, 8)
	for {
		// Nothing bounds this but the graph, now that the iteration budget is
		// gone: a run ends when the token runs out of edges or when the context
		// is cancelled. Every graph driven through here has to be one that ends
		// - which for a loop means a count or a condition that stops it - or
		// the test hangs rather than failing.
		id, ok := r.step(ctx)
		if !ok {
			return visits
		}
		visits = append(visits, id)
	}
}

// =============================================== reachableFrom ===============================================

func TestReachableFromWalksTheFlowForwards(t *testing.T) {
	nodes := nodesByID("start", "a", "b", "c")
	edges := map[string][]string{
		"start": {"a", "b"},
		"a":     {"c"},
	}

	reachable := reachableFrom("start", edges, nodes)

	for _, id := range []string{"start", "a", "b", "c"} {
		if !reachable[id] {
			t.Errorf("node %q is not reachable, want it reached", id)
		}
	}
	if len(reachable) != 4 {
		t.Errorf("reached %d nodes, want 4: %v", len(reachable), reachable)
	}
}

func TestReachableFromLeavesOutWhatStartCannotGetTo(t *testing.T) {
	nodes := nodesByID("start", "live", "island-a", "island-b")
	edges := map[string][]string{
		"start":    {"live"},
		"island-a": {"island-b"},
	}

	reachable := reachableFrom("start", edges, nodes)

	if !reachable["start"] || !reachable["live"] {
		t.Errorf("the live branch is not reachable: %v", reachable)
	}
	if reachable["island-a"] || reachable["island-b"] {
		t.Errorf("an unreachable branch was reached: %v", reachable)
	}
}

// A node is only ever expanded once, so a graph that loops back on itself
// terminates the walk instead of spinning in it. This test hangs rather than
// fails if that ever regresses, so it runs behind its own deadline.
func TestReachableFromTerminatesOnACycle(t *testing.T) {
	nodes := nodesByID("start", "a", "b")
	edges := map[string][]string{
		"start": {"a"},
		"a":     {"b"},
		"b":     {"a", "start"},
	}

	done := make(chan map[string]bool, 1)
	go func() { done <- reachableFrom("start", edges, nodes) }()

	select {
	case reachable := <-done:
		if len(reachable) != 3 {
			t.Errorf("reached %v, want all three nodes", reachable)
		}
	case <-time.After(testTimeout):
		t.Fatalf("reachableFrom did not return within %s: it is spinning in the cycle", testTimeout)
	}
}

// An edge can outlive the node it points at in a hand-edited file, and a run
// must not invent a node from one.
func TestReachableFromIgnoresEdgesToNodesThatDoNotExist(t *testing.T) {
	nodes := nodesByID("start", "a")
	edges := map[string][]string{
		"start": {"a", "ghost"},
		"a":     {"another-ghost"},
	}

	reachable := reachableFrom("start", edges, nodes)

	if reachable["ghost"] || reachable["another-ghost"] {
		t.Errorf("a node that is not in the flowchart was reached: %v", reachable)
	}
	if len(reachable) != 2 {
		t.Errorf("reached %d nodes, want just start and a: %v", len(reachable), reachable)
	}
}

func TestReachableFromAStartIDThatIsNotANodeReachesNothing(t *testing.T) {
	nodes := nodesByID("a", "b")
	edges := map[string][]string{"missing": {"a"}}

	if reachable := reachableFrom("missing", edges, nodes); len(reachable) != 0 {
		t.Errorf("reachableFrom = %v, want nothing", reachable)
	}
}

// =============================================== the shared reachability fixtures ===============================================

// reachabilityFixtureDir holds the graphs this suite shares with the frontend.
// See the README in it: the same files are read by
// frontend/src/lib/utils/nodeLabels.parity.test.ts, and dropping a new one in
// there extends both suites without either test being edited.
const reachabilityFixtureDir = "testdata/reachability"

// reachabilityFixture is one graph and the answer both implementations of the
// walk have to give for it.
type reachabilityFixture struct {
	// Description is the claim the fixture makes, printed when it fails so the
	// failure names the rule that broke rather than just a file.
	Description string `json:"description"`

	// Flow is the graph in exactly the shape a saved macro has on disk, so a
	// fixture is a real flowchart rather than a test-only encoding of one.
	Flow FlowData `json:"flow"`

	// Reachable is every node id the run can get to, compared as a set.
	Reachable []string `json:"reachable"`
}

// Reachability is written twice - here, to tell the user which wired-up nodes
// the run will never get to, and in nodeLabels.ts, to decide how the status
// panel names and orders them. This is the Go half of the check that the two
// still agree; the TypeScript half reads the same files. Without it, a status
// panel that describes a run which did not happen is a silent regression.
//
// Fixtures are discovered by globbing rather than listed, so the two suites can
// never cover different sets.
func TestReachableFromAgreesWithTheSharedFixtures(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join(reachabilityFixtureDir, "*.json"))
	if err != nil {
		t.Fatalf("globbing %s: %v", reachabilityFixtureDir, err)
	}
	if len(paths) == 0 {
		t.Fatalf("no fixtures in %s: the frontend parity test reads the same directory, so both suites are covering nothing", reachabilityFixtureDir)
	}

	app := newTestApp(t)
	for _, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), ".json")
		t.Run(name, func(t *testing.T) {
			fixture := loadReachabilityFixture(t, path)

			nodes := nodesByIDFrom(fixture.Flow)
			edges := adjacency(fixture.Flow)

			// startFlow refuses a graph with no Start node before it ever walks
			// it, so there is no id to start from and nothing is reachable -
			// which is what the frontend computes for the same graph, and what
			// such a fixture must therefore expect.
			startID := ""
			if start, err := app.findStartNode(fixture.Flow.Nodes); err == nil {
				startID = start.ID
			}

			// Behind a deadline: a fixture with a cycle proves the walk
			// terminates, and a proof that hangs the suite is no proof.
			done := make(chan map[string]bool, 1)
			go func() { done <- reachableFrom(startID, edges, nodes) }()

			var reachable map[string]bool
			select {
			case reachable = <-done:
			case <-time.After(testTimeout):
				t.Fatalf("reachableFrom did not return within %s on %s: it is spinning in the graph", testTimeout, name)
			}

			got := make([]string, 0, len(reachable))
			for id := range reachable {
				got = append(got, id)
			}
			sort.Strings(got)

			// Built with make rather than appended onto a nil slice: an empty
			// fixture is a legitimate answer, and reflect.DeepEqual tells a nil
			// slice apart from an empty one.
			want := make([]string, 0, len(fixture.Reachable))
			want = append(want, fixture.Reachable...)
			sort.Strings(want)

			if !reflect.DeepEqual(got, want) {
				t.Errorf("reachable = %v, want %v\nfixture %s: %s", got, want, filepath.Base(path), fixture.Description)
			}
		})
	}
}

// loadReachabilityFixture reads one fixture, rejecting anything the file cannot
// mean: an unknown field is a typo in a hand-written fixture, and a fixture
// naming a node the graph does not have would quietly assert nothing here while
// asserting something in the frontend.
func loadReachabilityFixture(t *testing.T, path string) reachabilityFixture {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", path, err)
	}

	var fixture reachabilityFixture
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fixture); err != nil {
		t.Fatalf("parsing fixture %s: %v", path, err)
	}

	if fixture.Description == "" {
		t.Fatalf("fixture %s has no description: it is what a failure prints", path)
	}
	if len(fixture.Flow.Nodes) == 0 {
		t.Fatalf("fixture %s has no nodes", path)
	}

	ids := make(map[string]bool, len(fixture.Flow.Nodes))
	for _, node := range fixture.Flow.Nodes {
		ids[node.ID] = true
	}
	for _, id := range fixture.Reachable {
		if !ids[id] {
			t.Fatalf("fixture %s expects node %q to be reachable, but the graph has no such node", path, id)
		}
	}
	return fixture
}

// =============================================== the shared walk fixtures ===============================================

// walkFixtureDir holds the graphs this suite shares with the frontend for the
// walk itself: what runs, in what order, and how a loop ends. See the README in
// it.
const walkFixtureDir = "testdata/walk"

// walkFixture is one graph and the run the interpreter has to make of it.
type walkFixture struct {
	// Description is the claim the fixture makes, printed when it fails.
	Description string `json:"description"`

	// Flow is the graph in exactly the shape a saved macro has on disk.
	Flow FlowData `json:"flow"`

	// Visits is the node ids the token runs, in order and with repeats. A node
	// on two paths appears twice.
	Visits []string `json:"visits"`

	// Multipath is what the diamond lint reports, in flowchart order.
	Multipath []string `json:"multipath"`

	// Outputs is the handle a node leaves by, for the nodes that pick one.
	// Anything unlisted leaves by "", the only output. A LoopStartNode is not
	// listed here and cannot be - see the executor below - and a LoopEndNode
	// needs no entry, because "" is what it returns anyway.
	Outputs map[string]string `json:"outputs"`
}

// The walk is written twice, here and in the frontend, for the same reason
// reachability is. These fixtures are what keeps the two agreeing about order,
// about a node that runs twice, and about a loop that ends.
//
// No goroutine, no deadline and no log scraping: a run is a value, so this
// drives it directly and reads the visits off step's return.
func TestTheWalkAgreesWithTheSharedFixtures(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join(walkFixtureDir, "*.json"))
	if err != nil {
		t.Fatalf("globbing %s: %v", walkFixtureDir, err)
	}
	if len(paths) == 0 {
		t.Fatalf("no fixtures in %s: the frontend parity test reads the same directory, so both suites are covering nothing", walkFixtureDir)
	}

	app := newTestApp(t)
	for _, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), ".json")
		t.Run(name, func(t *testing.T) {
			fixture := loadWalkFixture(t, path)

			start, err := app.findStartNode(fixture.Flow.Nodes)
			if err != nil {
				t.Fatalf("fixture %s: %v", name, err)
			}

			// The fixture's own executor, so nothing in these graphs is ever
			// really run: a fixture is free to use any node type, and the
			// handle a node leaves by is the fixture's to state.
			//
			// A Loop Start is the exception, and has to be. Its handle is not
			// a property of the node that a fixture could state - it is the
			// answer to "how many times have I been here", which is the whole
			// thing these fixtures are pinning, and a fixture that stated
			// "body" would simply loop forever. So the real handler runs, with
			// the frame the walk entered on arrival. Nothing else about it
			// touches the machine: it reads a payload, counts, and yields.
			//
			// A Loop End needs no exception: it runs nothing and leaves by "",
			// which is what an unlisted node gets here anyway, and "" takes the
			// one edge the editor drew from its `back` handle. That it needs no
			// case is the point of the pair - the walk knows nothing about it.
			exec := func(ctx context.Context, task Task, state *runState) (string, error) {
				if task.Type == loopStartNodeType {
					return executeLoopStartTask(ctx, task, state, app)
				}
				return fixture.Outputs[task.ID], nil
			}

			r := newRun(fixture.Flow, start.ID, exec)
			got := walkAll(context.Background(), r)

			want := make([]string, 0, len(fixture.Visits))
			want = append(want, fixture.Visits...)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("visits = %v, want %v\nfixture %s: %s", got, want, filepath.Base(path), fixture.Description)
			}

			// Every fixture ends by running out of edges. There is no longer any
			// other way for one to end: the iteration budget is gone, so a
			// fixture with a loop in it terminates by that loop's own logic or
			// it does not terminate at all.
			if r.outcome != runFinished {
				t.Errorf("outcome = %v, want the run to have finished normally\nfixture %s: %s", r.outcome, filepath.Base(path), fixture.Description)
			}
			if len(r.frames) != 0 {
				t.Errorf("the run ended holding %d loop frame(s), want every loop it entered to have been left\nfixture %s", len(r.frames), filepath.Base(path))
			}
			// The frames' counterpart, and the second half of "the walk left
			// nothing behind": a run that ends by running out of edges ends with
			// an empty stack by definition, so what this really pins is that the
			// fixture ended that way rather than by a step giving up with nodes
			// still pending. A leak like the one loopFrame.stackDepth exists to
			// prevent shows up in `visits` first - the leaked branches all run
			// when the loop finally leaves - but it is worth being told which of
			// the two went wrong.
			if len(r.stack) != 0 {
				t.Errorf("the run ended with %d node(s) still waiting for the token, want it to have run out of edges: %v\nfixture %s", len(r.stack), r.stack, filepath.Base(path))
			}

			multipath := multipathNodes(fixture.Flow, start.ID)
			wantMultipath := make([]string, 0, len(fixture.Multipath))
			wantMultipath = append(wantMultipath, fixture.Multipath...)
			if !reflect.DeepEqual(multipath, wantMultipath) {
				t.Errorf("multipath = %v, want %v\nfixture %s: %s", multipath, wantMultipath, filepath.Base(path), fixture.Description)
			}
		})
	}
}

// loadWalkFixture reads one fixture, rejecting anything the file cannot mean.
func loadWalkFixture(t *testing.T, path string) walkFixture {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", path, err)
	}

	var fixture walkFixture
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fixture); err != nil {
		t.Fatalf("parsing fixture %s: %v", path, err)
	}

	if fixture.Description == "" {
		t.Fatalf("fixture %s has no description: it is what a failure prints", path)
	}
	if len(fixture.Flow.Nodes) == 0 {
		t.Fatalf("fixture %s has no nodes", path)
	}
	if len(fixture.Visits) == 0 {
		t.Fatalf("fixture %s expects no visits at all, which no graph with a Start node can produce", path)
	}

	ids := make(map[string]bool, len(fixture.Flow.Nodes))
	for _, node := range fixture.Flow.Nodes {
		ids[node.ID] = true
		if node.Position == nil {
			t.Fatalf("fixture %s: node %q has no position, so it is not a macro the editor could have drawn", path, node.ID)
		}
	}
	for _, id := range append(append([]string{}, fixture.Visits...), fixture.Multipath...) {
		if !ids[id] {
			t.Fatalf("fixture %s names node %q, but the graph has no such node", path, id)
		}
	}

	// Every fixture is a graph the editor would accept, or it is asserting
	// something about a file that could not exist.
	if err := validateOutputHandles(fixture.Flow); err != nil {
		t.Fatalf("fixture %s is not a runnable macro: %v", path, err)
	}
	return fixture
}

// =============================================== findStartNode ===============================================

func TestFindStartNodeReturnsTheFirstStartNode(t *testing.T) {
	app := newTestApp(t)
	nodes := []Node{
		{ID: "delay-1", Type: "DelayNode"},
		{ID: "start-1", Type: "StartNode"},
		{ID: "start-2", Type: "StartNode"},
	}

	start, err := app.findStartNode(nodes)
	if err != nil {
		t.Fatalf("findStartNode: %v", err)
	}
	if start.ID != "start-1" {
		t.Errorf("start node = %q, want %q", start.ID, "start-1")
	}
}

func TestFindStartNodeWithoutOne(t *testing.T) {
	app := newTestApp(t)

	_, err := app.findStartNode([]Node{{ID: "delay-1", Type: "DelayNode"}})
	if err == nil {
		t.Fatal("findStartNode succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "no Start node") {
		t.Errorf("error = %q, want it to mention the missing Start node", err)
	}
}

// =============================================== validateOutputHandles ===============================================

// One wire per handle is what makes fan-out order unambiguous, and a Sequence
// node is how a macro fans out. Several edges leaving *different* handles of
// one node is exactly that, and must be accepted.
func TestValidateOutputHandlesAcceptsASequenceNode(t *testing.T) {
	flow := FlowData{
		Nodes: []Node{{ID: "seq", Type: "SequenceNode"}, {ID: "a"}, {ID: "b"}},
		Edges: []Edge{
			{ID: "e1", Source: "seq", Target: "a", SourceHandle: "out-1"},
			{ID: "e2", Source: "seq", Target: "b", SourceHandle: "out-2"},
		},
	}

	if err := validateOutputHandles(flow); err != nil {
		t.Errorf("validateOutputHandles refused a Sequence node: %v", err)
	}
}

func TestValidateOutputHandlesRefusesTwoEdgesOnOneHandle(t *testing.T) {
	flow := FlowData{
		Nodes: []Node{{ID: "start", Type: "StartNode"}, {ID: "a"}, {ID: "b"}},
		Edges: []Edge{
			{ID: "e1", Source: "start", Target: "a", SourceHandle: "right"},
			{ID: "e2", Source: "start", Target: "b", SourceHandle: "right"},
		},
	}

	err := validateOutputHandles(flow)
	if err == nil {
		t.Fatal("validateOutputHandles accepted two edges on one handle")
	}
	// The message has to name both, or the user is left hunting for which wire
	// on which node.
	if !strings.Contains(err.Error(), "start") || !strings.Contains(err.Error(), "right") {
		t.Errorf("error = %q, want it to name the node and the handle", err)
	}
}

// The refusal has to happen before anything runs: half a macro typed into
// whatever the user had focused is much worse than a macro that does not start.
func TestStartFlowRefusesAFlowchartThatFansOutFromOneHandle(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)

	flow := inertFlow([]string{"start", "a", "b"}, [][2]string{{"start", "a"}, {"start", "b"}})
	// Undo inertFlow's one-handle-per-edge numbering: this is the hand-edited
	// file the check exists for.
	flow.Edges[1].SourceHandle = flow.Edges[0].SourceHandle

	err := app.startFlow(flow)
	if err == nil {
		t.Fatal("startFlow accepted two edges on one output handle")
	}
	if !strings.Contains(err.Error(), "exactly one edge") {
		t.Errorf("error = %q, want it to state the rule", err)
	}
	if app.GetIsExecuting() {
		t.Error("a refused flowchart left the app executing")
	}
	if logs.contains("Processing task") {
		t.Errorf("a node ran before the flowchart was refused:\n%s", logs)
	}
}

// =============================================== the walk ===============================================

// The load-bearing rule: "" is "the only output" and takes every outgoing edge
// whatever its handle is named. Every edge this app draws carries "right", so a
// handle-equality rule here would strand every macro in the tree at the Start
// node.
func TestFollowOnTheOnlyOutputTakesEveryEdgeWhateverItsHandle(t *testing.T) {
	flow := FlowData{
		Nodes: []Node{{ID: "seq"}, {ID: "a"}, {ID: "b"}},
		Edges: []Edge{
			{ID: "e1", Source: "seq", Target: "b", SourceHandle: "out-2"},
			{ID: "e2", Source: "seq", Target: "a", SourceHandle: "out-1"},
		},
	}
	r := newRun(flow, "seq", inertExec)

	// In handle order, not in the order the file listed them.
	if got := r.follow("seq", ""); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("follow(seq, \"\") = %v, want [a b]", got)
	}
}

func TestFollowOnANamedOutputTakesOnlyThatEdge(t *testing.T) {
	flow := FlowData{
		Nodes: []Node{{ID: "branch"}, {ID: "a"}, {ID: "b"}},
		Edges: []Edge{
			{ID: "e1", Source: "branch", Target: "a", SourceHandle: "out-1"},
			{ID: "e2", Source: "branch", Target: "b", SourceHandle: "out-2"},
		},
	}
	r := newRun(flow, "branch", inertExec)

	if got := r.follow("branch", "out-2"); !reflect.DeepEqual(got, []string{"b"}) {
		t.Errorf("follow(branch, out-2) = %v, want [b]", got)
	}
}

// An unknown output ends that path rather than panicking, and a path ending is
// how a macro finishes.
func TestAnUnknownOutputEndsThePathRatherThanPanicking(t *testing.T) {
	flow := FlowData{
		Nodes: []Node{
			{ID: "branch", Type: "StartNode"},
			{ID: "never", Type: "StartNode"},
		},
		Edges: []Edge{{ID: "e1", Source: "branch", Target: "never", SourceHandle: "out-1"}},
	}
	r := newRun(flow, "branch", func(_ context.Context, _ Task, _ *runState) (string, error) {
		return "no-such-handle", nil
	})

	got := walkAll(context.Background(), r)

	if !reflect.DeepEqual(got, []string{"branch"}) {
		t.Errorf("visits = %v, want just the node that picked the unknown output", got)
	}
	if r.outcome != runFinished {
		t.Errorf("outcome = %v, want the run to have finished normally", r.outcome)
	}
}

// An edge can outlive the node it points at in a hand-edited file, and the walk
// must not invent a node from one - the same rule reachableFrom follows, on the
// side that would really press keys.
func TestTheWalkIgnoresAnEdgeToANodeThatIsNotThere(t *testing.T) {
	flow := inertFlow([]string{"start", "real"}, [][2]string{{"start", "real"}})
	flow.Edges = append(flow.Edges, Edge{
		ID:           "dangling",
		Source:       "start",
		Target:       "ghost",
		SourceHandle: "out-2",
	})

	r := newRun(flow, "start", inertExec)
	got := walkAll(context.Background(), r)

	if !reflect.DeepEqual(got, []string{"start", "real"}) {
		t.Errorf("visits = %v, want just the nodes the flowchart has", got)
	}
	if r.outcome != runFinished {
		t.Errorf("outcome = %v, want the run to have finished normally", r.outcome)
	}
}

// Cancellation is checked the moment a handler returns, before any edge is
// followed - so a run stopped while a node was running does not go on to start
// the next one.
func TestCancellationStopsTheRunBeforeTheNextNode(t *testing.T) {
	flow := inertFlow([]string{"first", "second"}, [][2]string{{"first", "second"}})
	r := newRun(flow, "first", inertExec)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	id, ok := r.step(ctx)
	if id != "first" {
		t.Errorf("step ran %q, want the node the token was already on", id)
	}
	if ok {
		t.Error("step reported the run as continuing after cancellation")
	}
	if r.outcome != runCancelled {
		t.Errorf("outcome = %v, want the run cancelled", r.outcome)
	}
	if len(r.stack) != 0 {
		t.Errorf("the token moved on to %v after cancellation", r.stack)
	}
}

// The other half of the check: between two instantaneous nodes, in the driver,
// so a loop that never blocks still stops promptly.
func TestAWalkThatIsCancelledBeforeItStartsRunsNothing(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)
	app.setExecuting(true)

	flow := inertFlow([]string{"start", "second"}, [][2]string{{"start", "second"}})
	r := newRun(flow, "start", app.executeNode)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app.walk(ctx, r)

	if r.steps != 0 {
		t.Errorf("%d node(s) ran under a cancelled context, want none", r.steps)
	}
	if !logs.contains(emittedEvent("execution-stopped")) {
		t.Errorf("no execution-stopped event was emitted:\n%s", logs)
	}
	if logs.contains(emittedEvent("execution-completed")) {
		t.Errorf("a cancelled run reported itself completed:\n%s", logs)
	}
	if app.GetIsExecuting() {
		t.Error("a cancelled run left the app reporting itself executing")
	}
}

// A cycle with no way out is a feature now, not a stall and not a budget
// overrun: an endless loop plus the toggle that stops it is the macro this app
// is for. Nothing bounds the number of nodes it runs, and what ends it is
// cancellation - which the walk must notice between two instantaneous nodes,
// because there is nothing else in this graph to notice it.
func TestAnEndlessCycleRunsUntilItIsCancelled(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)
	app.setExecuting(true)

	flow := inertFlow([]string{"start", "a", "b"}, [][2]string{{"start", "a"}, {"a", "b"}, {"b", "a"}})

	// Cancelled from inside the walk, on the twentieth node, which is the only
	// way this graph ever ends. Counting rather than timing: "however many nodes
	// it got through in 50ms" would be a different number on every machine.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	steps := 0
	r := newRun(flow, "start", func(ctx context.Context, task Task, state *runState) (string, error) {
		steps++
		if steps == 20 {
			cancel()
		}
		return app.executeNode(ctx, task, state)
	})

	app.walk(ctx, r)

	if r.outcome != runCancelled {
		t.Errorf("outcome = %v, want the loop to have been ended by cancellation", r.outcome)
	}
	if r.steps != 20 {
		t.Errorf("the run executed %d node(s), want the twenty it was allowed before the stop", r.steps)
	}
	if logs.contains(emittedEvent("execution-completed")) {
		t.Errorf("a cancelled run reported itself completed:\n%s", logs)
	}
	// Round and round: three nodes, twenty executions.
	if got := logs.count("Processing task a"); got < 2 {
		t.Errorf("node a ran %d time(s), want the loop to have gone round:\n%s", got, logs)
	}
	if app.GetIsExecuting() {
		t.Error("a cancelled run left the app reporting itself executing")
	}
}

// The two cycles the loop node does not govern, and the wall-clock bound that
// does. Both shapes reach no declared yield point at all - the first has no Loop
// Start in it, and the second's frame is popped every time it leaves by `done`,
// so the next arrival has no previous iteration to measure a yield from - and
// both used to run flat out, pinning a core and filling the OS input queue
// faster than the target application drains it.
//
// On the fake clock, where "the walk gave up time nobody asked it to" is exact
// arithmetic rather than a tolerance. Every node here costs nodeStep of fake
// time, deliberately less than walkYield so that a step never counts as a yield
// in its own right; what is asserted is the difference between the time the
// nodes spent and the time the run took, which is the yielding and nothing else.
func TestACycleWithNoYieldPointInItIsBoundedByTheWallClock(t *testing.T) {
	const nodeStep = walkYield / 2

	// Long enough to cross maxWalkWithoutYield at least once, and no longer:
	// every step of it is logged.
	steps := int(2*maxWalkWithoutYield/nodeStep) + 1

	cases := []struct {
		name string
		flow FlowData
	}{
		{
			// An ordinary edge drawn backwards. validateOutputHandles does not
			// refuse it and the multipath lint deliberately excludes back-edges,
			// so nothing warns the user either.
			name: "a cycle with no loop node in it",
			flow: inertFlow([]string{"start", "a", "b"}, [][2]string{{"start", "a"}, {"a", "b"}, {"b", "a"}}),
		},
		{
			// A Loop Start inside a cycle that leaves by `done` every time - a
			// count of zero here, but an unwired body or a condition that already
			// holds are the same graph.
			name: "a cycle through a loop that always leaves by done",
			flow: FlowData{
				Nodes: []Node{
					{ID: "start", Type: "StartNode", Data: map[string]interface{}{}, Position: map[string]float64{"x": 0, "y": 0}},
					{ID: "loop", Type: loopStartNodeType, Data: map[string]interface{}{"count": float64(0)}, Position: map[string]float64{"x": 0, "y": 0}},
					{ID: "body", Type: "StartNode", Data: map[string]interface{}{}, Position: map[string]float64{"x": 0, "y": 0}},
					{ID: "round", Type: "StartNode", Data: map[string]interface{}{}, Position: map[string]float64{"x": 0, "y": 0}},
				},
				Edges: []Edge{
					{ID: "e1", Source: "start", Target: "loop", SourceHandle: "right"},
					{ID: "e2", Source: "loop", Target: "body", SourceHandle: loopBodyHandle},
					{ID: "e3", Source: "loop", Target: "round", SourceHandle: loopDoneHandle},
					{ID: "e4", Source: "round", Target: "loop", SourceHandle: "right"},
				},
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			verifyNoLeaks(t)

			synctest.Test(t, func(t *testing.T) {
				app := newBubbleTestApp(t)
				app.setExecuting(true)

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()

				ran, spent := 0, time.Duration(0)
				r := newRun(testCase.flow, "start", func(ctx context.Context, task Task, state *runState) (string, error) {
					ran++
					if ran == steps {
						cancel()
					}
					if task.Type == loopStartNodeType {
						// The real handler, so that the frame really is entered
						// and popped on every turn. It spends no time: a loop
						// that leaves by `done` never reaches its yield.
						return executeLoopStartTask(ctx, task, state, app)
					}
					time.Sleep(nodeStep)
					spent += nodeStep
					return "", nil
				})

				started := time.Now()
				app.walk(ctx, r)

				yielded := time.Since(started) - spent
				if yielded < walkYield {
					t.Errorf("%d nodes ran in %v of which %v was the nodes' own: the walk yielded %v, want at least one %v turn given back",
						ran, time.Since(started), spent, yielded, walkYield)
				}
				if yielded%walkYield != 0 {
					t.Errorf("the walk gave up %v, want whole %v turns and nothing else", yielded, walkYield)
				}
				if r.outcome != runCancelled {
					t.Errorf("outcome = %v, want the cycle to have been ended by cancellation", r.outcome)
				}
			})
		})
	}
}

// The other side of the bound: a macro that blocks for real pays nothing for it.
// A step that took at least a yield's worth of wall time has already given the
// input queue the gap the yield exists to create, so the timer starts again from
// there - which is what keeps a loop with a Delay node in it, or one going round
// at minLoopIterationYield, from being charged 10ms every 500ms on top.
func TestAWalkThatBlocksForItselfIsChargedNoYield(t *testing.T) {
	verifyNoLeaks(t)

	synctest.Test(t, func(t *testing.T) {
		app := newBubbleTestApp(t)
		app.setExecuting(true)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Four times the bound's worth of macro, in steps that each block for a
		// whole yield.
		const nodeStep = walkYield
		steps := int(4 * maxWalkWithoutYield / nodeStep)

		flow := inertFlow([]string{"start", "a", "b"}, [][2]string{{"start", "a"}, {"a", "b"}, {"b", "a"}})
		ran := 0
		r := newRun(flow, "start", func(_ context.Context, _ Task, _ *runState) (string, error) {
			ran++
			if ran == steps {
				cancel()
			}
			time.Sleep(nodeStep)
			return "", nil
		})

		started := time.Now()
		app.walk(ctx, r)

		if elapsed := time.Since(started); elapsed != time.Duration(steps)*nodeStep {
			t.Errorf("%d nodes of %v took %v, want no yield added to a run that was already giving way",
				steps, nodeStep, elapsed)
		}
	})
}

// A handler that fails does not stop the run. That is what the dependency
// scheduler did - a failed task still let its dependents run - and error
// handling as an output is a later feature, so a reader who assumes this was an
// oversight should find a test saying otherwise.
func TestANodeThatFailsDoesNotStopTheRun(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)
	app.setExecuting(true)

	flow := inertFlow([]string{"start", "boom", "after"}, [][2]string{{"start", "boom"}, {"boom", "after"}})
	// Unknown types are the one failure executeTask itself is responsible for.
	flow.Nodes[1].Type = "NotANodeType"

	r := newRun(flow, "start", app.executeNode)
	app.walk(context.Background(), r)

	if r.steps != 3 {
		t.Errorf("the run executed %d node(s), want all three", r.steps)
	}
	if !logs.contains("Task boom failed") {
		t.Errorf("the failure was not logged next to the node it belongs to:\n%s", logs)
	}
	if !logs.contains(emittedEvent("execution-completed")) {
		t.Errorf("the run did not carry on past the failure to completion:\n%s", logs)
	}
}

// =============================================== run state ===============================================

// The run state lives on the run, not on the App, so that a stopped macro's
// leftovers cannot be visible to the next one - the hazard of 33797cb. There is
// nowhere it could be left behind, and this is the test that says so.
func TestRunStateDoesNotSurviveARun(t *testing.T) {
	flow := inertFlow([]string{"start"}, nil)

	first := newRun(flow, "start", inertExec)
	first.state.set("attempts", 3)
	if got, ok := first.state.value("attempts"); !ok || got != 3 {
		t.Fatalf("the run state did not keep what was put in it: %v, %v", got, ok)
	}

	second := newRun(flow, "start", inertExec)
	if got, ok := second.state.value("attempts"); ok {
		t.Errorf("the next run of the same macro can see %v left over from the last one", got)
	}
}

// The run state does not survive a run that was *stopped*, which is the half
// the constructor test above cannot show: a macro that set a variable, was
// stopped part-way and started again must see it unset. Same hazard as
// 33797cb, same suspicion.
//
// Driven through app.walk rather than startFlow because startFlow builds its
// own run around app.executeNode, and no node type stores anything without
// reaching robotgo. What is being asserted is the lifecycle either way: a run
// ends, its state goes out of scope with it, and the next run builds a fresh
// one.
func TestRunStateDoesNotSurviveAStoppedRun(t *testing.T) {
	app := newTestApp(t)
	captureLogs(t)
	app.setExecuting(true)

	flow := inertFlow([]string{"start", "second"}, [][2]string{{"start", "second"}})

	// The first run stores a value on its first node and is stopped there, so
	// it never reaches the second - exactly the shape of a macro the user
	// panic-pressed mid-way.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := newRun(flow, "start", func(_ context.Context, task Task, state *runState) (string, error) {
		state.set("attempts", float64(1))
		if task.ID == "start" {
			cancel()
		}
		return "", nil
	})
	app.walk(ctx, first)

	if first.outcome != runCancelled {
		t.Fatalf("the first run ended as %v, want it stopped part-way", first.outcome)
	}

	// The second run asks, on its first node, whether the leftovers are visible.
	app.setExecuting(true)
	var leftover any
	var sawLeftover bool
	second := newRun(flow, "start", func(_ context.Context, _ Task, state *runState) (string, error) {
		if value, ok := state.value("attempts"); ok {
			leftover, sawLeftover = value, true
		}
		return "", nil
	})
	app.walk(context.Background(), second)

	if sawLeftover {
		t.Errorf("the next run can see %v left over from the macro the user stopped", leftover)
	}
}

// The two halves of run state, through the walk that carries a value between
// them: a node stores its result, and a later Branch node's condition reads it.
//
// The producer stores through the same storeResult call the colour handler
// makes, and the branch is the real handler - so what this pins is that the run
// is what carries the value from one to the other.
func TestAValueStoredByOneNodeIsReadableByALaterBranch(t *testing.T) {
	app := newTestApp(t)
	captureLogs(t)

	flow := FlowData{
		Nodes: []Node{
			{ID: "start", Type: "StartNode", Data: map[string]interface{}{}},
			{ID: "wait", Type: "ProducerNode", Data: map[string]interface{}{"storeResultAs": "found"}},
			{ID: "branch", Type: "BranchNode", Data: map[string]interface{}{
				"variable": "found", "operator": opEquals, "value": "00ff00",
			}},
			{ID: "on-true", Type: "StartNode", Data: map[string]interface{}{}},
			{ID: "on-false", Type: "StartNode", Data: map[string]interface{}{}},
		},
		Edges: []Edge{
			{ID: "e1", Source: "start", Target: "wait", SourceHandle: "right"},
			{ID: "e2", Source: "wait", Target: "branch", SourceHandle: "right"},
			{ID: "e3", Source: "branch", Target: "on-true", SourceHandle: branchTrue},
			{ID: "e4", Source: "branch", Target: "on-false", SourceHandle: branchFalse},
		},
	}

	exec := func(ctx context.Context, task Task, state *runState) (string, error) {
		if task.Type == "ProducerNode" {
			storeResult(state, task.Data, "00ff00")
			return "", nil
		}
		return executeTask(ctx, task, state, app)
	}

	r := newRun(flow, "start", exec)
	got := walkAll(context.Background(), r)

	if want := []string{"start", "wait", "branch", "on-true"}; !reflect.DeepEqual(got, want) {
		t.Errorf("visits = %v, want %v - the branch did not read what the node before it stored", got, want)
	}
}

// The other half, and the one the design's test plan calls out: a node that
// stored nothing leaves the name unset rather than empty, so isSet answers no
// and the macro takes the branch that says the colour never turned up.
func TestANodeThatStoredNothingLeavesTheNameUnsetForALaterBranch(t *testing.T) {
	app := newTestApp(t)
	captureLogs(t)

	flow := FlowData{
		Nodes: []Node{
			{ID: "start", Type: "StartNode", Data: map[string]interface{}{}},
			// No storeResultAs at all, which is what a node the user did not ask
			// to remember anything looks like.
			{ID: "wait", Type: "ProducerNode", Data: map[string]interface{}{}},
			{ID: "branch", Type: "BranchNode", Data: map[string]interface{}{
				"variable": "found", "operator": opIsSet,
			}},
			{ID: "on-true", Type: "StartNode", Data: map[string]interface{}{}},
			{ID: "on-false", Type: "StartNode", Data: map[string]interface{}{}},
		},
		Edges: []Edge{
			{ID: "e1", Source: "start", Target: "wait", SourceHandle: "right"},
			{ID: "e2", Source: "wait", Target: "branch", SourceHandle: "right"},
			{ID: "e3", Source: "branch", Target: "on-true", SourceHandle: branchTrue},
			{ID: "e4", Source: "branch", Target: "on-false", SourceHandle: branchFalse},
		},
	}

	exec := func(ctx context.Context, task Task, state *runState) (string, error) {
		if task.Type == "ProducerNode" {
			storeResult(state, task.Data, "00ff00")
			return "", nil
		}
		return executeTask(ctx, task, state, app)
	}

	r := newRun(flow, "start", exec)
	got := walkAll(context.Background(), r)

	if want := []string{"start", "wait", "branch", "on-false"}; !reflect.DeepEqual(got, want) {
		t.Errorf("visits = %v, want %v - a name nothing stored read as set", got, want)
	}
}

// The walk tells a node which of its outputs the user wired, because some
// outputs are conditional on the graph rather than on the payload - Wait For
// Color's timeout, and Phase 4's Loop node after it. Sorted, deduplicated, and
// empty for a node with nothing drawn from it.
func TestTheWalkTellsANodeWhichOfItsOutputsAreWired(t *testing.T) {
	flow := FlowData{
		Nodes: []Node{
			{ID: "wait", Type: "ColorPickerNode"},
			{ID: "on-match", Type: "StartNode"},
			{ID: "on-timeout", Type: "StartNode"},
		},
		Edges: []Edge{
			{ID: "e1", Source: "wait", Target: "on-timeout", SourceHandle: "timeout"},
			{ID: "e2", Source: "wait", Target: "on-match", SourceHandle: "right"},
		},
	}
	r := newRun(flow, "wait", inertExec)

	// In handle order, not the order the file listed the edges in.
	if got := r.task(flow.Nodes[0]).WiredOutputs; !reflect.DeepEqual(got, []string{"right", "timeout"}) {
		t.Errorf("WiredOutputs = %v, want [right timeout]", got)
	}
	if got := r.task(flow.Nodes[1]).WiredOutputs; len(got) != 0 {
		t.Errorf("WiredOutputs = %v for a node with no outgoing edges, want none", got)
	}
	if !r.task(flow.Nodes[0]).hasWiredOutput("timeout") {
		t.Error("hasWiredOutput did not find a handle the graph has an edge on")
	}
	if r.task(flow.Nodes[0]).hasWiredOutput("out-1") {
		t.Error("hasWiredOutput found a handle nothing is wired to")
	}
}

// A name nothing stored is unset, which is not the same as a name stored empty.
func TestRunStateTellsUnsetFromEmpty(t *testing.T) {
	r := newRun(inertFlow([]string{"start"}, nil), "start", inertExec)

	if _, ok := r.state.value("nothing-put-this-here"); ok {
		t.Error("a name nothing ever stored reads as set")
	}

	r.state.set("empty", "")
	if got, ok := r.state.value("empty"); !ok || got != "" {
		t.Errorf("value(empty) = %v, %v, want an empty string that is set", got, ok)
	}
}

// =============================================== warnAboutSkippedNodes ===============================================

// Only nodes that are wired to something are reported: one the user has just
// dropped on the canvas is a normal editing state, not a surprise. The order is
// the flowchart's, not sorted, so the same graph always produces the same
// message.
func TestWarnAboutSkippedNodesReportsOnlyWiredUpUnreachableNodes(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)

	flow := inertFlow(
		[]string{"start", "zeta", "lonely", "alpha", "live"},
		[][2]string{{"start", "live"}, {"zeta", "alpha"}},
	)
	reachable := reachableFrom("start", adjacency(flow), nodesByIDFrom(flow))

	app.warnAboutSkippedNodes(flow, reachable)

	if !logs.contains("Skipping 2 node(s) not reachable from Start: zeta, alpha") {
		t.Errorf("the warning did not name zeta and alpha in flowchart order:\n%s", logs)
	}
	if logs.contains("lonely") {
		t.Errorf("a node in no edge at all was reported:\n%s", logs)
	}
	if !logs.contains(emittedEvent("execution-nodes-skipped")) {
		t.Errorf("no execution-nodes-skipped event was emitted:\n%s", logs)
	}
}

func TestWarnAboutSkippedNodesSaysNothingWhenEverythingRuns(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)

	flow := inertFlow([]string{"start", "a"}, [][2]string{{"start", "a"}})
	app.warnAboutSkippedNodes(flow, map[string]bool{"start": true, "a": true})

	if logs.contains("Skipping") || logs.contains(emittedEvent("execution-nodes-skipped")) {
		t.Errorf("a fully reachable graph produced a warning:\n%s", logs)
	}
}

// =============================================== the multipath lint ===============================================

// Two branches joining into one node: the join, and everything after it, runs
// twice, and nothing on the canvas says so.
func TestMultipathNodesNamesTheJoin(t *testing.T) {
	flow := inertFlow(
		[]string{"start", "seq", "upper", "lower", "join"},
		[][2]string{{"start", "seq"}, {"seq", "upper"}, {"seq", "lower"}, {"upper", "join"}, {"lower", "join"}},
	)

	if got := multipathNodes(flow, "start"); !reflect.DeepEqual(got, []string{"join"}) {
		t.Errorf("multipathNodes = %v, want [join]", got)
	}
}

// A loop's header has two incoming edges by construction - the one that enters
// it and the one that comes round again. Reporting every loop the user draws as
// a mistake is what excluding back-edges is for, and Phase 4 draws loops.
func TestMultipathNodesIgnoresABackEdge(t *testing.T) {
	flow := inertFlow(
		[]string{"start", "header", "body"},
		[][2]string{{"start", "header"}, {"header", "body"}, {"body", "header"}},
	)

	if got := multipathNodes(flow, "start"); len(got) != 0 {
		t.Errorf("multipathNodes = %v, want nothing: the second edge into the header is a back-edge", got)
	}
}

// The Start node runs unconditionally, whatever is wired into it.
func TestMultipathNodesIgnoresEdgesIntoStart(t *testing.T) {
	flow := inertFlow(
		[]string{"start", "a", "b"},
		[][2]string{{"start", "a"}, {"a", "start"}, {"b", "start"}},
	)

	if got := multipathNodes(flow, "start"); len(got) != 0 {
		t.Errorf("multipathNodes = %v, want nothing", got)
	}
}

// A join fed only by branches the run can never get to is not something that
// runs twice - it is something that never runs, which is the other warning's
// business.
func TestMultipathNodesIgnoresUnreachableSources(t *testing.T) {
	flow := inertFlow(
		[]string{"start", "live", "island-a", "island-b", "join"},
		[][2]string{{"start", "live"}, {"live", "join"}, {"island-a", "join"}, {"island-b", "join"}},
	)

	if got := multipathNodes(flow, "start"); len(got) != 0 {
		t.Errorf("multipathNodes = %v, want nothing: only one of the join's sources runs", got)
	}
}

func TestWarnAboutMultipathNodesSaysNothingAboutALinearMacro(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)

	flow := inertFlow([]string{"start", "a", "b"}, [][2]string{{"start", "a"}, {"a", "b"}})
	app.warnAboutMultipathNodes(flow, "start")

	if logs.contains(emittedEvent("execution-nodes-multipath")) {
		t.Errorf("a linear macro was reported as running something twice:\n%s", logs)
	}
}

func TestWarnAboutMultipathNodesEmitsTheIDs(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)

	flow := inertFlow(
		[]string{"start", "seq", "upper", "lower", "join"},
		[][2]string{{"start", "seq"}, {"seq", "upper"}, {"seq", "lower"}, {"upper", "join"}, {"lower", "join"}},
	)
	app.warnAboutMultipathNodes(flow, "start")

	if !logs.contains(emittedEvent("execution-nodes-multipath")) {
		t.Errorf("no execution-nodes-multipath event was emitted:\n%s", logs)
	}
	if !logs.contains("more than one path and will run more than once: join") {
		t.Errorf("the warning did not name the join:\n%s", logs)
	}
}

// =============================================== startFlow ===============================================

func TestStartExecutionRejectsInvalidJSON(t *testing.T) {
	app := newTestApp(t)

	err := app.StartExecution("{not a flowchart")
	if err == nil {
		t.Fatal("StartExecution accepted invalid JSON")
	}
	if !strings.Contains(err.Error(), "invalid flowchart data") {
		t.Errorf("error = %q, want it to mention the flowchart data", err)
	}
	if app.GetIsExecuting() {
		t.Error("a rejected flowchart left the app executing")
	}
}

func TestStartFlowRejectsAnEmptyFlowchart(t *testing.T) {
	app := newTestApp(t)

	err := app.startFlow(FlowData{})
	if err == nil {
		t.Fatal("startFlow accepted a flowchart with no nodes")
	}
	if !strings.Contains(err.Error(), "at least one node") {
		t.Errorf("error = %q, want it to mention the missing nodes", err)
	}
	if app.GetIsExecuting() {
		t.Error("a rejected flowchart left the app executing")
	}
}

func TestStartFlowRejectsAFlowchartWithNoStartNode(t *testing.T) {
	app := newTestApp(t)

	flow := FlowData{Nodes: []Node{{ID: "delay-1", Type: "DelayNode", Data: map[string]interface{}{}}}}
	err := app.startFlow(flow)
	if err == nil {
		t.Fatal("startFlow accepted a flowchart with no Start node")
	}
	if !strings.Contains(err.Error(), "no Start node") {
		t.Errorf("error = %q, want it to mention the missing Start node", err)
	}
	if app.GetIsExecuting() {
		t.Error("a rejected flowchart left the app executing")
	}
}

// The toggle lives in RunMacro, not here. startFlow is the shared start path,
// and the frontend's Run button sits next to a Stop button - a Run press that
// silently stopped the run would be indistinguishable from one that started a
// fresh one - so a second run while one is going is still refused, and refusing
// must not disturb the run that is already going. See RunMacro for the whole
// argument.
func TestStartFlowRefusesASecondRun(t *testing.T) {
	app := newTestApp(t)
	app.setExecuting(true)

	err := app.startFlow(inertFlow([]string{"start"}, nil))
	if err == nil {
		t.Fatal("startFlow started a second run")
	}
	if !strings.Contains(err.Error(), "already in progress") {
		t.Errorf("error = %q, want it to say a run is already in progress", err)
	}
	if !app.GetIsExecuting() {
		t.Error("the refusal cleared the execution state of the run that is already going")
	}
}

// setExecuting reaches the tray, and there is no tray in a test. The helpers are
// documented as nil-safe; every other test in this file relies on that, so it
// gets its own check rather than being discovered as a panic somewhere else.
func TestSetExecutingIsSafeWithoutATray(t *testing.T) {
	app := newTestApp(t)
	if app.tray != nil {
		t.Fatal("the test app has a tray, so this proves nothing")
	}

	app.setExecuting(true)
	if !app.GetIsExecuting() {
		t.Error("setExecuting(true) did not take")
	}

	app.setExecuting(false)
	if app.GetIsExecuting() {
		t.Error("setExecuting(false) did not take")
	}
}

func TestStartFlowRunsALinearFlowToCompletion(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)

	flow := inertFlow(
		[]string{"start", "second", "third"},
		[][2]string{{"start", "second"}, {"second", "third"}},
	)
	if err := app.startFlow(flow); err != nil {
		t.Fatalf("startFlow: %v", err)
	}

	waitFor(t, testTimeout, "the run to report itself completed", func() bool {
		return logs.contains(emittedEvent("execution-completed"))
	})
	waitFor(t, testTimeout, "the execution state to be cleared", func() bool {
		return !app.GetIsExecuting()
	})

	for _, id := range []string{"start", "second", "third"} {
		if !logs.contains("Processing task " + id) {
			t.Errorf("node %q never ran:\n%s", id, logs)
		}
	}
	if !logs.contains("Execution finished after 3 node(s)") {
		t.Errorf("the run did not report running exactly the three nodes:\n%s", logs)
	}
}

// A branch the run can never reach is left out because the token never arrives
// at it, not because anything pruned it - and the user is told about it.
func TestStartFlowLeavesOutADeadBranchAndSaysSo(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)

	flow := inertFlow(
		[]string{"start", "live", "join", "dead"},
		[][2]string{{"start", "live"}, {"live", "join"}, {"dead", "join"}},
	)
	if err := app.startFlow(flow); err != nil {
		t.Fatalf("startFlow: %v", err)
	}

	waitFor(t, testTimeout, "the live half of the flow to complete", func() bool {
		return logs.contains(emittedEvent("execution-completed"))
	})

	if logs.contains("Processing task dead") {
		t.Errorf("a node with no path from Start ran:\n%s", logs)
	}
	if !logs.contains("Execution finished after 3 node(s)") {
		t.Errorf("the run did not execute exactly the three reachable nodes:\n%s", logs)
	}
	if !logs.contains("Skipping 1 node(s) not reachable from Start: dead") {
		t.Errorf("the dead branch was not reported as skipped:\n%s", logs)
	}
}

// A cycle used to be a stall - a run that could never make progress - and is now
// an ordinary loop, ended by its own count rather than by anything the engine
// imposes. The whole startFlow path, because a Loop Start is the first thing
// since the walk landed whose behaviour depends on the run it is inside, and
// because this is where the Loop End's own handler runs through the real
// dispatcher rather than through a fixture's executor.
func TestStartFlowRunsALoopItsCountManyTimes(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)

	flow := loopFlow(map[string]interface{}{"count": float64(3)})
	if err := app.startFlow(flow); err != nil {
		t.Fatalf("startFlow: %v", err)
	}

	waitFor(t, testTimeout, "the loop to run out of iterations and the run to finish", func() bool {
		return logs.contains(emittedEvent("execution-completed"))
	})
	waitFor(t, testTimeout, "the execution state to be cleared", func() bool {
		return !app.GetIsExecuting()
	})

	if logs.contains(emittedEvent("execution-stalled")) {
		t.Errorf("a cycle still reports a stall, which is no longer a thing that can happen:\n%s", logs)
	}
	if got := logs.count("Processing task body"); got != 3 {
		t.Errorf("the body ran %d time(s), want the count of 3:\n%s", got, logs)
	}
	// start, then the Loop Start four times - three that ran the body and the one
	// that found the count spent - with the body and the Loop End three times
	// each, and the exit once.
	if !logs.contains("Execution finished after 12 node(s)") {
		t.Errorf("the run did not execute the twelve nodes a count of three comes to:\n%s", logs)
	}
	if !logs.contains(`LoopStartNode loop: leaving by "done" after 3 iteration(s)`) {
		t.Errorf("the loop did not report leaving by done after its count:\n%s", logs)
	}
	// The back edge is an ordinary edge from an ordinary node: the Loop End runs
	// three times, once per turn, and each time hands the token back to the top.
	if got := logs.count("LoopEndNode end: back to the top of the loop"); got != 3 {
		t.Errorf("the Loop End ran %d time(s), want one per iteration:\n%s", got, logs)
	}
}

// =============================================== RunMacro ===============================================

// The tray and the global hotkeys have no frontend to get a graph from, so they
// run a macro straight off disk.
func TestRunMacroRunsASavedMacro(t *testing.T) {
	useTempAppDirs(t)
	app := newTestApp(t)
	logs := captureLogs(t)

	flow := inertFlow([]string{"start", "second"}, [][2]string{{"start", "second"}})
	if _, _, err := saveMacro(flow, "Runnable", ""); err != nil {
		t.Fatalf("saveMacro: %v", err)
	}

	if err := app.RunMacro("runnable"); err != nil {
		t.Fatalf("RunMacro: %v", err)
	}

	waitFor(t, testTimeout, "the macro to run to completion", func() bool {
		return logs.contains(emittedEvent("execution-completed"))
	})
	// The workspace may be showing a different macro, or nothing at all, so the
	// run says which macro these task events belong to.
	if !logs.contains(emittedEvent("macro-started")) {
		t.Errorf("no macro-started event was emitted:\n%s", logs)
	}
}

func TestRunMacroOnAMacroThatIsNotThere(t *testing.T) {
	useTempAppDirs(t)
	app := newTestApp(t)

	if err := app.RunMacro("nothing-saved-under-this-id"); err == nil {
		t.Fatal("RunMacro succeeded, want an error")
	}
	if app.GetIsExecuting() {
		t.Error("a macro that could not be loaded left the app executing")
	}
}

// The toggle, on the macro that makes it necessary: a Loop node with neither a
// count nor a condition, which is legal and runs forever. Press once to start,
// press again to stop, press a third time and a fresh run begins - under a new
// generation, which is what keeps the stopped run's nodes out of the new one
// (33797cb).
func TestRunMacroTogglesARunOffAndOnAgain(t *testing.T) {
	useTempAppDirs(t)
	app := newTestApp(t)
	logs := captureLogs(t)

	// No count and no condition: forever, at the minimum per-iteration yield.
	if _, _, err := saveMacro(loopFlow(map[string]interface{}{}), "Endless", ""); err != nil {
		t.Fatalf("saveMacro: %v", err)
	}

	first, _ := app.runner.Current()
	if err := app.RunMacro("endless"); err != nil {
		t.Fatalf("the first press: %v", err)
	}
	waitFor(t, testTimeout, "the endless macro to get going", func() bool {
		return app.GetIsExecuting() && logs.count("Processing task body") > 0
	})

	// The second press stops it, and starts nothing.
	if err := app.RunMacro("endless"); err != nil {
		t.Fatalf("the second press: %v", err)
	}
	if app.GetIsExecuting() {
		t.Error("the second press left the app executing")
	}
	if !logs.contains("a run was in progress, so the press stopped it") {
		t.Errorf("the second press did not report itself as the stop half of the toggle:\n%s", logs)
	}
	if !logs.contains(emittedEvent("execution-stopped")) {
		t.Errorf("the stopped run did not announce its own ending:\n%s", logs)
	}
	if logs.contains(emittedEvent("execution-completed")) {
		t.Errorf("a stopped run reported itself completed:\n%s", logs)
	}

	stopped, _ := app.runner.Current()
	if stopped == first {
		t.Errorf("the runner is still on generation %d, want the stop to have retired it", first)
	}

	// The third press starts a fresh run, under the generation the stop
	// installed. Stopped again at the end so the test leaves nothing running.
	if err := app.RunMacro("endless"); err != nil {
		t.Fatalf("the third press: %v", err)
	}
	waitFor(t, testTimeout, "the third press to start a run", func() bool {
		return app.GetIsExecuting()
	})

	third, _ := app.runner.Current()
	if third != stopped {
		t.Errorf("the third run is on generation %d, want the one the stop installed (%d)", third, stopped)
	}
	app.StopExecution()
}

// A press that lands when nothing is running starts a run rather than being
// swallowed by the toggle - the case a plain "stop if executing" would get
// right by accident and a stale flag would get wrong.
func TestRunMacroAfterARunEndedByItselfStartsAnother(t *testing.T) {
	useTempAppDirs(t)
	app := newTestApp(t)
	logs := captureLogs(t)

	flow := inertFlow([]string{"start", "second"}, [][2]string{{"start", "second"}})
	if _, _, err := saveMacro(flow, "Brief", ""); err != nil {
		t.Fatalf("saveMacro: %v", err)
	}

	for press := 1; press <= 2; press++ {
		if err := app.RunMacro("brief"); err != nil {
			t.Fatalf("press %d: %v", press, err)
		}
		waitFor(t, testTimeout, "the macro to finish", func() bool {
			return logs.count(emittedEvent("execution-completed")) == press
		})
	}

	if got := logs.count("Processing task second"); got != 2 {
		t.Errorf("the macro ran %d time(s) over two presses, want twice:\n%s", got, logs)
	}
	if logs.contains("a run was in progress, so the press stopped it") {
		t.Errorf("a press with nothing running was taken as the stop half of the toggle:\n%s", logs)
	}
}

// =============================================== StopExecution ===============================================

func TestStopExecutionWithNothingRunningLeavesTheGenerationAlone(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)
	generation := app.runner.Context()

	app.StopExecution()

	if generation.Err() != nil {
		t.Error("StopExecution cancelled the generation although no run was in progress")
	}
	if !logs.contains("no execution is in progress") {
		t.Errorf("StopExecution did not report that there was nothing to stop:\n%s", logs)
	}
}

func TestStopExecutionStopsARunInProgress(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)
	generation := app.runner.Context()

	// A delay long enough that the run is certainly still going when the stop
	// lands. It watches the run's context, so the stop ends it rather than the
	// test sitting through a minute of it.
	flow := inertFlow([]string{"start", "waiting"}, [][2]string{{"start", "waiting"}})
	flow.Nodes[1].Type = "DelayNode"
	flow.Nodes[1].Data = map[string]interface{}{"delayType": "Fixed", "time": float64(60000)}

	if err := app.startFlow(flow); err != nil {
		t.Fatalf("startFlow: %v", err)
	}
	waitFor(t, testTimeout, "the delay node to start", func() bool {
		return logs.contains("Processing task waiting")
	})

	app.StopExecution()

	if generation.Err() == nil {
		t.Error("StopExecution left the run's generation live")
	}
	if app.GetIsExecuting() {
		t.Error("StopExecution left the app reporting itself executing")
	}
	// Once, not twice. The walk announces its own ending and StopExecution does
	// not, because a stop reported twice arrives at the frontend as two status
	// messages built from the same millisecond - which is a duplicate key in a
	// keyed each, not just a repeated sentence.
	if got := logs.count(emittedEvent("execution-stopped")); got != 1 {
		t.Errorf("execution-stopped was emitted %d time(s), want exactly 1:\n%s", got, logs)
	}
	if logs.contains(emittedEvent("execution-completed")) {
		t.Errorf("a stopped run also reported itself completed:\n%s", logs)
	}
	// Stop waits for the walk, so by the time it returns the node it
	// interrupted has finished unwinding.
	if !logs.contains("Completed execution of task ID: waiting") {
		t.Errorf("StopExecution returned while the node it stopped was still running:\n%s", logs)
	}
}

// A run that has already ended is not stopped again: nothing is running, so
// there is nothing to report and nothing to cancel.
func TestStopExecutionAfterARunEndedReportsNothing(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)
	generation := app.runner.Context()

	flow := inertFlow([]string{"start", "second"}, [][2]string{{"start", "second"}})
	if err := app.startFlow(flow); err != nil {
		t.Fatalf("startFlow: %v", err)
	}
	waitFor(t, testTimeout, "the run to finish by itself", func() bool {
		return logs.contains(emittedEvent("execution-completed"))
	})

	app.StopExecution()

	if got := logs.count(emittedEvent("execution-stopped")); got != 0 {
		t.Errorf("a run that had already finished was also reported stopped %d time(s):\n%s", got, logs)
	}
	if generation.Err() != nil {
		t.Error("stopping after a finished run cancelled its generation")
	}
}

// ServiceShutdown is the one stop that does not ask whether a run is in
// progress, because the process is going away either way. A macro must not
// outlive the app that started it.
func TestServiceShutdownStopsARunInProgress(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)
	generation := app.runner.Context()

	flow := inertFlow([]string{"start", "waiting"}, [][2]string{{"start", "waiting"}})
	flow.Nodes[1].Type = "DelayNode"
	flow.Nodes[1].Data = map[string]interface{}{"delayType": "Fixed", "time": float64(60000)}

	if err := app.startFlow(flow); err != nil {
		t.Fatalf("startFlow: %v", err)
	}
	waitFor(t, testTimeout, "the delay node to start", func() bool {
		return logs.contains("Processing task waiting")
	})

	if err := app.ServiceShutdown(); err != nil {
		t.Fatalf("ServiceShutdown: %v", err)
	}

	if generation.Err() == nil {
		t.Error("ServiceShutdown left the run's generation live")
	}
	if app.GetIsExecuting() {
		t.Error("ServiceShutdown left the app reporting itself executing")
	}
	// It waits, like every other stop: nothing of the run is still touching the
	// machine by the time the process goes.
	if !logs.contains("Completed execution of task ID: waiting") {
		t.Errorf("ServiceShutdown returned while the node it stopped was still running:\n%s", logs)
	}
}
