// taskqueue.go
//
// The worker pool that drains queued tasks and reports their completion back
// to the App.

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
	// defaultWorkerCount is how many tasks the pool executes concurrently.
	defaultWorkerCount = 3
)

// TaskQueue manages the queue of tasks to be executed by a pool of workers.
//
// The queue is restartable. Start brings up a "generation": a context, a task
// channel and a set of workers that belong together. Stop cancels that
// generation, waits for its workers to exit and installs a fresh generation so
// that a later Start (a second macro run) works exactly like the first.
//
// The task channel is deliberately never closed. Closing it would race with
// Enqueue: after a close, both the send and the ctx.Done case of Enqueue's
// select are ready, Go picks a ready case at random, and the send would panic
// on a closed channel and take the process down. Context cancellation alone is
// enough to shut the workers down; the WaitGroup gives us a deterministic
// drain.
type TaskQueue struct {
	// lifecycleMu serializes whole Start/Stop operations against each other,
	// so two concurrent Stops cannot both install a new generation.
	lifecycleMu sync.Mutex

	// mu guards the generation fields below. It is only ever held briefly:
	// never across wg.Wait and never across a channel send, because workers
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

	// wg tracks the workers of the current generation.
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

// Start initializes worker goroutines to process tasks. It is a no-op if the
// queue is already running, so callers may invoke it defensively.
func (q *TaskQueue) Start(workerCount int) {
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

	for i := 0; i < workerCount; i++ {
		q.wg.Add(1)
		go q.worker(ctx, i, tasks)
	}
	log.Printf("TaskQueue started with %d workers", workerCount)
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

// worker processes tasks from the queue. It is bound to the context and
// channel of the generation it was started for, so a later generation never
// disturbs it.
func (q *TaskQueue) worker(ctx context.Context, workerID int, tasks <-chan Task) {
	defer q.wg.Done()
	log.Printf("Worker %d started", workerID)
	for {
		select {
		case <-ctx.Done():
			log.Printf("Worker %d stopping: context canceled", workerID)
			return
		case task := <-tasks:
			// Both cases can be ready at once and select picks at random, so
			// re-check cancellation before doing any real work.
			if ctx.Err() != nil {
				log.Printf("Worker %d dropping task %s: context canceled", workerID, task.ID)
				return
			}
			log.Printf("Worker %d processing task %s of type %s", workerID, task.ID, task.Type)
			q.app.emitEvent("task-started", task.ID)
			executeTask(task, q.app)
			q.app.emitEvent("task-completed", task.ID)
			// Notify task completion for dependency handling.
			q.app.notifyTaskCompletion(ctx, task.ID)
		}
	}
}

// Enqueue adds a task to the queue. It blocks while the queue is full, and
// drops the task if the queue is not running.
//
// "Not running" has to be tested with q.started, not with cancellation alone.
// Stop installs a fresh generation on its way out, so a queue that has been
// stopped is sitting on a live, uncancelled context and an empty channel with
// no workers behind it. A cancellation-only guard therefore passes, and the
// task is buffered into a generation nobody is draining - where it waits for
// the next Start and is then executed by the *next* run's workers. For this
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
//     releases q.mu across cancel and wg.Wait, because a worker finishing a
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

	// Cancel and wait with q.mu released: a worker that is finishing a task
	// calls back into the App, which can re-enter Enqueue and take q.mu.
	cancel()
	q.wg.Wait()

	q.mu.Lock()
	q.started = false
	q.newGenerationLocked()
	q.mu.Unlock()

	log.Println("TaskQueue has been stopped")
}
