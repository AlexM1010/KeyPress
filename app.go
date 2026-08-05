// app.go

package main

import (
	"context"
	"sync"
)

// notifyBufferSize is the capacity of the completion-notification channel.
// It is generously sized on purpose: workers block until a completion is
// accepted (dropping one would stall that branch of the dependency graph
// forever), and a blocking send could in theory deadlock against a full task
// channel. With a buffer this much larger than taskBufferSize, that would
// require a flowchart with thousands of simultaneously in-flight nodes.
const notifyBufferSize = 4096

// Update the App struct
type App struct {
	ctx         context.Context
	taskQueue   *TaskQueue
	isExecuting bool
	execMutex   sync.Mutex

	// completedMux guards completed.
	completed    map[string]bool
	completedMux sync.Mutex

	// graphMux guards nodeMap and dependencies. They are rebuilt by
	// StartExecution and read by the handleCompletions goroutine, so they
	// need their own lock; completedMux does not cover them.
	graphMux     sync.RWMutex
	dependencies map[string][]string
	nodeMap      map[string]Node

	notifyCh chan string
}

// NewApp creates a new App application struct
func NewApp() *App {
	a := &App{
		completed:    make(map[string]bool),
		notifyCh:     make(chan string, notifyBufferSize),
		dependencies: make(map[string][]string),
		nodeMap:      make(map[string]Node),
	}
	// Wire the queue to the App here rather than in startup, so a task can
	// never be processed while the queue's app pointer is still nil.
	a.taskQueue = NewTaskQueue(a, taskBufferSize)
	return a
}

// Initialize auth state on startup
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.taskQueue.Start(defaultWorkerCount)
}
