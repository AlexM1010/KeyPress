// app.go

package backend

import (
	"context"
	"log"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
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

	// wails and window are the handles the tray and the hotkeys need to show
	// the window and talk to the frontend. They are set by attach, before the
	// application runs, and never written again.
	wails  *application.App
	window application.Window

	// tray is the system tray this app is reachable through while its window
	// is closed. Nil only in tests, which never build one, so every use goes
	// through the nil-tolerant helpers on *tray.
	tray *tray

	// hotkeys maps a macro id to the accelerator currently registered for it,
	// mirroring what is in the settings file. It is the record of what has to
	// be released before a macro's hotkey can be changed - the shortcut
	// manager is keyed by accelerator and cannot answer "what does this macro
	// have?". hotkeyMux guards it.
	hotkeyMux sync.Mutex
	hotkeys   map[string]string
}

// NewApp creates a new App application struct
func NewApp() *App {
	a := &App{
		completed:    make(map[string]bool),
		notifyCh:     make(chan string, notifyBufferSize),
		dependencies: make(map[string][]string),
		nodeMap:      make(map[string]Node),
		hotkeys:      make(map[string]string),
	}
	// Wire the queue to the App here rather than in startup, so a task can
	// never be processed while the queue's app pointer is still nil.
	a.taskQueue = NewTaskQueue(a, taskBufferSize)
	return a
}

// attach hands the App the application and window it was built for. It is
// called from Run before the application starts, so nothing is reading these
// fields yet and they need no lock.
func (a *App) attach(wailsApp *application.App, window application.Window) {
	a.wails = wailsApp
	a.window = window
}

// ServiceName names this service in Wails' logs.
func (a *App) ServiceName() string {
	return "Keypress"
}

// ServiceStartup is Wails' startup hook for a bound service. It replaces the
// v2 OnStartup callback.
func (a *App) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	a.ctx = ctx
	a.taskQueue.Start(defaultWorkerCount)
	return nil
}

// ServiceShutdown stops the worker pool on the way out, so a macro that is
// still running does not outlive the app that started it.
func (a *App) ServiceShutdown() error {
	a.taskQueue.Stop()
	a.setExecuting(false)
	return nil
}

// ShowWindow brings the main window back into view. It is what the tray's
// "Open Keypress" item does, and what a hotkey that wants the window uses.
//
// Show alone is not enough on Windows: a window that was hidden while
// minimised comes back minimised, and one that comes back behind whatever the
// user is now working in is easy to miss entirely.
func (a *App) ShowWindow() {
	if a.window == nil {
		log.Println("ShowWindow: no window to show")
		return
	}
	a.window.Show()
	a.window.Restore()
	a.window.Focus()
}
