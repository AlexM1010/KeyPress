// events.go
//
// Wails runtime event plumbing: the single place the backend talks to the
// frontend event bus.

package main

import (
	"log"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// emitEvent emits an event to the frontend.
//
// a.ctx is only set in startup. The Wails runtime reacts to a nil context with
// log.Fatalf, which would terminate the whole process, so drop the event
// instead: there is no frontend listening before startup anyway.
func (a *App) emitEvent(event string, payload interface{}) {
	if a.ctx == nil {
		log.Printf("Dropping event %q: frontend runtime not initialized yet", event)
		return
	}
	runtime.EventsEmit(a.ctx, event, payload)
}
