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

//=============================================== Flow Execution ===============================================

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

// newGenerationLocked installs a fresh context and task channel. Callers must
// hold q.mu.
func (q *TaskQueue) newGenerationLocked() {
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
		go q.worker(i, ctx, tasks)
	}
	log.Printf("TaskQueue started with %d workers", workerCount)
}

// Context returns the context of the current generation. It is canceled by
// Stop; a subsequent Start runs under a fresh context. Callers that need to
// observe the shutdown of one particular run must capture this once, at the
// start of that run, rather than re-reading it.
func (q *TaskQueue) Context() context.Context {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.ctx
}

// worker processes tasks from the queue. It is bound to the context and
// channel of the generation it was started for, so a later generation never
// disturbs it.
func (q *TaskQueue) worker(workerID int, ctx context.Context, tasks <-chan Task) {
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

// Enqueue adds a task to the queue. It blocks while the queue is full and
// returns early if the queue has been stopped.
func (q *TaskQueue) Enqueue(task Task) {
	q.mu.Lock()
	ctx, tasks := q.ctx, q.tasks
	q.mu.Unlock()

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
