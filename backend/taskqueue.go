// taskqueue.go
//
// The dispatcher that drains queued tasks, runs each one, and reports their
// completion back to the App.

package backend

import (
	"context"
	"log"
	"sync"
)

// =============================================== Flow Execution ===============================================

const (
	// taskBufferSize is the capacity of the queue's task channel.
	taskBufferSize = 100

	// unlimitedWorkers runs every task the flow makes ready at once, which is
	// what the app uses.
	//
	// A fixed pool was the wrong shape for this app. Mouse tasks are serialised
	// by mouseMu regardless (robotgo's MouseSleep is a package global, and there
	// is only one cursor), so a cap never bought them anything; what it did do
	// was let long waits block short work. A ColorPickerNode polls for up to
	// thirty seconds by default and deliberately does not hold mouseMu, so under
	// the old three-worker pool three of them stalled an entire macro while
	// doing nothing but sleeping.
	//
	// Unlimited is not unbounded in practice. handleCompletions only enqueues a
	// task once its dependencies have finished, so what can run at once is the
	// width of the user's flow graph at that point - the parallelism is the one
	// they drew, rather than a constant they cannot see.
	//
	// Negative rather than zero, which already means something: Start(0) ran no
	// workers when this was a fixed pool, and a test relies on it to hold a
	// queue full. Spelling unlimited as 0 would have inverted that silently.
	unlimitedWorkers = -1
)

// TaskQueue manages the queue of tasks and the goroutines that run them.
//
// The queue is restartable. Start brings up a "generation": a context, a task
// channel and a dispatcher that belong together. Stop cancels that
// generation, waits for its work to finish and installs a fresh generation so
// that a later Start (a second macro run) works exactly like the first.
//
// The task channel is deliberately never closed. Closing it would race with
// Enqueue: after a close, both the send and the ctx.Done case of Enqueue's
// select are ready, Go picks a ready case at random, and the send would panic
// on a closed channel and take the process down. Context cancellation alone is
// enough to shut the dispatcher down; the WaitGroup gives us a deterministic
// drain.
type TaskQueue struct {
	// lifecycleMu serializes whole Start/Stop operations against each other,
	// so two concurrent Stops cannot both install a new generation.
	lifecycleMu sync.Mutex

	// mu guards the generation fields below. It is only ever held briefly:
	// never across wg.Wait and never across a channel send, because a task
	// can call back into the App and from there into Enqueue.
	mu      sync.Mutex
	tasks   chan Task
	ctx     context.Context
	cancel  context.CancelFunc
	started bool

	// gen identifies the current generation. Enqueue takes it as a token so a
	// caller can say which generation it means, rather than only "whichever is
	// current" - see Enqueue. NewTaskQueue installs the first generation, so a
	// live token is always >= 1 and the zero value is permanently stale.
	gen uint64

	// wg tracks the dispatcher of the current generation, which in turn does
	// not report done until the tasks it started have finished.
	wg sync.WaitGroup

	// bufferSize and app are immutable after construction.
	bufferSize int
	app        *App
}

// NewTaskQueue initializes a new TaskQueue.
func NewTaskQueue(app *App, bufferSize int) *TaskQueue {
	q := &TaskQueue{
		bufferSize: bufferSize,
		app:        app,
	}
	q.mu.Lock()
	q.newGenerationLocked()
	q.mu.Unlock()
	return q
}

// newGenerationLocked installs a fresh context and task channel, and retires
// every token issued for the previous one. Callers must hold q.mu.
func (q *TaskQueue) newGenerationLocked() {
	q.gen++
	q.ctx, q.cancel = context.WithCancel(context.Background())
	q.tasks = make(chan Task, q.bufferSize)
}

// Start brings up the generation's dispatcher. It is a no-op if the queue is
// already running, so callers may invoke it defensively.
//
// limit is the most tasks that may run at once. unlimitedWorkers - anything
// negative - means no limit, which is what the app always passes. A positive
// limit is for the tests that need a task to be the only one running, which is
// how they catch a task leaked out of a stopped generation; zero runs nothing
// at all, which is how one of them keeps a queue full.
func (q *TaskQueue) Start(limit int) {
	q.lifecycleMu.Lock()
	defer q.lifecycleMu.Unlock()

	q.mu.Lock()
	if q.started {
		q.mu.Unlock()
		return
	}
	q.started = true
	ctx, tasks := q.ctx, q.tasks
	q.mu.Unlock()

	// Only the dispatcher joins q.wg, and it does so here, before any Stop can
	// be waiting on it. The tasks it spawns are tracked by a WaitGroup of its
	// own - adding them to q.wg would be an Add racing a Wait.
	q.wg.Add(1)
	go q.dispatch(ctx, tasks, limit)

	if limit >= 0 {
		log.Printf("TaskQueue started with a limit of %d concurrent tasks", limit)
	} else {
		log.Print("TaskQueue started with no concurrency limit")
	}
}

// Context returns the context of the current generation. It is canceled by
// Stop; a subsequent Start runs under a fresh context. Callers that need to
// observe the shutdown of one particular run must capture this once, at the
// start of that run, rather than re-reading it.
func (q *TaskQueue) Context() context.Context {
	_, ctx := q.Current()
	return ctx
}

// Current returns the token and the context of the current generation.
//
// Both in one call, and read under a single acquisition of q.mu, because a run
// captures them together at its start and they must describe the same
// generation: taking them from two separate calls leaves a window where Stop
// lands in between and the run watches one generation while enqueueing into
// another - exactly the confusion the token exists to prevent.
func (q *TaskQueue) Current() (uint64, context.Context) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.gen, q.ctx
}

// dispatch takes tasks off the queue and runs each in its own goroutine. It is
// bound to the context and channel of the generation it was started for, so a
// later generation never disturbs it.
//
// The deferred order matters and is the reason this reports done last: defers
// run last-registered-first, so running holds the dispatcher here until every
// task it started has returned, and only then is q.wg satisfied. That is what
// keeps Stop's promise - when Stop returns, nothing of the stopped generation
// is still touching the mouse.
func (q *TaskQueue) dispatch(ctx context.Context, tasks <-chan Task, limit int) {
	defer q.wg.Done()

	var running sync.WaitGroup
	defer running.Wait()

	// A counting semaphore: a slot is taken before a task starts and returned
	// when it finishes. nil when unlimited, so the acquire below is skipped.
	//
	// A limit of zero deliberately makes this an *unbuffered* channel, which no
	// one ever receives from, so the acquire parks the dispatcher for good and
	// nothing runs. That is what Start(0) meant when this was a fixed pool, and
	// a test still uses it to keep a queue full.
	var slots chan struct{}
	if limit >= 0 {
		slots = make(chan struct{}, limit)
	}

	for {
		// The slot is taken *before* a task is dequeued, not after. Taking it
		// after would mean a full queue still drains into a dispatcher that
		// cannot start the work, which loses the backpressure Enqueue relies on
		// - and with a limit of zero would empty the buffer while running
		// nothing, rather than leaving it full.
		if slots != nil {
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				log.Print("Dispatcher stopping: context canceled")
				return
			}
		}

		select {
		case <-ctx.Done():
			log.Print("Dispatcher stopping: context canceled")
			return
		case task := <-tasks:
			// Both cases can be ready at once and select picks at random, so
			// re-check cancellation before doing any real work.
			if ctx.Err() != nil {
				log.Printf("Dispatcher dropping task %s: context canceled", task.ID)
				return
			}

			running.Add(1)
			go func(task Task) {
				defer running.Done()
				if slots != nil {
					defer func() { <-slots }()
				}
				q.run(ctx, task)
			}(task)
		}
	}
}

// run executes one task and reports it, in the goroutine dispatch gave it.
func (q *TaskQueue) run(ctx context.Context, task Task) {
	log.Printf("Processing task %s of type %s", task.ID, task.Type)
	q.app.emitEvent("task-started", task.ID)
	executeTask(task, q.app)
	q.app.emitEvent("task-completed", task.ID)
	// Notify task completion for dependency handling.
	q.app.notifyTaskCompletion(ctx, task.ID)
}

// Enqueue adds a task to the queue. It blocks while the queue is full, and
// drops the task if the queue is not running.
//
// "Not running" has to be tested with q.started, not with cancellation alone.
// Stop installs a fresh generation on its way out, so a queue that has been
// stopped is sitting on a live, uncancelled context and an empty channel with
// nothing draining it. A cancellation-only guard therefore passes, and the
// task is buffered into a generation nobody is draining - where it waits for
// the next Start and is then executed by the *next* run. For this
// app that is a mouse click or a keystroke the user never asked for. The
// caller that gets there is App.handleCompletions: it is an untracked
// goroutine, so wg.Wait does not cover it, and once it has taken a completion
// off notifyCh it runs the rest of the loop body - enqueues included - without
// re-checking its context.
//
// Not fixed by making Stop leave the generation cancelled and letting Start
// install the fresh one: Context() is the handle a run watches for shutdown,
// and a queue that reports a done context while idle would make every
// between-runs caller of it look like it was cancelled. Nor by closing the
// stopped channel - see the type comment for why that panics.
//
// The token is what makes that airtight, and q.started alone is not enough.
// started answers "is the queue running now", which cannot distinguish a
// legitimate enqueue by the current run from a straggler of a previous one:
// once the user has stopped a macro and started another, started is true again
// and the context is live, so a stale caller sails through both guards. Only
// the caller's own generation says which run it belongs to. gen is compared
// rather than the channel itself so a stale token is refused rather than
// silently writing into a discarded buffer.
//
// Both guards are kept because each covers a case the other does not: the
// token catches a caller from a retired generation, and started catches a
// caller holding the *current* token across a Stop, when the token is still
// current but nothing is draining the channel.
//
// gen, started, ctx and tasks are read in one critical section so the decision
// is made against a single generation. Two interleavings to be sure of:
//
//   - Enqueue lands between Stop's cancel() and its q.started = false (Stop
//     releases q.mu across cancel and wg.Wait, because a task finishing a
//     task can re-enter Enqueue). It sees started == true together with the
//     old generation, so it either observes the cancelled context and refuses,
//     or wins the race against cancel() and sends into the old channel, which
//     newGenerationLocked is about to discard. Both are fine: the task was
//     submitted while that run was still live, and it dies with it.
//   - Two Enqueues racing one Stop. Each takes q.mu separately and so gets its
//     own consistent triple; they may land on opposite sides of the Stop.
//     Neither can reach the fresh generation, because started is false from
//     the moment newGenerationLocked publishes it until the next Start, and
//     both are published under q.mu.
func (q *TaskQueue) Enqueue(gen uint64, task Task) {
	q.mu.Lock()
	current, started, ctx, tasks := q.gen, q.started, q.ctx, q.tasks
	q.mu.Unlock()

	if gen != current {
		log.Printf("Discarding task %s from generation %d: the queue is on generation %d",
			task.ID, gen, current)
		return
	}

	if !started {
		log.Println("Task queue is stopped. Cannot enqueue task:", task.ID)
		return
	}

	// Prefer cancellation: if the queue is already stopped, do not let a
	// random select pick the (still writable) channel of a dead generation.
	select {
	case <-ctx.Done():
		log.Println("Task queue is stopped. Cannot enqueue task:", task.ID)
		return
	default:
	}

	select {
	case tasks <- task:
		log.Printf("Enqueued task %s of type %s", task.ID, task.Type)
	case <-ctx.Done():
		log.Println("Task queue is stopped. Cannot enqueue task:", task.ID)
	}
}

// Stop gracefully shuts down the task queue and prepares it to be started
// again.
func (q *TaskQueue) Stop() {
	q.lifecycleMu.Lock()
	defer q.lifecycleMu.Unlock()

	q.mu.Lock()
	if !q.started {
		q.mu.Unlock()
		return
	}
	cancel := q.cancel
	q.mu.Unlock()

	// Cancel and wait with q.mu released: a task that is finishing
	// calls back into the App, which can re-enter Enqueue and take q.mu.
	cancel()
	q.wg.Wait()

	q.mu.Lock()
	q.started = false
	q.newGenerationLocked()
	q.mu.Unlock()

	log.Println("TaskQueue has been stopped")
}
