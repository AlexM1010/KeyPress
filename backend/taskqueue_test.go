// taskqueue_test.go
//
// Tests for the restartable worker pool. The claim the whole "generation"
// design exists to make good on is that a stopped queue can be started again
// and behave exactly as it did the first time, so that is what most of this
// file is about.
//
// Every task here is a StartNode, for the reason set out at the top of
// execution_test.go: it is the only task type that does not reach robotgo and
// drive the real mouse and keyboard. Completions are observed on app.notifyCh,
// which is where a worker reports a finished task - no logs or events needed.
//
// Shared helpers (waitFor, captureLogs, newTestApp, newBubbleTestApp) live in
// execution_test.go.
//
// Three tests here run inside a testing/synctest bubble, and each says at its
// own doc comment why. The common thread: they are the tests whose claim is
// that something did *not* happen - Enqueue did not return, no task ran - and
// outside a bubble that can only be approximated by sleeping for a while and
// taking the silence as proof. Inside one, synctest.Wait blocks until every
// other goroutine is durably blocked, which turns "not yet" into "not at all"
// and costs no real time. The tests that hammer the Enqueue/Stop race are
// deliberately *not* bubbled: they want the scheduler and the race detector,
// and a bubble would serialise away the interleaving they exist to provoke.

package backend

import (
	"fmt"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// inertTask is a task a worker can run without touching the machine it is
// running on.
func inertTask(id string) Task {
	return Task{ID: id, Type: "StartNode", Data: map[string]interface{}{}}
}

// currentGeneration is the token for whatever generation the queue is on right
// now.
//
// It is what a test means by "enqueue this normally". A test that needs a
// *stale* token - the case the token exists for - must capture it before the
// Stop it is meant to survive, so it deliberately does not go through here.
func currentGeneration(q *TaskQueue) uint64 {
	gen, _ := q.Current()
	return gen
}

// awaitCompletion waits for a worker to report the given task done, discarding
// completions of other tasks on the way.
func awaitCompletion(t *testing.T, app *App, taskID string) {
	t.Helper()

	deadline := time.After(testTimeout)
	for {
		select {
		case got := <-app.notifyCh:
			if got == taskID {
				return
			}
		case <-deadline:
			t.Fatalf("task %q was not processed within %s", taskID, testTimeout)
		}
	}
}

// refuseCompletion fails if any task is reported done. It is how a test says
// "nothing ran".
//
// Only callable from inside a synctest bubble, and that is the point. Outside
// one this could only ever be a bounded wait - it used to sleep 100ms and hope
// - which proves nothing more than that no task ran *yet*. synctest.Wait
// returns once every other goroutine in the bubble is durably blocked, so a
// task that was going to run has run by the time the channel is checked, and
// an empty channel means nothing ran at all rather than nothing ran in time.
func refuseCompletion(t *testing.T, app *App) {
	t.Helper()

	synctest.Wait()

	select {
	case got := <-app.notifyCh:
		t.Fatalf("task %q was processed, want nothing to run", got)
	default:
	}
}

// expectNextCompletion waits for the next completion and fails unless it is the
// one named. Where awaitCompletion skips past anything else on the way, this
// insists nothing else ran first - which is how a test catches a task that the
// previous generation leaked into this one.
//
// It relies on the pool being a single worker: the task channel is FIFO, so a
// leaked task was buffered ahead of the sentinel and one worker has to take it
// first. With several workers a leak could be executed after the sentinel and
// the assertion would miss it.
func expectNextCompletion(t *testing.T, app *App, taskID string) {
	t.Helper()

	select {
	case got := <-app.notifyCh:
		if got != taskID {
			t.Fatalf("task %q ran before %q, want %q to be the only task in this generation", got, taskID, taskID)
		}
	case <-time.After(testTimeout):
		t.Fatalf("task %q was not processed within %s", taskID, testTimeout)
	}
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

func TestTaskQueueProcessesEnqueuedTasks(t *testing.T) {
	app := newTestApp(t)
	queue := app.taskQueue

	queue.Start(unlimitedWorkers)
	queue.Enqueue(currentGeneration(queue), inertTask("first"))

	awaitCompletion(t, app, "first")
}

// Start is documented as safe to call defensively - startFlow calls it on every
// run - so a second call while the pool is up must not bring up a second set of
// workers on top of the first.
func TestTaskQueueStartIsIdempotent(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)
	queue := app.taskQueue

	queue.Start(2)
	queue.Start(2)
	queue.Start(5)

	// Start logs this line itself, before returning, so the count is settled by
	// the time the third call is back.
	if got := logs.count("TaskQueue started with"); got != 1 {
		t.Errorf("the pool was brought up %d times, want once:\n%s", got, logs)
	}

	// A no-op Start must not have broken the pool it declined to replace.
	queue.Enqueue(currentGeneration(queue), inertTask("after-the-second-start"))
	awaitCompletion(t, app, "after-the-second-start")

	// How many dispatchers there really are is only knowable once they have
	// exited, and Stop does not return until they have. One, not three: the
	// later calls added nothing.
	queue.Stop()
	if got := logs.count("Dispatcher stopping: context canceled"); got != 1 {
		t.Errorf("%d dispatchers exited, want the 1 the first Start brought up:\n%s", got, logs)
	}
}

// The point of the generations: a run that stopped the pool must not stop the
// next run from working.
func TestTaskQueueCanBeStartedAgainAfterBeingStopped(t *testing.T) {
	app := newTestApp(t)
	queue := app.taskQueue

	queue.Start(unlimitedWorkers)
	firstGeneration := queue.Context()
	queue.Enqueue(currentGeneration(queue), inertTask("before"))
	awaitCompletion(t, app, "before")

	queue.Stop()
	if firstGeneration.Err() == nil {
		t.Fatal("Stop left the first generation's context live")
	}

	queue.Start(unlimitedWorkers)
	secondGeneration := queue.Context()
	if secondGeneration == firstGeneration {
		t.Error("the restarted pool is running on the stopped generation's context")
	}
	if secondGeneration.Err() != nil {
		t.Fatalf("the restarted pool's context is already done: %v", secondGeneration.Err())
	}

	queue.Enqueue(currentGeneration(queue), inertTask("after"))
	awaitCompletion(t, app, "after")
}

// Stop waits for the task that was in flight, not just for the dispatcher, so
// nothing of the stopped generation is still touching the machine by the time
// it returns.
//
// This is the guarantee that survived the move from a fixed pool to a
// goroutine per task, and it is the one worth asserting directly: the
// dispatcher holds itself open until the tasks it started have returned, so
// q.wg is not satisfied while one is still running. A delay node is the probe
// because it is the only task type that blocks for a knowable time without
// touching the mouse, and it watches the generation's context - so Stop's
// cancel ends the wait rather than this test sitting through a minute of it.
//
// In a synctest bubble, because "is the task genuinely running yet" is a
// question about quiescence and nothing else. The old version polled the log
// every millisecond until the task announced itself, which is a guess dressed
// up as a wait; synctest.Wait answers it exactly. The minute-long delay is
// bubbled too, so waitInterruptible's timer is on the fake clock and cannot
// contribute a single millisecond of real time even if the cancel were to
// regress.
func TestTaskQueueStopWaitsForTheTaskInFlight(t *testing.T) {
	verifyNoLeaks(t)
	logs := captureLogs(t)

	synctest.Test(t, func(t *testing.T) {
		app := newBubbleTestApp(t)
		queue := app.taskQueue

		queue.Start(unlimitedWorkers)
		queue.Enqueue(currentGeneration(queue), Task{
			ID:   "in-flight",
			Type: "DelayNode",
			Data: map[string]interface{}{"delayType": "Fixed", "time": float64(60000)},
		})

		// Wait until it is genuinely running. Without this, Stop could win the
		// race to the channel and the assertion below would pass without ever
		// having tested anything. Once the bubble is quiescent the only thing
		// the task can be doing is sitting in its delay.
		synctest.Wait()
		if got := logs.count("Processing task in-flight"); got != 1 {
			t.Fatalf("the delay task never started:\n%s", logs)
		}

		queue.Stop()

		// executeTask logs this from a defer, so it is written whether the task
		// ran to completion or was cut short by the cancel - which is what makes
		// it the right signal. The completion on notifyCh is not:
		// notifyTaskCompletion drops it when the context is already cancelled,
		// so whether it arrives depends on whether the task beat Stop to it, and
		// asserting on it made this test fail about one run in three.
		if got := logs.count("Completed execution of task ID: in-flight"); got != 1 {
			t.Errorf("Stop returned while its task was still running:\n%s", logs)
		}

		if got := logs.count("Dispatcher stopping: context canceled"); got != 1 {
			t.Errorf("%d dispatchers exited, want 1:\n%s", got, logs)
		}
	})
}

func TestTaskQueueStopOnAQueueThatWasNeverStartedIsSafe(t *testing.T) {
	app := newTestApp(t)
	queue := NewTaskQueue(app, taskBufferSize)

	stopped := make(chan struct{})
	go func() {
		queue.Stop()
		queue.Stop()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-time.After(testTimeout):
		t.Fatalf("Stop on a queue that was never started blocked for %s", testTimeout)
	}

	// And it is still startable afterwards.
	queue.Start(1)
	t.Cleanup(queue.Stop)
	queue.Enqueue(currentGeneration(queue), inertTask("after-a-pointless-stop"))
	awaitCompletion(t, app, "after-a-pointless-stop")
}

// Enqueueing into a stopped queue must not panic on a closed channel and must
// not block the caller. Nothing runs, because there are no workers.
//
// And - the part that is not free - nothing runs later either. Stop leaves the
// queue on a fresh, uncancelled generation, so a task accepted after Stop would
// sit in that generation's buffer until the next Start handed it to the next
// run's workers. In this app that is a click or a keystroke the user never
// asked for, arriving after they pressed stop. The caller that gets here for
// real is handleCompletions, which is not covered by Stop's wait for its
// workers.
//
// In a synctest bubble, because both halves of the claim are about something
// *not* happening and a bubble is the only way to say that without picking a
// number. "Enqueue did not block" becomes: once the bubble is quiescent the
// enqueuer has either finished or is durably parked, and finding it finished
// settles it - no five second deadline that a slow machine could exceed
// legitimately. "Nothing ran" becomes the same kind of statement, and stops
// costing the suite the 100ms it used to sleep to make it.
func TestEnqueueAfterStopReturnsWithoutRunningAnything(t *testing.T) {
	verifyNoLeaks(t)

	synctest.Test(t, func(t *testing.T) {
		app := newBubbleTestApp(t)
		queue := app.taskQueue

		queue.Start(unlimitedWorkers)
		queue.Stop()

		enqueued := make(chan struct{})
		go func() {
			for i := 0; i < 5; i++ {
				queue.Enqueue(currentGeneration(queue), inertTask(fmt.Sprintf("orphan-%d", i)))
			}
			close(enqueued)
		}()

		synctest.Wait()
		select {
		case <-enqueued:
		default:
			t.Fatal("Enqueue on a stopped queue blocked")
		}

		refuseCompletion(t, app)

		// One worker, so the orphans - buffered ahead of the sentinel if they
		// were accepted at all - would have to be executed before it.
		queue.Start(1)
		queue.Enqueue(currentGeneration(queue), inertTask("the-next-run"))
		expectNextCompletion(t, app, "the-next-run")
	})
}

// A straggler from a run that is over must not be executed by the run that
// replaced it.
//
// This is the interleaving q.started cannot catch. By the time the stale caller
// reaches Enqueue the user has already started another macro, so the queue is
// legitimately running and its context is live: both of Enqueue's other guards
// pass. Only the caller's own token says which run it belongs to.
//
// The caller that gets here for real is App.handleCompletions, holding the
// generation startFlow captured for its run and untracked by Stop's wait for
// its workers.
func TestEnqueueWithAStaleTokenIsRefusedByTheNextRun(t *testing.T) {
	app := newTestApp(t)
	queue := app.taskQueue

	queue.Start(1)
	// Captured while that run is live, exactly as startFlow captures it.
	stale := currentGeneration(queue)
	queue.Stop()

	// The next run. The queue is started again, so nothing about its state says
	// the straggler below is stale - only the token does.
	queue.Start(1)
	t.Cleanup(queue.Stop)
	if stale == currentGeneration(queue) {
		t.Fatal("Stop and Start left the queue on one generation, so this test proves nothing")
	}

	queue.Enqueue(stale, inertTask("straggler"))

	// One worker and a FIFO channel: had the straggler been accepted it was
	// buffered first and would have to run before the sentinel.
	queue.Enqueue(currentGeneration(queue), inertTask("the-next-run"))
	expectNextCompletion(t, app, "the-next-run")
}

// The task channel is never closed, because a close would race Enqueue's select
// and panic on the send. This hammers exactly that race: a panic in any of these
// goroutines takes the test binary down, which is the loudest failure there is.
func TestEnqueueDuringStopDoesNotPanic(t *testing.T) {
	app := newTestApp(t)
	queue := app.taskQueue
	queue.Start(unlimitedWorkers)

	const (
		enqueuers        = 4
		tasksPerEnqueuer = 10
	)

	var wg sync.WaitGroup
	begin := make(chan struct{})
	for enqueuer := 0; enqueuer < enqueuers; enqueuer++ {
		wg.Add(1)
		go func(enqueuer int) {
			defer wg.Done()
			<-begin
			for i := 0; i < tasksPerEnqueuer; i++ {
				queue.Enqueue(currentGeneration(queue), inertTask(fmt.Sprintf("racer-%d-%d", enqueuer, i)))
			}
		}(enqueuer)
	}

	close(begin)
	queue.Stop()
	// The whole run is well under the channel's capacity, so no enqueuer can be
	// left parked on a full buffer that nothing is draining.
	waitWithin(t, &wg, "the concurrent enqueuers to return")

	// Whatever the race did, the queue is still usable.
	app.drainNotifications()
	queue.Start(unlimitedWorkers)
	queue.Enqueue(currentGeneration(queue), inertTask("survivor"))
	awaitCompletion(t, app, "survivor")
}

// The same race as above, asked the harder question: not "did it panic" but
// "did anything get through". A task submitted while the queue is being torn
// down may run in the generation being stopped or not run at all, but it must
// never be left sitting in the generation Stop installs on its way out, waiting
// for the next run's workers to pick it up.
//
// The enqueuers run flat out until Stop has returned, so they are still calling
// Enqueue during the window between Stop's cancel and the fresh generation it
// leaves behind - which is the window the real caller, handleCompletions, sits
// in. Backpressure bounds them: once the buffer is full they park on the send
// until cancellation releases them.
func TestEnqueueRacingStopDoesNotLeakIntoTheNextGeneration(t *testing.T) {
	app := newTestApp(t)
	captureLogs(t) // the enqueuers are loud; keep the suite's output readable
	queue := app.taskQueue
	queue.Start(unlimitedWorkers)

	const enqueuers = 4

	var wg sync.WaitGroup
	begin := make(chan struct{})
	stopped := make(chan struct{})
	for enqueuer := 0; enqueuer < enqueuers; enqueuer++ {
		wg.Add(1)
		go func(enqueuer int) {
			defer wg.Done()
			<-begin
			for i := 0; ; i++ {
				select {
				case <-stopped:
					return
				default:
				}
				queue.Enqueue(currentGeneration(queue), inertTask(fmt.Sprintf("racer-%d-%d", enqueuer, i)))
			}
		}(enqueuer)
	}

	close(begin)
	queue.Stop()
	close(stopped)
	waitWithin(t, &wg, "the concurrent enqueuers to return")

	// Stop waited for the workers, so nothing is still reporting completions:
	// what is on notifyCh is exactly what the stopped generation ran, and that
	// was legitimate. Clear it, then see what the next generation inherits.
	app.drainNotifications()

	queue.Start(1)
	queue.Enqueue(currentGeneration(queue), inertTask("the-next-run"))
	expectNextCompletion(t, app, "the-next-run")
}

// Enqueue blocks while the queue is full, and stopping the queue is what gets
// it moving again. A caller parked on a full buffer must not be left there by a
// shutdown.
//
// In a synctest bubble, because "it is still blocked" was the one assertion
// here that a sleep could only approximate: the old version waited 50ms and
// took Enqueue's silence as proof, which is both slow and weaker than it looks
// - an Enqueue that returned at 51ms would have passed. synctest.Wait waits
// until every other goroutine in the bubble is durably blocked, so the
// enqueuer being parked at that moment is a fact about the program rather than
// about how long the test was willing to wait.
func TestEnqueueOnAFullQueueUnblocksWhenTheQueueStops(t *testing.T) {
	verifyNoLeaks(t)

	synctest.Test(t, func(t *testing.T) {
		app := newBubbleTestApp(t)
		// One slot and no workers, so the second task has nowhere to go.
		queue := NewTaskQueue(app, 1)
		queue.Start(0)
		t.Cleanup(queue.Stop)

		queue.Enqueue(currentGeneration(queue), inertTask("fills-the-buffer"))

		blocked := make(chan struct{})
		go func() {
			queue.Enqueue(currentGeneration(queue), inertTask("has-to-wait"))
			close(blocked)
		}()

		synctest.Wait()
		select {
		case <-blocked:
			t.Fatal("Enqueue returned although the queue was full")
		default:
		}

		queue.Stop()

		synctest.Wait()
		select {
		case <-blocked:
		default:
			t.Fatal("Enqueue was still parked on the full queue after Stop")
		}
	})
}

// Context is documented as the handle on one particular generation: captured
// once at the start of a run, it goes done when that run's pool is stopped, and
// it is not quietly replaced by the next Start.
func TestTaskQueueContextTracksTheGeneration(t *testing.T) {
	app := newTestApp(t)
	queue := app.taskQueue

	queue.Start(1)
	run := queue.Context()
	if run.Err() != nil {
		t.Fatalf("a running queue's context is done: %v", run.Err())
	}

	queue.Start(1) // no-op, must not swap the generation
	if queue.Context() != run {
		t.Error("a no-op Start replaced the generation's context")
	}

	queue.Stop()
	if run.Err() == nil {
		t.Error("the stopped generation's context is still live")
	}

	queue.Start(1)
	if queue.Context() == run {
		t.Error("Start reused the stopped generation's context")
	}
	if run.Err() == nil {
		t.Error("the old generation's context came back to life")
	}
}
