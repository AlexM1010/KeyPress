// execution_test.go
//
// Tests for the flow execution engine: the graph the run is built from, the
// nodes it leaves out, and the three ways a run can end - finished, stalled or
// cancelled.
//
// Two things shape everything here.
//
// Every node in a test graph is a StartNode. Running any other node type reaches
// robotgo, which moves the real mouse and presses real keys on whatever machine
// the suite happens to be on; a StartNode logs, emits an event and returns.
// Since the engine only ever looks at a node's id and its edges, a graph of
// Start nodes exercises the scheduler exactly as a real macro does. findStartNode
// takes the first one in the list, so the first id in a test graph is where the
// run begins and the rest are ordinary nodes that happen to be inert.
//
// Events are observed through the log. a.wails is nil in a test, so emitEvent
// drops the event and logs that it did - which makes the log the only place an
// event is visible without standing up a real Wails application. captureLogs
// and emittedEvent below wrap that so the coupling lives in one place.

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
// worker goroutines as well as the test's own, so it is guarded.
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
// the state every test runs in - and shuts its worker pool down afterwards so a
// finished test leaves no workers behind.
func newTestApp(t *testing.T) *App {
	t.Helper()

	app := NewApp()
	t.Cleanup(app.taskQueue.Stop)
	return app
}

// inertFlow builds a flowchart from node ids and the edges between them. Every
// node is a StartNode; see the file comment for why.
func inertFlow(nodeIDs []string, edges [][2]string) FlowData {
	flow := FlowData{
		Nodes: make([]Node, 0, len(nodeIDs)),
		Edges: make([]Edge, 0, len(edges)),
	}
	for _, id := range nodeIDs {
		flow.Nodes = append(flow.Nodes, Node{
			ID:   id,
			Type: "StartNode",
			Data: map[string]interface{}{},
		})
	}
	for i, edge := range edges {
		flow.Edges = append(flow.Edges, Edge{
			ID:     fmt.Sprintf("edge-%d", i),
			Source: edge[0],
			Target: edge[1],
		})
	}
	return flow
}

// nodesByID is the node map shape setGraph wants, built from bare ids.
func nodesByID(ids ...string) map[string]Node {
	nodes := make(map[string]Node, len(ids))
	for _, id := range ids {
		nodes[id] = Node{ID: id, Type: "StartNode", Data: map[string]interface{}{}}
	}
	return nodes
}

// markCompleted records nodes as done, the way handleCompletions would.
func markCompleted(app *App, ids ...string) {
	app.completedMux.Lock()
	defer app.completedMux.Unlock()
	for _, id := range ids {
		app.completed[id] = true
	}
}

// completedIDs reports what the run has recorded as done, sorted.
func completedIDs(app *App) []string {
	app.completedMux.Lock()
	defer app.completedMux.Unlock()

	ids := make([]string, 0, len(app.completed))
	for id := range app.completed {
		ids = append(ids, id)
	}

	sort.Strings(ids)
	return ids
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

// The walk is written twice - here, to decide which nodes run, and in
// nodeLabels.ts, to decide how the status panel names and orders them. This is
// the Go half of the check that the two still agree; the TypeScript half reads
// the same files. Without it, a status panel that describes a run which did not
// happen is a silent regression.
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

			nodes := make(map[string]Node, len(fixture.Flow.Nodes))
			for _, node := range fixture.Flow.Nodes {
				nodes[node.ID] = node
			}
			edges := make(map[string][]string)
			for _, edge := range fixture.Flow.Edges {
				edges[edge.Source] = append(edges[edge.Source], edge.Target)
			}

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

// =============================================== the dependency graph ===============================================

func TestDependentsOfListsWhatRunsNext(t *testing.T) {
	app := newTestApp(t)
	app.setGraph(nodesByID("start", "a", "b"), map[string][]string{"start": {"a", "b"}})

	got := app.dependentsOf("start")
	sort.Strings(got)
	if want := []string{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("dependentsOf(start) = %v, want %v", got, want)
	}
	if deps := app.dependentsOf("a"); len(deps) != 0 {
		t.Errorf("dependentsOf(a) = %v, want none", deps)
	}
}

// The doc comment promises a copy, so that a caller can use the result without
// holding graphMux. If it handed out the live slice, this would corrupt the
// graph for the rest of the run.
func TestDependentsOfReturnsACopy(t *testing.T) {
	app := newTestApp(t)
	app.setGraph(nodesByID("start", "a", "b"), map[string][]string{"start": {"a", "b"}})

	dependents := app.dependentsOf("start")
	dependents[0] = "vandalised"
	dependents = append(dependents, "extra")
	_ = dependents

	got := app.dependentsOf("start")
	sort.Strings(got)
	if want := []string{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("the graph now says dependentsOf(start) = %v, want %v", got, want)
	}
}

func TestPrerequisitesOfListsEveryIncomingEdge(t *testing.T) {
	app := newTestApp(t)
	app.setGraph(nodesByID("a", "b", "join"), map[string][]string{
		"a": {"join"},
		"b": {"join"},
	})

	got := app.prerequisitesOf("join")
	sort.Strings(got)
	if want := []string{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("prerequisitesOf(join) = %v, want %v", got, want)
	}
	if deps := app.prerequisitesOf("a"); len(deps) != 0 {
		t.Errorf("prerequisitesOf(a) = %v, want none", deps)
	}
}

// A join waits for every branch that feeds it, not just the one that happened to
// finish first.
func TestCanEnqueueWaitsForEveryPrerequisite(t *testing.T) {
	app := newTestApp(t)
	app.setGraph(nodesByID("a", "b", "join"), map[string][]string{
		"a": {"join"},
		"b": {"join"},
	})

	if app.canEnqueue("join") {
		t.Error("join is runnable with neither branch done")
	}

	markCompleted(app, "a")
	if app.canEnqueue("join") {
		t.Error("join is runnable with only one branch done")
	}

	markCompleted(app, "b")
	if !app.canEnqueue("join") {
		t.Error("join is not runnable with both branches done")
	}
}

func TestCanEnqueueANodeWithNoPrerequisites(t *testing.T) {
	app := newTestApp(t)
	app.setGraph(nodesByID("start"), nil)

	if !app.canEnqueue("start") {
		t.Error("a node nothing feeds is not runnable")
	}
}

// Sorted, so that the same stall always reports the same way.
func TestPendingNodeIDsListsWhatIsLeftInOrder(t *testing.T) {
	app := newTestApp(t)
	app.setGraph(nodesByID("charlie", "alpha", "bravo", "delta"), nil)
	markCompleted(app, "bravo")

	got := app.pendingNodeIDs()
	if want := []string{"alpha", "charlie", "delta"}; !reflect.DeepEqual(got, want) {
		t.Errorf("pendingNodeIDs = %v, want %v", got, want)
	}
}

func TestPendingNodeIDsIsEmptyWhenEverythingIsDone(t *testing.T) {
	app := newTestApp(t)
	app.setGraph(nodesByID("a", "b"), nil)
	markCompleted(app, "a", "b")

	if got := app.pendingNodeIDs(); len(got) != 0 {
		t.Errorf("pendingNodeIDs = %v, want none", got)
	}
}

// =============================================== drainNotifications ===============================================

func TestDrainNotificationsEmptiesTheChannel(t *testing.T) {
	app := newTestApp(t)
	for _, id := range []string{"a", "b", "c"} {
		app.notifyCh <- id
	}

	drainedWithin(t, app)

	if len(app.notifyCh) != 0 {
		t.Errorf("%d notification(s) left in the channel, want none", len(app.notifyCh))
	}
}

func TestDrainNotificationsOnAnEmptyChannelDoesNothing(t *testing.T) {
	app := newTestApp(t)

	drainedWithin(t, app)

	if len(app.notifyCh) != 0 {
		t.Errorf("the channel holds %d notification(s), want none", len(app.notifyCh))
	}
}

// drainedWithin runs drainNotifications behind a deadline: it must not block,
// and a test that proves that must not hang to do it.
func drainedWithin(t *testing.T, app *App) {
	t.Helper()

	done := make(chan struct{})
	go func() {
		app.drainNotifications()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(testTimeout):
		t.Fatalf("drainNotifications blocked for %s", testTimeout)
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
	reachable := reachableFrom("start", map[string][]string{"start": {"live"}, "zeta": {"alpha"}}, map[string]Node{
		"start":  flow.Nodes[0],
		"zeta":   flow.Nodes[1],
		"lonely": flow.Nodes[2],
		"alpha":  flow.Nodes[3],
		"live":   flow.Nodes[4],
	})

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

	app.execMutex.Lock()
	app.setExecutingLocked(false)
	app.execMutex.Unlock()
	if app.GetIsExecuting() {
		t.Error("setExecutingLocked(false) did not take")
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

	if pending := app.pendingNodeIDs(); len(pending) != 0 {
		t.Errorf("nodes left unfinished: %v", pending)
	}
	if logs.contains(emittedEvent("execution-stalled")) {
		t.Errorf("a linear flow reported a stall:\n%s", logs)
	}
}

// The subtle one. A prerequisite that lives on a branch the run can never reach
// counts as satisfied, because pruning drops that edge from the dependency map
// before it is ever consulted. Without the pruning, "join" waits forever for
// "dead" and the whole run stalls one node short.
func TestStartFlowRunsANodeWhoseOtherPrerequisiteIsOnADeadBranch(t *testing.T) {
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

	if logs.contains(emittedEvent("execution-stalled")) {
		t.Errorf("the run stalled on the dead branch:\n%s", logs)
	}
	if pending := app.pendingNodeIDs(); len(pending) != 0 {
		t.Errorf("nodes left unfinished: %v", pending)
	}
	if got := completedIDs(app); !reflect.DeepEqual(got, []string{"join", "live", "start"}) {
		t.Errorf("completed = %v, want the three reachable nodes", got)
	}
	// The dead branch is wired up but unreachable, so the user is told about it.
	if !logs.contains("Skipping 1 node(s) not reachable from Start: dead") {
		t.Errorf("the dead branch was not reported as skipped:\n%s", logs)
	}
}

// A cycle can never satisfy its own prerequisites, so the run can never make
// progress again. The in-flight count notices that the moment it becomes true
// and ends the run, rather than leaving the app reporting itself busy forever.
func TestStartFlowReportsAStallOnACyclicGraph(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)

	flow := inertFlow(
		[]string{"start", "a", "b"},
		[][2]string{{"start", "a"}, {"a", "b"}, {"b", "a"}},
	)
	if err := app.startFlow(flow); err != nil {
		t.Fatalf("startFlow: %v", err)
	}

	waitFor(t, testTimeout, "the run to report a stall", func() bool {
		return logs.contains(emittedEvent("execution-stalled"))
	})
	waitFor(t, testTimeout, "the execution state to be cleared", func() bool {
		return !app.GetIsExecuting()
	})

	if got := app.pendingNodeIDs(); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("pending nodes = %v, want [a b]", got)
	}
	if !logs.contains("2 node(s) never became runnable: a, b") {
		t.Errorf("the stall did not name the stuck nodes:\n%s", logs)
	}
	if logs.contains(emittedEvent("execution-completed")) {
		t.Errorf("a stalled run also reported itself completed:\n%s", logs)
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

// =============================================== handleCompletions ===============================================

// A completion for a task that is not in this run's graph must not count
// towards it, or a stray notification from an earlier run declares the current
// one finished a node early.
func TestHandleCompletionsIgnoresATaskFromOutsideTheRun(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)

	app.setGraph(nodesByID("only"), nil)
	app.setExecuting(true)
	runGen, runCtx := app.taskQueue.Current()
	go app.handleCompletions(runCtx, runGen)

	app.notifyCh <- "stranger"
	app.notifyCh <- "only"

	waitFor(t, testTimeout, "the run to complete", func() bool {
		return logs.contains(emittedEvent("execution-completed"))
	})

	if !logs.contains("Ignoring completion of task stranger") {
		t.Errorf("the stray completion was not ignored:\n%s", logs)
	}
	if got := completedIDs(app); !reflect.DeepEqual(got, []string{"only"}) {
		t.Errorf("completed = %v, want just the node in the run", got)
	}
	if app.GetIsExecuting() {
		t.Error("the run finished but the app still reports itself executing")
	}
}

// The queue being stopped out from under a run ends it as stopped, not as
// finished.
func TestHandleCompletionsEndsTheRunWhenTheQueueIsCancelled(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)

	app.setGraph(nodesByID("a", "b"), map[string][]string{"a": {"b"}})
	app.setExecuting(true)

	// A context of this test's own, so cancellation can be driven directly
	// rather than by stopping the queue. The generation is the live one: this
	// test is about ctx ending the run, not about a stale token.
	ctx, cancel := context.WithCancel(context.Background())
	go app.handleCompletions(ctx, currentGeneration(app.taskQueue))
	cancel()

	waitFor(t, testTimeout, "the run to report itself stopped", func() bool {
		return logs.contains(emittedEvent("execution-stopped"))
	})
	waitFor(t, testTimeout, "the execution state to be cleared", func() bool {
		return !app.GetIsExecuting()
	})

	if logs.contains(emittedEvent("execution-completed")) {
		t.Errorf("a cancelled run reported itself completed:\n%s", logs)
	}
}

// =============================================== StopExecution ===============================================

func TestStopExecutionWithNothingRunningLeavesTheQueueAlone(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)
	app.taskQueue.Start(defaultWorkerCount)
	generation := app.taskQueue.Context()

	app.StopExecution()

	if generation.Err() != nil {
		t.Error("StopExecution stopped the worker pool although no run was in progress")
	}
	if !logs.contains("no execution is in progress") {
		t.Errorf("StopExecution did not report that there was nothing to stop:\n%s", logs)
	}
}

func TestStopExecutionStopsARunInProgress(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)
	app.taskQueue.Start(defaultWorkerCount)
	generation := app.taskQueue.Context()
	app.setExecuting(true)

	app.StopExecution()

	if generation.Err() == nil {
		t.Error("StopExecution left the worker pool running")
	}
	if app.GetIsExecuting() {
		t.Error("StopExecution left the app reporting itself executing")
	}
	if !logs.contains(emittedEvent("execution-stopped")) {
		t.Errorf("no execution-stopped event was emitted:\n%s", logs)
	}
}
