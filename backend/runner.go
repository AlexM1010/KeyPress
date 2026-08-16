// runner.go
//
// The lifecycle of a run: the generation token, the context that belongs to it,
// and the WaitGroup that lets Stop promise nothing of a stopped run is still
// touching the mouse.

package backend

import (
	"context"
	"errors"
	"log"
	"sync"
)

// =============================================== Flow Execution ===============================================

// errRunnerClosed is why Go refuses once Close has been called: the app is on
// its way out and there is no next run to be had. It reaches the user through
// startFlow, so it is worded for the dialog runFromTray shows - "Could not run
// this macro: the app is shutting down" - rather than for the log.
var errRunnerClosed = errors.New("the app is shutting down")

// errStaleGeneration is why Go refuses a token that is no longer current: the
// generation the caller meant to run in was stopped between its Current and its
// Go.
var errStaleGeneration = errors.New("execution was stopped before the run could start")

// Runner owns the lifecycle of a run: the generation that is current, the
// context that belongs to it, and the WaitGroup a stop waits on. It starts
// runs; it does not schedule them, and there is nothing for it to queue - the
// interpreter gives one goroutine an execution token and has it walk the graph
// itself.
//
// A "generation" is a context and the runs launched into it. Go launches a run
// into the generation the caller names; Stop cancels that generation, waits for
// its run to return and installs a fresh one, so a later Go - the user's next
// macro - works exactly like the first.
//
// A run that ends by itself deliberately does *not* retire its generation.
// Nothing was cancelled, so the next run inherits a live context and the same
// token.
//
// Close is the other way a generation ends, and the terminal one. It cancels
// and waits exactly as Stop does but installs nothing and marks the runner
// closed, so every later Go is refused. That is what stops a global hotkey, a
// tray click or a StartExecution landing during teardown from beginning a walk
// that nothing will ever stop.
type Runner struct {
	// lifecycleMu serializes whole Go/Stop/Close operations against each other,
	// so two concurrent Stops cannot both install a new generation, and so a Go
	// can never be a wg.Add racing Stop's wg.Wait.
	//
	// It is held across that Wait, which makes one rule for everything a run
	// does: **a run must never call Go, Stop or Close**, directly or through the
	// App. Current and Context are fine - they take r.mu only. The one to watch
	// for is a future "run another macro" node reaching startFlow from inside a
	// handler: that would deadlock on execMutex and on this.
	lifecycleMu sync.Mutex

	// mu guards the generation fields below. It is only ever held briefly, and
	// never across wg.Wait: a run on its way out calls back into the App, and
	// the App is at liberty to ask this type anything it likes.
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc

	// gen identifies the current generation. Go takes it as a token so a caller
	// can say which generation it means, rather than only "whichever is
	// current" - see Go. NewRunner installs the first generation, so a live
	// token is always >= 1 and the zero value is permanently stale.
	gen uint64

	// running counts the runs of the current generation that have not returned
	// yet. A count rather than a flag: a run that has just finished is briefly
	// still winding down while the next one is being started, and a flag would
	// have the second launch either refused or clearing the first one's
	// bookkeeping.
	running int

	// closed is set by Close and never cleared. It is what makes shutdown
	// terminal rather than one more stop: Stop leaves the runner usable on
	// purpose, and "usable" is exactly wrong when the process is going away.
	// One-way, so a Stop that lands after a Close cannot resurrect it.
	closed bool

	// wg tracks every run Go has launched and not yet seen return.
	wg sync.WaitGroup
}

// NewRunner initializes a new Runner.
func NewRunner() *Runner {
	r := &Runner{}
	r.mu.Lock()
	r.newGenerationLocked()
	r.mu.Unlock()
	return r
}

// newGenerationLocked installs a fresh context and retires every token issued
// for the previous one. Callers must hold r.mu.
func (r *Runner) newGenerationLocked() {
	r.gen++
	r.ctx, r.cancel = context.WithCancel(context.Background())
}

// Go starts fn as a run of generation gen, and reports why it would not.
//
// The run joins r.wg here, before any Stop can be waiting on it. That is what
// keeps Stop's promise - when Stop returns, nothing of the stopped generation
// is still touching the mouse - and it is why a bare `go a.walk(...)` would be
// wrong however tidy it looks: nothing would wait for it, and the generation
// could advance under a run that is still driving the real keyboard.
//
// A closed runner refuses everything. Every entry point a macro can be started
// from - the frontend's StartExecution, the tray menu, a global hotkey - funnels
// through startFlow and so through here, which is why the refusal lives at this
// end rather than in the hotkey path: one check covers all three, including the
// ones nobody has thought of yet.
//
// A token that is not current is refused too, so that fn cannot be launched
// under a context whose cancel has already been thrown away - a macro nothing
// could stop. Today no caller can actually produce that: startFlow holds
// execMutex across its Current and its Go, and every stop there is takes
// execMutex too - StopExecution, ServiceShutdown, and the toggle's stopIfRunning,
// which was written to hold the lock across its check *and* its stop for exactly
// this reason - so the three are mutually exclusive and this branch is
// unreachable from the app.
//
// It is kept as the invariant rather than as a guard against the callers there
// are now. What would need it is a stop that does not hold execMutex: a stop
// from a Wails callback that cannot take that lock, or a second entry point that
// forgets to. The refusal is what makes such a caller safe to add without
// re-deriving this lifecycle, and it costs one comparison per run.
//
// The wg.Add is under lifecycleMu, which Stop and Close hold across their
// wg.Wait, and that is not redundant with r.mu: running++ and wg.Add(1) are
// deliberately not in one r.mu critical section, so without lifecycleMu a Stop
// could read running == 1 between them and enter wg.Wait before the Add - a
// Wait that waits for nothing, and a Stop that returns with a run still going.
func (r *Runner) Go(gen uint64, fn func()) error {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		log.Printf("Refusing a run from generation %d: the runner is closed", gen)
		return errRunnerClosed
	}
	if gen != r.gen {
		current := r.gen
		r.mu.Unlock()
		log.Printf("Refusing a run from generation %d: the runner is on generation %d", gen, current)
		return errStaleGeneration
	}
	r.running++
	r.mu.Unlock()

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer r.runReturned(gen)
		fn()
	}()
	return nil
}

// runReturned records that a run of gen has finished.
//
// It takes r.mu only. Taking lifecycleMu here would deadlock against Stop,
// which holds it across the wg.Wait this is on the way to satisfying.
//
// A run of a generation that has already been retired has nothing to decrement:
// Stop zeroed the count when it installed the new generation.
func (r *Runner) runReturned(gen uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if gen != r.gen {
		return
	}
	r.running--
}

// idle reports whether the bookkeeping shows no run of the current generation
// outstanding. That is very nearly "every run has returned", and deliberately
// not quite: running-- is deferred, so it happens after the run body has
// returned but a moment before the goroutine itself exits. wg is what actually
// orders the goroutine's end; this is what orders its work.
//
// It exists for the tests, and specifically for the ones that mean "after the
// run has ended". A run signalling from inside its own body proves nothing
// about the bookkeeping: running-- happens in a deferred call, after the body
// has returned, so a test that stops the runner on such a signal is asserting
// whichever branch of Stop the scheduler happened to reach. This is the state
// those tests actually mean to wait for.
func (r *Runner) idle() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running == 0
}

// Closed reports whether Close has been called.
//
// Only ever false-to-true, which is what makes it usable as an early check
// rather than a guard: a caller that reads false may still be refused by Go a
// moment later, and Go is the authority. startFlow asks so that it can decline
// before claiming the executing flag, because clearing that flag again writes
// twenty-odd tray menu items the main thread may already be destroying.
func (r *Runner) Closed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

// Context returns the context of the current generation. It is canceled by
// Stop; a subsequent Go runs under a fresh context. Callers that need to
// observe the shutdown of one particular run must capture this once, at the
// start of that run, rather than re-reading it.
func (r *Runner) Context() context.Context {
	_, ctx := r.Current()
	return ctx
}

// Current returns the token and the context of the current generation.
//
// Both in one call, and read under a single acquisition of r.mu, because a run
// captures them together at its start and they must describe the same
// generation: taking them from two separate calls leaves a window where Stop
// lands in between and the run walks under one generation's context while
// having launched into another - exactly the confusion the token exists to
// prevent.
func (r *Runner) Current() (uint64, context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.gen, r.ctx
}

// Stop cancels the current generation, waits for its run and prepares the
// runner to be used again. It is what the user pressing stop does, so leaving
// the runner startable is the point of it rather than an oversight - for the
// stop that must not, see Close.
//
// Callers must not hold a lock the run takes on its way out. App.walk clears
// the execution state and emits the run's last event from inside the run
// goroutine, so StopExecution releases nothing but takes execMutex around the
// whole of itself - and the walk deliberately touches neither.
func (r *Runner) Stop() {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	r.mu.Lock()
	running, cancel := r.running, r.cancel
	r.mu.Unlock()

	if running == 0 {
		// Nothing of this generation is running, so there is nothing to cancel
		// and no reason to retire the generation - a run that ended by itself
		// leaves the runner ready for the next one on the same context, and
		// cancelling here would make every between-runs reader of Context()
		// look at a dead one.
		//
		// The wait is still worth doing: a run that has finished its work and
		// decremented the count is briefly still a goroutine, and a caller of
		// Stop is entitled to assume there is none left when it returns. It
		// costs nothing when there never was one.
		//
		// This branch is also what stops a late Stop from resurrecting a closed
		// runner, and that is worth spelling out because neither function
		// mentions the other. There is no closed check here. What there is:
		// Close zeroes running before it returns, so a Stop that lands after it
		// takes this branch, and this branch deliberately installs no fresh
		// generation. Change either half - a Close that leaves the count alone,
		// or a Stop that renews the generation unconditionally - and shutdown
		// hands out a live context again.
		r.wg.Wait()
		log.Println("Runner.Stop: nothing is running")
		return
	}

	// Cancel and wait with r.mu released: a run that is finishing calls back
	// into the App, and nothing about that path may need this lock.
	cancel()
	r.wg.Wait()

	r.mu.Lock()
	r.running = 0
	r.newGenerationLocked()
	r.mu.Unlock()

	log.Println("The runner has been stopped")
}

// Close cancels the current generation, waits for its run and leaves the runner
// closed to any further one. It is shutdown, not stop.
//
// The wait is Stop's, unchanged and for the same reason: when this returns,
// nothing of the cancelled generation is still touching the mouse. What differs
// is the end - no fresh generation is installed, and closed is set - so a
// hotkey, a tray click or a StartExecution arriving while the app tears itself
// down is refused by Go instead of beginning a walk with nothing left alive to
// stop it. It is the hazard commit 33797cb fixed inside one app session, one
// level up: there, a stopped macro's nodes running inside the next run; here, a
// whole run starting after the last one was supposed to be the last.
//
// The generation is cancelled unconditionally, where Stop leaves an idle one
// alone. Stop spares it because a between-runs Context() must not hand out a
// dead context; after Close there are no more runs to hand one to, and a live
// context nothing will ever cancel is the thing to avoid instead.
//
// Idempotent: the second call finds the runner closed, has nothing to cancel
// and nothing to wait for - the first call held lifecycleMu across its own wait,
// and the only wg.Add there is refuses a closed runner under that same lock - and
// returns.
//
// The wait covers the whole walk, finishRun included: finishRun is the walk's
// last act and it is inside the goroutine wg tracks, so when this returns the
// run's final event has gone out and its final tray writes have happened. That
// is stronger than "nothing is touching the mouse" and it is load-bearing at
// shutdown - Wails' cleanup runs systray.destroy() immediately after the hook
// that calls this, so this wait is the only thing ordering the walk's
// SetMenuItemInfo calls before the menu they write to is destroyed.
//
// The wait happens on the main thread. Wails runs the shutdown hook inside its
// own InvokeSync, so the Windows message loop is parked here until the node in
// flight notices the cancel - which is what the cancellation checks between
// nodes are worth at shutdown, and it puts one requirement on the walk beyond
// the "no lock of the App's" rule in execution.go: **the walk's exit path must
// not marshal to the main thread.** It does not today - emitEvent hands off to
// Wails' own mailbox goroutine, and tray.setExecuting writes the menu items
// directly - but a finishRun that called ShowWindow or Dialog.Show, both of
// which are InvokeSync, would deadlock quit outright: the main thread waiting
// here, the walk waiting for a message loop that will never pump again.
func (r *Runner) Close() {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		log.Println("Runner.Close: the runner is already closed")
		return
	}
	r.closed = true
	cancel := r.cancel
	r.mu.Unlock()

	// Cancel and wait with r.mu released, exactly as Stop does and for the same
	// reason: a run that is finishing calls back into the App.
	cancel()
	r.wg.Wait()

	r.mu.Lock()
	r.running = 0
	r.mu.Unlock()

	log.Println("The runner has been closed")
}
