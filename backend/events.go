// events.go
//
// Wails runtime event plumbing: the single place the backend talks to the
// frontend event bus.

package backend

import (
	"log"
)

// emitEvent emits an event to the frontend.
//
// a.wails is only set by attach, just before the application runs, and the
// Wails event manager is not usable before that - so drop the event instead of
// dereferencing a nil application. Nothing is listening that early anyway.
//
// Note that an event emitted while the window is hidden is not lost: the
// webview is still alive behind it, so a macro started from the tray or a
// hotkey lights its nodes up exactly as it would with the window on screen.
func (a *App) emitEvent(event string, payload interface{}) {
	if a.wails == nil {
		log.Printf("Dropping event %q: application not initialized yet", event)
		return
	}
	a.wails.Event.Emit(event, payload)
}
