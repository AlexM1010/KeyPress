// app.go

package backend

import (
	"context"
	"log"
	"sync"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Update the App struct
type App struct {
	ctx    context.Context
	runner *Runner

	// execMutex serialises whole start and stop operations against each other,
	// so that a run cannot begin while the previous one is still being torn
	// down. StopExecution holds it across the wait for the walk goroutine.
	//
	// Unlike hotkeyMux below, this one is safe to take on the main thread, and
	// ServiceShutdown does. That is not luck: nothing it is held across marshals
	// anywhere. setExecuting reaches the tray as a direct w32.SetMenuItemInfo
	// syscall on the calling goroutine, and emitEvent hands off to Wails'
	// mailbox rather than waiting on the main thread. Runner.Close's wait is the
	// one thing that blocks here, and the walk it waits for is held to the same
	// rule - see the requirement in Runner.Close.
	execMutex sync.Mutex

	// isExecuting is whether a run is in progress. Atomic rather than guarded
	// by execMutex because the walk goroutine clears it as it exits, and
	// StopExecution waits for that goroutine with execMutex held - see
	// setExecuting.
	isExecuting atomic.Bool

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
	//
	// **hotkeyMux must never be acquired from the main thread.** syncHotkeys
	// holds it across GlobalShortcut.Register and Unregister, and each of those
	// wraps its native call in Wails' InvokeSyncWithError: the calling goroutine
	// posts to the main thread and parks on a WaitGroup until the main thread
	// pumps it. So the lock can be held by a goroutine that is waiting on the
	// main thread, and a main-thread callback that waits for the lock deadlocks
	// the message loop outright - nothing else pumps it, so the holder never
	// gets to release. Everything that runs on the main thread is subject to
	// this: the shutdown hook (Wails calls it inside InvokeSync), menu and
	// shortcut callbacks, window event handlers.
	hotkeyMux sync.Mutex
	hotkeys   map[string]string
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{
		hotkeys: make(map[string]string),
		runner:  NewRunner(),
	}
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
//
// There is nothing to bring up here any more: a run starts its own goroutine in
// startFlow, and the runner is idle between runs by construction.
func (a *App) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	a.ctx = ctx
	return nil
}

// ServiceShutdown stops a run that is still going on the way out, so a macro
// does not outlive the app that started it, and leaves nothing behind that
// could start another.
//
// Close rather than Stop, because there is no next run to keep the runner ready
// for: a stop leaves it startable on purpose, and a hotkey or a tray click
// landing during teardown would take it up on that and begin a walk nothing
// would be left to stop. See Runner.Close.
//
// The global shortcuts are deliberately not released here, and releasing them
// here would be both useless and fatal. Useless because Wails' own cleanup calls
// GlobalShortcut.UnregisterAll() before the InvokeSync that reaches this hook,
// so by the time this runs every chord is already released and a second pass
// would do nothing but log one "not registered" error per hotkey on every clean
// exit. Fatal because this hook runs on the main thread - it is inside that
// InvokeSync - and taking hotkeyMux there deadlocks quit: see the invariant on
// hotkeyMux in the App struct. What actually closes the window in which a chord
// can start a run is the refusal in Runner.Go, which covers the tray menu and
// the frontend too.
//
// It takes execMutex for the same reason StopExecution does: a run that is
// halfway through startFlow must finish launching or be refused, not be left
// half-started by a shutdown that lands in the middle of it. That one is safe to
// take on the main thread precisely where hotkeyMux is not - see its
// declaration.
func (a *App) ServiceShutdown() error {
	a.execMutex.Lock()
	defer a.execMutex.Unlock()

	a.runner.Close()
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
