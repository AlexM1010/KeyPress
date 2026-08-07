// tray.go
//
// The system tray icon and menu. This is what keeps Keypress usable with its
// window closed: the app does not quit when the window goes away, and the tray
// is how the user starts a macro, stops one, gets the window back, and finally
// exits.

package backend

import (
	"fmt"
	"log"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// maxTrayMacros bounds the "Run macro" submenu.
//
// The slots are created once and then relabelled, because that is the only way
// to change this menu that Wails supports while the app is running - items can
// be shown, hidden and renamed in place, but a menu cannot grow new ones. A
// fixed pool also keeps a data directory with hundreds of macros from turning
// the tray into an unusable wall of text. Anything past the limit is reachable
// from the window, and the menu says so rather than silently truncating.
const maxTrayMacros = 20

// tray owns the system tray icon and its menu.
//
// Every method is safe to call on a nil receiver. The App builds a tray in Run
// but not in tests, and callers scattered through the execution engine should
// not each have to remember that.
type tray struct {
	app     *App
	systray *application.SystemTray
	root    *application.Menu

	// macroSlots are the reusable "Run macro" entries, in menu order. Their
	// click handlers are bound once, to a slot index rather than to a macro, so
	// that a refresh only has to rewrite labels and never rebinds a callback
	// underneath a click that is already happening.
	macroSlots []*application.MenuItem

	// emptyItem stands in for the macro list when there are no macros to show,
	// and overflowItem appears when there are more than the menu can hold.
	emptyItem    *application.MenuItem
	overflowItem *application.MenuItem

	stopItem *application.MenuItem

	// mu guards macroIDs, which the refresh writes and a menu click reads.
	mu       sync.Mutex
	macroIDs []string
}

// newTray builds the tray icon and its menu.
//
// It is called before the application runs. Wails defers the actual icon
// creation until then, so everything set up here is in place the moment the
// icon appears.
func newTray(app *App, icon []byte) *tray {
	t := &tray{
		app:     app,
		systray: app.wails.SystemTray.New(),
		root:    application.NewMenu(),
	}

	if len(icon) > 0 {
		t.systray.SetIcon(icon)
	}
	t.systray.SetTooltip("Keypress")

	t.root.Add("Open Keypress").OnClick(func(*application.Context) {
		app.ShowWindow()
	})
	t.root.AddSeparator()

	macros := t.root.AddSubmenu("Run macro")
	t.macroSlots = make([]*application.MenuItem, maxTrayMacros)
	for i := range t.macroSlots {
		index := i
		item := macros.Add("")
		item.SetHidden(true)
		item.OnClick(func(*application.Context) {
			t.runSlot(index)
		})
		t.macroSlots[i] = item
	}
	// Both of these are placeholders for a state the macro list might be in,
	// so both start hidden and refresh decides which - if either - applies.
	t.emptyItem = macros.Add("No saved macros").SetEnabled(false)
	t.emptyItem.SetHidden(true)
	t.overflowItem = macros.Add(fmt.Sprintf("More than %d macros - open Keypress to run the rest", maxTrayMacros)).SetEnabled(false)
	t.overflowItem.SetHidden(true)

	t.stopItem = t.root.Add("Stop macro").SetEnabled(false)
	t.stopItem.OnClick(func(*application.Context) {
		app.StopExecution()
	})

	t.root.AddSeparator()
	t.root.Add("Quit Keypress").OnClick(func(*application.Context) {
		app.wails.Quit()
	})

	t.systray.SetMenu(t.root)

	// With no window attached, a left click has no default meaning. Opening the
	// window is what a user who clicked the icon almost always wants; the menu
	// stays on the right click, where Windows users expect it.
	t.systray.OnClick(func() {
		app.ShowWindow()
	})

	t.refreshMacros()
	return t
}

// refreshMacros rewrites the "Run macro" submenu from the macros on disk.
//
// A failed listing leaves the menu exactly as it was rather than emptying it:
// a transient read error should not make every macro look deleted.
func (t *tray) refreshMacros() {
	if t == nil {
		return
	}

	summaries, err := t.app.ListProjects()
	if err != nil {
		log.Printf("tray: keeping the previous macro list: %v", err)
		return
	}

	shown := summaries
	if len(shown) > maxTrayMacros {
		shown = shown[:maxTrayMacros]
	}

	ids := make([]string, len(shown))
	for i, summary := range shown {
		ids[i] = summary.ID
	}

	t.mu.Lock()
	t.macroIDs = ids
	t.mu.Unlock()

	for i, item := range t.macroSlots {
		if i >= len(shown) {
			item.SetHidden(true)
			continue
		}
		item.SetLabel(trayMacroLabel(shown[i]))
		item.SetHidden(false)
	}

	t.emptyItem.SetHidden(len(shown) > 0)
	t.overflowItem.SetHidden(len(summaries) <= maxTrayMacros)
}

// trayMacroLabel is how a macro is named in the tray menu: its name, plus its
// hotkey when it has one, so the menu doubles as the reminder of what that
// hotkey is.
func trayMacroLabel(summary ProjectSummary) string {
	if summary.Hotkey == "" {
		return summary.Name
	}
	return fmt.Sprintf("%s\t%s", summary.Name, summary.Hotkey)
}

// runSlot starts the macro currently occupying a menu slot.
//
// The slot is resolved to a macro at click time rather than captured when the
// handler was bound, so a menu that has been refreshed since always runs the
// macro the user is actually looking at.
func (t *tray) runSlot(index int) {
	t.mu.Lock()
	if index >= len(t.macroIDs) {
		t.mu.Unlock()
		return
	}
	id := t.macroIDs[index]
	t.mu.Unlock()

	t.app.runFromTray(id)
}

// setExecuting reflects a run starting or finishing in the menu: Stop only
// means anything while something is running, and starting a second macro on
// top of the first is refused by the engine anyway, so the menu says so up
// front instead of letting the user click into an error.
func (t *tray) setExecuting(running bool) {
	if t == nil {
		return
	}
	t.stopItem.SetEnabled(running)
	for _, item := range t.macroSlots {
		item.SetEnabled(!running)
	}
}

// runFromTray starts a macro on behalf of the tray or a global hotkey and
// reports any failure in a dialog.
//
// A dialog rather than an event, because both callers work with the window
// closed: an event would go to a frontend nobody can see, and the user would
// be left pressing a hotkey that silently does nothing.
func (a *App) runFromTray(id string) {
	if err := a.RunMacro(id); err != nil {
		log.Printf("runFromTray %q: %v", id, err)
		if a.wails == nil {
			return
		}
		a.wails.Dialog.Error().
			SetTitle("Keypress").
			SetMessage(fmt.Sprintf("Could not run this macro: %v", err)).
			Show()
	}
}
