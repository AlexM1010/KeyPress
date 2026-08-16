// runner_test.go
//
// Tests for the run lifecycle. The claim the whole "generation" design exists
// to make good on is that a stopped run can be replaced by another that behaves
// exactly as the first did, so that is what most of this file is about.
//
// The other claim, and the sharper one, is Stop's: when it returns, nothing of
// the stopped generation is still running. That is what the WaitGroup buys, and
// TestStopWaitsForTheNodeInFlight is what proves it - through a real macro,
// because the interesting case is a node that is in the middle of something
// when the user presses stop.
//
// Close makes the same promise and then refuses everything afterwards, which is
// the third thing tested here: a hotkey or a tray click landing during teardown
// must not start a walk the app is no longer around to stop.
//
// Shared helpers (waitFor, captureLogs, newTestApp, newBubbleTestApp) live in
// execution_test.go.

package backend

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// currentGeneration is the token for whatever generation the runner is on right
// now.
//
// It is what a test means by "start a run normally". A test that needs a
// *stale* token - the case the token exists for - must capture it before the
// Stop it is meant to survive, so it deliberately does not go through here.
func currentGeneration(r *Runner) uint64 {
	gen, _ := r.Current()
	return gen
}

// waitWithin waits on a WaitGroup behind a deadline, so a goroutine that never
// returns fails the test instead of hanging the suite.
func waitWithin(t *testing.T, wg *sync.WaitGroup, what string) {
	t.Helper()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(testTimeout):
		t.Fatalf("timed out after %s waiting for %s", testTimeout, what)
	}
}

func TestRunnerRunsWhatItIsGiven(t *testing.T) {
	app := newTestApp(t)
	runner := app.runner

	ran := make(chan struct{})
	if err := runner.Go(currentGeneration(runner), func() { close(ran) }); err != nil {
		t.Fatalf("Go refused a run on the current generation: %v", err)
	}

	select {
	case <-ran:
	case <-time.After(testTimeout):
		t.Fatalf("the run did not start within %s", testTimeout)
	}
}

// The run is handed the generation's context, and Stop is what ends it. A run
// that ignored cancellation would keep driving the real keyboard after the user
// pressed stop.
func TestStopCancelsTheRunsContext(t *testing.T) {
	app := newTestApp(t)
	runner := app.runner

	gen, ctx := runner.Current()
	started := make(chan struct{})
	returned := make(chan struct{})
	runner.Go(gen, func() {
		close(started)
		<-ctx.Done()
		close(returned)
	})
	<-started

	runner.Stop()

	// Stop does not return until the run has, so this is already settled.
	select {
	case <-returned:
	default:
		t.Fatal("Stop returned while its run was still going")
	}
}

// A run that ends by itself does not retire its generation: nothing was
// cancelled, so the next run inherits a live context.
func TestARunThatEndsByItselfKeepsTheGenerationUsable(t *testing.T) {
	app := newTestApp(t)
	runner := app.runner

	gen, ctx := runner.Current()
	done := make(chan struct{})
	runner.Go(gen, func() { close(done) })
	<-done

	// The close above happens inside the run's body, and the runner only records
	// it as returned in a defer *after* that body returns - so on its own it says
	// nothing about whether Stop will see a live run. Wait for the state this
	// test is actually about, or it asserts whichever branch of Stop the
	// scheduler happened to pick and fails on a loaded machine.
	waitFor(t, testTimeout, "the run to be recorded as returned", runner.idle)

	runner.Stop()

	if ctx.Err() != nil {
		t.Error("Stop cancelled the generation of a run that had already ended")
	}
	if got := currentGeneration(runner); got != gen {
		t.Errorf("the runner moved from generation %d to %d although nothing was running", gen, got)
	}

	ranAgain := make(chan struct{})
	if err := runner.Go(currentGeneration(runner), func() { close(ranAgain) }); err != nil {
		t.Fatalf("Go refused a second run after the first ended by itself: %v", err)
	}
	select {
	case <-ranAgain:
	case <-time.After(testTimeout):
		t.Fatalf("the second run did not start within %s", testTimeout)
	}
}

// Runs are counted, not flagged, and this is the case that needs the count: a
// run that is still going does not stop another being launched into the same
// generation, and one Stop joins both.
//
// The app cannot produce the overlap - execMutex serialises startFlow against
// itself - but the runner is what makes that a policy rather than a dependency,
// and the shape it exists for is real: a run that has finished its work is
// briefly still winding down while the next is launched. A bool in place of the
// count either refuses the second run or lets the first one's exit clear the
// second one's bookkeeping, and this is the test that would notice.
func TestGoAcceptsASecondRunWhileOneIsStillGoing(t *testing.T) {
	app := newTestApp(t)
	runner := app.runner

	gen, ctx := runner.Current()
	var live atomic.Int64
	started := make(chan struct{}, 2)

	for i := 0; i < 2; i++ {
		if err := runner.Go(gen, func() {
			live.Add(1)
			defer live.Add(-1)
			started <- struct{}{}
			<-ctx.Done()
		}); err != nil {
			t.Fatalf("Go refused run %d on the current generation: %v", i+1, err)
		}
	}
	<-started
	<-started
	if got := live.Load(); got != 2 {
		t.Fatalf("%d run(s) are going, want both", got)
	}

	runner.Stop()

	// Stop returns only once both have, so this is settled rather than awaited.
	if got := live.Load(); got != 0 {
		t.Errorf("%d run(s) outlived Stop", got)
	}
	if !runner.idle() {
		t.Error("the runner still counts a run as going after Stop")
	}
}

// The point of the generations: a run that stopped the runner must not stop the
// next run from working.
func TestRunnerCanBeUsedAgainAfterBeingStopped(t *testing.T) {
	app := newTestApp(t)
	runner := app.runner

	firstGeneration := runner.Context()
	blocked := make(chan struct{})
	runner.Go(currentGeneration(runner), func() {
		<-firstGeneration.Done()
		close(blocked)
	})

	runner.Stop()
	<-blocked
	if firstGeneration.Err() == nil {
		t.Fatal("Stop left the first generation's context live")
	}

	secondGeneration := runner.Context()
	if secondGeneration == firstGeneration {
		t.Error("the runner is still on the stopped generation's context")
	}
	if secondGeneration.Err() != nil {
		t.Fatalf("the fresh generation's context is already done: %v", secondGeneration.Err())
	}

	ran := make(chan struct{})
	if err := runner.Go(currentGeneration(runner), func() { close(ran) }); err != nil {
		t.Fatalf("Go refused a run on the fresh generation: %v", err)
	}
	select {
	case <-ran:
	case <-time.After(testTimeout):
		t.Fatalf("the run after the stop did not start within %s", testTimeout)
	}
}

// A straggler from a run that is over must not be started by the run that
// replaced it.
//
// The caller that gets here for real is startFlow: it reads the token and the
// context together and then launches, and a Stop from the tray or from
// ServiceShutdown can land in between. Without the token the walk would be
// launched under a context whose cancel has already been thrown away - a macro
// nothing can stop.
func TestGoWithAStaleTokenIsRefused(t *testing.T) {
	app := newTestApp(t)
	runner := app.runner

	// Captured while that run is live, exactly as startFlow captures it.
	stale := currentGeneration(runner)
	running := runner.Context()
	runner.Go(stale, func() { <-running.Done() })
	runner.Stop()

	if stale == currentGeneration(runner) {
		t.Fatal("Stop left the runner on one generation, so this test proves nothing")
	}

	var started atomic.Bool
	if err := runner.Go(stale, func() { started.Store(true) }); err == nil {
		t.Fatal("Go accepted a run from a generation that has been stopped")
	}
	if started.Load() {
		t.Error("the stale run ran")
	}
}

func TestStopOnARunnerThatWasNeverUsedIsSafe(t *testing.T) {
	// For the leak check it registers; the runner under test is a fresh one of
	// this test's own.
	newTestApp(t)
	runner := NewRunner()

	stopped := make(chan struct{})
	go func() {
		runner.Stop()
		runner.Stop()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-time.After(testTimeout):
		t.Fatalf("Stop on a runner that was never used blocked for %s", testTimeout)
	}

	// And it is still usable afterwards.
	ran := make(chan struct{})
	if err := runner.Go(currentGeneration(runner), func() { close(ran) }); err != nil {
		t.Fatalf("Go refused a run after a pointless Stop: %v", err)
	}
	select {
	case <-ran:
	case <-time.After(testTimeout):
		t.Fatalf("the run did not start within %s", testTimeout)
	}
}

// Stop and Go racing each other, hammered: a run may be started into the
// generation being stopped or refused outright, but nothing may be left running
// once Stop has returned, and the runner has to be usable afterwards.
func TestGoRacingStopLeavesNothingBehind(t *testing.T) {
	app := newTestApp(t)
	captureLogs(t) // the racers are loud; keep the suite's output readable
	runner := app.runner

	const racers = 4

	var live atomic.Int64
	var wg sync.WaitGroup
	begin := make(chan struct{})
	stopped := make(chan struct{})
	for racer := 0; racer < racers; racer++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-begin
			for {
				select {
				case <-stopped:
					return
				default:
				}
				gen, ctx := runner.Current()
				runner.Go(gen, func() {
					live.Add(1)
					defer live.Add(-1)
					<-ctx.Done()
				})
			}
		}()
	}

	close(begin)
	runner.Stop()
	close(stopped)
	waitWithin(t, &wg, "the racing launchers to return")

	// Every run started before the Stop watched the generation it was launched
	// into, and Stop waited for all of them. Anything still live now was started
	// into the generation Stop installed on its way out, which is the leak this
	// is about - it would be running under a context the stop never cancelled.
	runner.Stop()
	if got := live.Load(); got != 0 {
		t.Errorf("%d run(s) survived the stop", got)
	}

	ran := make(chan struct{})
	if err := runner.Go(currentGeneration(runner), func() { close(ran) }); err != nil {
		t.Fatalf("Go refused a run after the race: %v", err)
	}
	select {
	case <-ran:
	case <-time.After(testTimeout):
		t.Fatalf("the runner is unusable after the race")
	}
}

// Context is documented as the handle on one particular generation: captured
// once at the start of a run, it goes done when that run is stopped, and it is
// not quietly replaced.
func TestRunnerContextTracksTheGeneration(t *testing.T) {
	app := newTestApp(t)
	runner := app.runner

	run := runner.Context()
	if run.Err() != nil {
		t.Fatalf("an idle runner's context is done: %v", run.Err())
	}

	runner.Go(currentGeneration(runner), func() { <-run.Done() })
	if runner.Context() != run {
		t.Error("starting a run replaced the generation's context")
	}

	runner.Stop()
	if run.Err() == nil {
		t.Error("the stopped generation's context is still live")
	}

	if runner.Context() == run {
		t.Error("Stop left the runner on the stopped generation's context")
	}
	if run.Err() == nil {
		t.Error("the old generation's context came back to life")
	}
}

// Stop waits for the node that was in flight, not just for the walk's loop, so
// nothing of the stopped run is still touching the machine by the time it
// returns. This is the guarantee the walk inherited from the dispatcher it
// replaced, and the reason the walk goroutine is in the runner's WaitGroup at
// all.
//
// A delay node is the probe because it is the only task type that blocks for a
// knowable time without touching the mouse, and it watches the run's context -
// so Stop's cancel ends the wait rather than this test sitting through a minute
// of it.
//
// In a synctest bubble, because "is the node genuinely running yet" is a
// question about quiescence and nothing else. Polling the log until the task
// announces itself is a guess dressed up as a wait; synctest.Wait answers it
// exactly. The minute-long delay is bubbled too, so waitInterruptible's timer is
// on the fake clock and cannot contribute a single millisecond of real time even
// if the cancel were to regress.
func TestStopWaitsForTheNodeInFlight(t *testing.T) {
	verifyNoLeaks(t)
	logs := captureLogs(t)

	synctest.Test(t, func(t *testing.T) {
		app := newBubbleTestApp(t)

		flow := inertFlow([]string{"start", "in-flight"}, [][2]string{{"start", "in-flight"}})
		flow.Nodes[1].Type = "DelayNode"
		flow.Nodes[1].Data = map[string]interface{}{"delayType": "Fixed", "time": float64(60000)}

		if err := app.startFlow(flow); err != nil {
			t.Fatalf("startFlow: %v", err)
		}

		// Wait until the delay is genuinely running. Without this, the stop
		// could win the race and the assertion below would pass without ever
		// having tested anything. Once the bubble is quiescent the only thing
		// the run can be doing is sitting in that delay.
		synctest.Wait()
		if got := logs.count("Processing task in-flight"); got != 1 {
			t.Fatalf("the delay node never started:\n%s", logs)
		}

		app.StopExecution()

		// executeTask logs this from a defer, so it is written whether the node
		// ran to completion or was cut short by the cancel - which is what makes
		// it the right signal.
		if got := logs.count("Completed execution of task ID: in-flight"); got != 1 {
			t.Errorf("Stop returned while its node was still running:\n%s", logs)
		}
		if app.GetIsExecuting() {
			t.Error("the stopped run left the app reporting itself executing")
		}
	})
}

// =============================================== Close ===============================================

// The whole point of Close: what it leaves behind refuses to run anything. Every
// entry point a macro can be started from goes through Go, so this one refusal
// covers the hotkey, the tray and the frontend at once.
func TestGoIsRefusedOnAClosedRunner(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)
	runner := app.runner

	runner.Close()

	var started atomic.Bool
	err := runner.Go(currentGeneration(runner), func() { started.Store(true) })
	if err == nil {
		t.Fatal("Go accepted a run on a closed runner")
	}
	if !errors.Is(err, errRunnerClosed) {
		t.Errorf("Go refused with %v, want it to say the runner is closed", err)
	}
	if started.Load() {
		t.Error("the refused run ran anyway")
	}
	// The refusal is logged as well as returned, because the two callers that
	// reach it with the window closed - the tray and a global hotkey - leave the
	// log as the only account of what happened.
	if !logs.contains("the runner is closed") {
		t.Errorf("the refusal was not logged:\n%s", logs)
	}
}

// Close is terminal, and a terminal operation has to survive being called
// twice. Not because Wails runs the shutdown hook twice - it does not;
// shutdownServices pops each service under a lock, and destroy() and cleanup()
// both guard re-entry - but because nothing in this type's contract says so, and
// a second caller is one added line away: a quit path that closes the runner
// before the hook, a test helper, a future explicit Shutdown. The second call
// has nothing to cancel and nothing to wait for, and must say so rather than
// block on a WaitGroup or cancel a context that has already been thrown away.
func TestCloseIsIdempotent(t *testing.T) {
	app := newTestApp(t)
	runner := app.runner

	closed := make(chan struct{})
	go func() {
		runner.Close()
		runner.Close()
		close(closed)
	}()

	select {
	case <-closed:
	case <-time.After(testTimeout):
		t.Fatalf("a second Close blocked for %s", testTimeout)
	}

	if err := runner.Go(currentGeneration(runner), func() {}); err == nil {
		t.Error("the runner accepted a run after being closed twice")
	}
}

// Close keeps Stop's promise: when it returns, nothing of the generation it
// cancelled is still going. Losing that would mean a macro still pressing keys
// while the process exits around it.
func TestCloseWaitsForTheRunInFlight(t *testing.T) {
	app := newTestApp(t)
	runner := app.runner

	gen, ctx := runner.Current()
	started := make(chan struct{})
	returned := make(chan struct{})
	runner.Go(gen, func() {
		close(started)
		<-ctx.Done()
		close(returned)
	})
	<-started

	runner.Close()

	// Settled rather than awaited: Close does not return until the run has.
	select {
	case <-returned:
	default:
		t.Fatal("Close returned while its run was still going")
	}
	if ctx.Err() == nil {
		t.Error("Close left the closed generation's context live")
	}
	if !runner.idle() {
		t.Error("the runner still counts a run as going after Close")
	}
}

// Close does not hand the next caller a fresh generation to run in - that is
// the difference from Stop, and it is what makes shutdown terminal.
func TestCloseDoesNotInstallAFreshGeneration(t *testing.T) {
	app := newTestApp(t)
	runner := app.runner

	gen, ctx := runner.Current()

	runner.Close()

	nextGen, nextCtx := runner.Current()
	if nextGen != gen {
		t.Errorf("Close moved the runner from generation %d to %d", gen, nextGen)
	}
	if nextCtx != ctx {
		t.Error("Close installed a fresh context, leaving the runner startable")
	}
	if nextCtx.Err() == nil {
		t.Error("the closed runner's context is still live")
	}
}

// A Stop that lands after a Close must not resurrect the runner. The two can
// meet for real: the tray's Stop item is still clickable while the app tears
// itself down.
func TestStopAfterCloseLeavesTheRunnerClosed(t *testing.T) {
	app := newTestApp(t)
	runner := app.runner

	runner.Close()
	runner.Stop()

	if err := runner.Go(currentGeneration(runner), func() {}); err == nil {
		t.Error("a Stop after Close made the runner startable again")
	}
}

// =============================================== shutdown, through the App ===============================================

// The bug this is all for. ServiceShutdown used to leave the runner startable,
// so a global hotkey or a tray click arriving during teardown started a walk
// that nothing was left to stop. It goes through startFlow rather than through
// the runner because startFlow is what every one of those entry points calls,
// and reporting a run that is not happening is the user-facing half of the
// fault.
func TestStartFlowIsRefusedAfterServiceShutdown(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)

	if err := app.ServiceShutdown(); err != nil {
		t.Fatalf("ServiceShutdown: %v", err)
	}

	flow := inertFlow([]string{"start", "second"}, [][2]string{{"start", "second"}})
	err := app.startFlow(flow)
	if err == nil {
		t.Fatal("startFlow started a run after the app had shut down")
	}
	if !errors.Is(err, errRunnerClosed) {
		t.Errorf("startFlow refused with %v, want it to say the app is shutting down", err)
	}

	if logs.contains("Processing task start") {
		t.Errorf("a node ran after shutdown:\n%s", logs)
	}
	if app.GetIsExecuting() {
		t.Error("a refused run left the app reporting itself executing")
	}
	// Nothing to announce either: the run never began, so the status panel must
	// not be told one ended.
	if logs.contains(emittedEvent("execution-completed")) {
		t.Errorf("a run that was refused reported itself completed:\n%s", logs)
	}
}

// RunMacro is the tray's and the hotkey's entry point, and it is the one that
// reports failure in a dialog - so the refusal has to come back to it as an
// error rather than being swallowed by startFlow.
func TestRunMacroIsRefusedAfterServiceShutdown(t *testing.T) {
	useTempAppDirs(t)
	app := newTestApp(t)

	flow := inertFlow([]string{"start", "second"}, [][2]string{{"start", "second"}})
	if _, _, err := saveMacro(flow, "Runnable", ""); err != nil {
		t.Fatalf("saveMacro: %v", err)
	}

	if err := app.ServiceShutdown(); err != nil {
		t.Fatalf("ServiceShutdown: %v", err)
	}

	if err := app.RunMacro("runnable"); err == nil {
		t.Fatal("RunMacro started a macro after the app had shut down")
	}
	if app.GetIsExecuting() {
		t.Error("a refused macro left the app reporting itself executing")
	}
}

// The regression the shutdown fix must not cause: a user pressing stop leaves
// the runner usable, so the next macro runs exactly as the first did. Through
// startFlow, because that is where "usable" is cashed in.
func TestAMacroRunsAgainAfterStopExecution(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)

	stopped := inertFlow([]string{"start", "waiting"}, [][2]string{{"start", "waiting"}})
	stopped.Nodes[1].Type = "DelayNode"
	stopped.Nodes[1].Data = map[string]interface{}{"delayType": "Fixed", "time": float64(60000)}

	if err := app.startFlow(stopped); err != nil {
		t.Fatalf("startFlow: %v", err)
	}
	waitFor(t, testTimeout, "the delay node to start", func() bool {
		return logs.contains("Processing task waiting")
	})

	app.StopExecution()

	if err := app.startFlow(inertFlow([]string{"start", "second"}, [][2]string{{"start", "second"}})); err != nil {
		t.Fatalf("the run after a stop was refused: %v", err)
	}
	waitFor(t, testTimeout, "the second macro to finish", func() bool {
		return logs.contains(emittedEvent("execution-completed"))
	})
}
