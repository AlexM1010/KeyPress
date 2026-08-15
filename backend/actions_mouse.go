// actions_mouse.go
//
// Handlers for the mouse node types: movement (with optional drag) and
// clicking/scrolling.

package backend

import (
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/go-vgo/robotgo"
)

// defaultMouseSleep is the value robotgo.MouseSleep is restored to once a
// mouse task is done with it.
const defaultMouseSleep = 100

// mouseMu guards the robotgo.MouseSleep package-level global and every robotgo
// mouse call that reads it (Move, MoveSmooth, Click, Toggle, Scroll, ...).
//
// Up to defaultWorkerCount tasks run at once, so without this two mouse tasks
// would interleave their "set speed / move / restore speed" sequences: one
// task's restore-to-default clobbers another's live setting, and the concurrent
// read/write of the global is a data race. Holding it for the whole handler
// makes each mouse task atomic with respect to other mouse tasks. Keyboard and
// delay tasks are unaffected and still run concurrently.
var mouseMu sync.Mutex

// numberIn reads a numeric field out of a decoded node payload.
//
// Every number in a saved macro arrives as a float64, because that is what
// encoding/json decodes a JSON number into - there is no integer case to
// handle, and asserting to one would fail on every value. This exists so the
// assertion is written once rather than at each field, and so a missing field
// and a field of the wrong type are the same answer to the caller: not usable.
func numberIn(data map[string]interface{}, key string) (float64, bool) {
	value, ok := data[key].(float64)
	return value, ok
}

// mouseMoveFailed reports a mouse-move node that could not be read, in the
// shape the frontend's status panel already listens for.
//
// Every one of these used to be five lines repeated inline, which is most of
// why the movement settings below were left as bare assertions instead: the
// cost of checking a field was a paragraph. It is a line now.
func mouseMoveFailed(task Task, app *App, reason string) {
	log.Printf("MoveMouse error: %s for task %s", reason, task.ID)
	app.emitEvent("task-error", map[string]interface{}{
		"taskID": task.ID,
		"error":  reason,
	})
}

// resolvePosition turns one end of a mouse-move node into screen coordinates,
// reporting false if the node does not carry usable ones.
//
// `currentX` / `currentY` are passed in rather than read here, and that is the
// point: a "Mouse" position means where the cursor was when the task *started*,
// so both ends have to resolve against one reading taken before anything moves.
// Reading the live location inside here would make an end-position of "Mouse"
// mean "wherever the start move just put it", which is a different macro.
func resolvePosition(pos map[string]interface{}, currentX, currentY int) (float64, float64, bool) {
	if pos["type"] == "Mouse" {
		return float64(currentX), float64(currentY), true
	}

	coords, ok := pos["coordinates"].(map[string]interface{})
	if !ok {
		return 0, 0, false
	}

	x, okX := numberIn(coords, "x")
	y, okY := numberIn(coords, "y")
	if !okX || !okY {
		return 0, 0, false
	}

	return x, y, true
}

// mouseMoveSettings is everything a mouse-move node says about *how* to move,
// as opposed to where.
type mouseMoveSettings struct {
	speedType       string
	speedValue      float64
	randomize       bool
	variance        float64
	pathType        string
	dragWhileMoving bool
}

// readMouseMoveSettings pulls those settings out of a decoded node payload,
// returning the reason it could not as a non-empty string.
//
// A function rather than a run of assertions inside the handler, because the
// handler moves the real mouse and so cannot be unit tested, while this is the
// half that actually gets a saved macro wrong. It used to be six bare type
// assertions: a node missing any of these fields - which a hand-edited file, or
// a macro saved before a field existed, can easily be - panicked mid-run.
// `executeTask` recovers, so the app survived, but the user got "panic:
// interface conversion" with nothing saying which field was at fault.
//
// The line between "report it" and "fall back" is whether the run can mean
// anything without the field. A speed or a path type cannot be guessed. But
// `randomize` and `variance` only matter when randomisation is on, and
// `dragWhileMoving` absent plainly means no drag, so those three take a default
// rather than failing a macro that predates them.
func readMouseMoveSettings(data map[string]interface{}) (mouseMoveSettings, string) {
	speed, ok := data["speed"].(map[string]interface{})
	if !ok {
		return mouseMoveSettings{}, "Invalid or missing speed settings"
	}

	speedType, ok := speed["type"].(string)
	if !ok {
		return mouseMoveSettings{}, "Invalid or missing speed type"
	}

	speedValue, ok := numberIn(speed, "value")
	if !ok {
		return mouseMoveSettings{}, "Invalid or missing speed value"
	}

	pathType, ok := data["pathType"].(string)
	if !ok {
		return mouseMoveSettings{}, "Invalid or missing path type"
	}

	randomize, _ := speed["randomize"].(bool)
	variance, hasVariance := numberIn(speed, "variance")
	if !hasVariance {
		variance = 0
	}
	dragWhileMoving, _ := data["dragWhileMoving"].(bool)

	return mouseMoveSettings{
		speedType:       speedType,
		speedValue:      speedValue,
		randomize:       randomize,
		variance:        variance,
		pathType:        pathType,
		dragWhileMoving: dragWhileMoving,
	}, ""
}

// executeMouseMoveTask moves the cursor between the configured positions.
func executeMouseMoveTask(task Task, app *App) {
	log.Printf("MoveMouse task starting - Data: %+v", task.Data)

	mouseMu.Lock()
	defer mouseMu.Unlock()
	// Deferred (so it also runs on the error paths below) and registered after
	// the Unlock defer, so it runs before the mutex is released.
	defer func() { robotgo.MouseSleep = defaultMouseSleep }()

	// Extract and validate position configurations
	startPos, ok1 := task.Data["startPosition"].(map[string]interface{})
	endPos, ok2 := task.Data["endPosition"].(map[string]interface{})
	if !ok1 || !ok2 {
		mouseMoveFailed(task, app, "Invalid or missing position configurations")
		return
	}

	// Get current mouse position for 'Mouse' type positions
	currentX, currentY := robotgo.Location()

	// Resolve both ends and read the settings *before* anything moves.
	//
	// Nothing below this point can refuse the run, and that ordering is the whole
	// intent: the move to the start position used to happen between resolving the
	// two ends, so a node with an unreadable end position or an unreadable speed
	// reported its error only after it had already yanked the user's cursor
	// across the screen. A task that is going to fail should not move the mouse
	// first.
	startX, startY, ok := resolvePosition(startPos, currentX, currentY)
	if !ok {
		mouseMoveFailed(task, app, "Invalid start coordinates")
		return
	}

	endX, endY, ok := resolvePosition(endPos, currentX, currentY)
	if !ok {
		mouseMoveFailed(task, app, "Invalid end coordinates")
		return
	}

	// Read as one unit so the reading can be tested without a mouse attached -
	// see `readMouseMoveSettings`.
	settings, reason := readMouseMoveSettings(task.Data)
	if reason != "" {
		mouseMoveFailed(task, app, reason)
		return
	}
	speedType := settings.speedType
	speedValue := settings.speedValue
	randomize := settings.randomize
	variance := settings.variance
	pathType := settings.pathType
	dragWhileMoving := settings.dragWhileMoving

	// Move to the start position. Deliberately still ahead of the MouseSleep
	// assignment below: this is repositioning, not the movement the node
	// describes, so it runs at the default speed rather than the configured one -
	// which is what it did when it sat higher up, and is not something the
	// reordering should quietly change.
	robotgo.Move(int(startX), int(startY))

	// Calculate final speed with randomization if enabled
	finalSpeed := speedValue
	if randomize {
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		varianceAmount := speedValue * (variance / 100.0)
		finalSpeed += (r.Float64()*2 - 1) * varianceAmount
	}

	// Set mouse movement speed based on configuration
	robotgo.MouseSleep = int(finalSpeed) // Convert to appropriate sleep value

	// Start drag if required.
	//
	// The release is deferred as well as done explicitly below, and that is the
	// point rather than belt-and-braces: between here and there the handler
	// moves the cursor, and if any of that panics - or a later edit adds an
	// early return - the left button stays physically down after the task ends.
	// `executeTask` recovers from the panic, so the app keeps running and the
	// user is left holding a drag they never started, over whatever window
	// happens to be under the cursor, with no way to end it but to click. A
	// deferred release costs nothing and cannot leave that behind.
	dragging := false
	defer func() {
		if dragging {
			if err := robotgo.MouseUp("left"); err != nil {
				log.Printf("MouseUp error during cleanup: %v for task %s", err, task.ID)
			}
		}
	}()

	if dragWhileMoving {
		if err := robotgo.MouseDown("left"); err != nil {
			log.Printf("MouseDown error: %v for task %s", err, task.ID)
			app.emitEvent("task-error", map[string]interface{}{
				"taskID": task.ID,
				"error":  fmt.Sprintf("MouseDown failed: %v", err),
			})
			return
		}
		dragging = true
	}

	// Execute movement based on configuration
	if speedType == "Instant" {
		robotgo.Move(int(endX), int(endY))
	} else {
		mouseDelay := int(finalSpeed)
		if pathType == "Straight" {
			// Use MoveSmooth with minimal randomization for straight path TODO: use relative?
			success := robotgo.MoveSmooth(int(endX), int(endY), 1.0, 1.2, mouseDelay)
			if !success {
				log.Printf("MoveSmooth failed for task %s", task.ID)
				// Fallback to regular move
				robotgo.Move(int(endX), int(endY))
			}
		} else {
			// Use MoveSmooth with more randomization for human-like movement
			success := robotgo.MoveSmooth(int(endX), int(endY), 1.0, 2.0, mouseDelay)
			if !success {
				log.Printf("MoveSmooth failed for task %s", task.ID)
				// Fallback to regular move
				robotgo.Move(int(endX), int(endY))
			}
		}
	}

	// Release drag if active. Reported here, where a failure is worth telling
	// the user about; the deferred release above is only a backstop and stays
	// quiet once this has run.
	if dragging {
		dragging = false
		if err := robotgo.MouseUp("left"); err != nil {
			log.Printf("MouseUp error: %v for task %s", err, task.ID)
			app.emitEvent("task-error", map[string]interface{}{
				"taskID": task.ID,
				"error":  fmt.Sprintf("MouseUp failed: %v", err),
			})
			return
		}
	}

	// robotgo.MouseSleep is reset to its default by the deferred restore above.

	app.emitEvent("task-success", map[string]interface{}{
		"taskID": task.ID,
		"type":   "MoveMouse",
	})
}

// executeMouseClickTask performs the configured clicks and scrolling.
func executeMouseClickTask(task Task, app *App) {
	log.Printf("Click task starting - Data: %+v", task.Data)

	// robotgo.Click/Toggle/Scroll all read the MouseSleep global, so clicking
	// has to take the same lock as moving.
	mouseMu.Lock()
	defer mouseMu.Unlock()

	// Get buttonType
	buttonType, ok := task.Data["buttonType"].(string)
	if !ok {
		err := fmt.Sprintf("Invalid buttonType value: %v", task.Data["buttonType"])
		log.Printf("Click error: %s for task %s", err, task.ID)
		app.emitEvent("task-error", map[string]interface{}{
			"taskID": task.ID,
			"error":  err,
		})
		return
	}

	// Map buttonType to robotgo button
	if buttonType != "left" && buttonType != "right" && buttonType != "middle" {
		err := fmt.Sprintf("Unsupported buttonType: %s", buttonType)
		log.Printf("Click error: %s for task %s", err, task.ID)
		app.emitEvent("task-error", map[string]interface{}{
			"taskID": task.ID,
			"error":  err,
		})
		return
	}

	// Get numberOfClicks
	numberOfClicks, ok := task.Data["numberOfClicks"].(float64)
	if !ok {
		err := fmt.Sprintf("Invalid numberOfClicks value: %v", task.Data["numberOfClicks"])
		log.Printf("Click error: %s for task %s", err, task.ID)
		app.emitEvent("task-error", map[string]interface{}{
			"taskID": task.ID,
			"error":  err,
		})
		return
	}

	// Get clickDelay. Both of these are in **milliseconds** - that is what the
	// node's TimeInput stores whatever unit it happens to be displaying - so the
	// fallback is 100, not 0.1. It read 0.1 with a comment claiming 100ms, which
	// is the same number the seconds-based reading of the field would want and
	// exactly 1000x too small for the field as it is actually stored: 0.1ms is no
	// pause at all, so a node that fell back to it fired its clicks as fast as
	// the OS would take them.
	clickDelay, ok := task.Data["clickDelay"].(float64)
	if !ok {
		clickDelay = 100
	}
	clickDuration := time.Duration(clickDelay) * time.Millisecond

	// Get pressReleaseDelay and releaseAfterPress
	pressReleaseDelay, ok := task.Data["pressReleaseDelay"].(float64)
	if !ok {
		pressReleaseDelay = 100
	}
	pressDuration := time.Duration(pressReleaseDelay) * time.Millisecond

	releaseAfterPress, _ := task.Data["releaseAfterPress"].(bool)

	// Perform the click actions
	if numberOfClicks > 0 {
		log.Printf("Performing %v clicks with %v delay and %v press duration",
			numberOfClicks, clickDuration, pressDuration)

		for i := 0; i < int(numberOfClicks); i++ {
			if releaseAfterPress {
				robotgo.MouseDown(buttonType)
				time.Sleep(pressDuration)
				robotgo.MouseUp(buttonType)
			} else {
				robotgo.Click(buttonType)
			}

			if i < int(numberOfClicks)-1 {
				time.Sleep(clickDuration)
			}
		}
	}

	// Get scroll options
	scrollDirections, _ := task.Data["scrollDirection"].([]interface{})
	scrollLines, hasScrollLines := task.Data["scrollLines"].(float64)

	// Handle scrolling if configured.
	//
	// `!= 0` rather than `> 0`. The sign is the direction - it is the only way
	// the node can express one, since the UI offers an axis and a line count and
	// nothing else - so a `> 0` guard silently threw away every scroll up or
	// left. The node's own input accepts down to -100000, so those macros were
	// accepted, saved, and then quietly did nothing. Zero still means no scroll,
	// which is why this is not simply dropped.
	if len(scrollDirections) > 0 && hasScrollLines && scrollLines != 0 {
		for _, dir := range scrollDirections {
			direction, ok := dir.(string)
			if !ok {
				continue
			}

			scrollAmount := int(scrollLines)
			log.Printf("Scrolling %s with %v lines", direction, scrollAmount)

			switch direction {
			case "Vertical":
				// Positive is down, negative is up: ScrollDir(x, "down") is
				// Scroll(0, -x), so a negative count inverts by itself.
				robotgo.ScrollDir(scrollAmount, "down")
			case "Horizontal":
				// Positive is **left**, negative is right - robotgo's own
				// ScrollDir maps "left" to Scroll(x, 0) and "right" to
				// Scroll(-x, 0). The comment here used to claim the opposite;
				// the call is left as it was rather than flipped to match it,
				// because positive counts already worked and already scrolled
				// left, and any macro relying on that would silently reverse.
				robotgo.Scroll(scrollAmount, 0)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	app.emitEvent("task-success", map[string]interface{}{
		"taskID": task.ID,
		"type":   "Click",
	})
}
