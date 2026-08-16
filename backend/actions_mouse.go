// actions_mouse.go
//
// Handlers for the mouse node types: movement (with optional drag) and
// clicking/scrolling.

package backend

import (
	"context"
	"errors"
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

// scrollSettleDelay is the pause after each scroll direction, giving the
// application under the cursor a chance to process the wheel events before the
// next batch arrives. It is waited on interruptibly like every other pause in
// this file, so a node scrolling several directions does not outlive a Stop by
// the length of its own list.
const scrollSettleDelay = 100 * time.Millisecond

// mouseMu guards the robotgo.MouseSleep package-level global and every robotgo
// mouse call that reads it (Move, MoveSmooth, Click, Toggle, Scroll, ...).
//
// It excludes nothing today, and that is worth saying plainly rather than
// leaving a reader to work out. A run is one goroutine walking the graph, so
// there are never two mouse tasks to interleave; when the engine ran every
// ready task at once, two of them would interleave their "set speed / move /
// restore speed" sequences - one task's restore-to-default clobbering another's
// live setting, and the read/write of the global a plain data race.
//
// Kept because the invariant it states is the one that breaks silently if that
// ever stops being true, and an uncontended mutex costs nothing to state it
// with. Deleting it is a decision to make deliberately, not a tidy-up to fold
// into a change that happens to make it redundant.
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
// shape the frontend's status panel already listens for, and returns the same
// reason as an error for the handler to hand back.
//
// Every one of these used to be five lines repeated inline, which is most of
// why the movement settings below were left as bare assertions instead: the
// cost of checking a field was a paragraph. It is a line now.
//
// Reporting and returning in one call is what keeps a node's two channels
// saying the same thing: the event carries the reason to the status panel, the
// error carries it to whatever called the handler, and neither can be updated
// without the other.
func mouseMoveFailed(task Task, app *App, reason string) error {
	log.Printf("MoveMouse error: %s for task %s", reason, task.ID)
	app.emitEvent("task-error", map[string]interface{}{
		"taskID": task.ID,
		"error":  reason,
	})
	return errors.New(reason)
}

// clickFailed is mouseMoveFailed for a click node, down to the log prefix the
// handler has always used.
func clickFailed(task Task, app *App, reason string) error {
	log.Printf("Click error: %s for task %s", reason, task.ID)
	app.emitEvent("task-error", map[string]interface{}{
		"taskID": task.ID,
		"error":  reason,
	})
	return errors.New(reason)
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
//
// ctx is the context of the run this task belongs to. It is a parameter rather
// than something read out of app.runner, which this handler used to do - see
// executeDelayTask for the general argument. The mouse handlers get one benefit
// of their own from it: reading it here meant taking the runner's own mutex from
// inside a handler that holds mouseMu for its whole body, so the two locks had
// an order between them that only stayed acyclic because Stop releases the
// runner's mutex across its cancel and its wait. A parameter removes the edge
// rather than relying on that.
func executeMouseMoveTask(ctx context.Context, task Task, state *runState, app *App) (string, error) {
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
		return "", mouseMoveFailed(task, app, "Invalid or missing position configurations")
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
		return "", mouseMoveFailed(task, app, "Invalid start coordinates")
	}

	endX, endY, ok := resolvePosition(endPos, currentX, currentY)
	if !ok {
		return "", mouseMoveFailed(task, app, "Invalid end coordinates")
	}

	// Read as one unit so the reading can be tested without a mouse attached -
	// see `readMouseMoveSettings`.
	settings, reason := readMouseMoveSettings(task.Data)
	if reason != "" {
		return "", mouseMoveFailed(task, app, reason)
	}
	speedType := settings.speedType
	speedValue := settings.speedValue
	randomize := settings.randomize
	variance := settings.variance
	pathType := settings.pathType
	dragWhileMoving := settings.dragWhileMoving

	// A Stop that landed while the token was on an earlier node, or while this
	// one waited on mouseMu, means the run is over before the cursor has moved
	// at all. Nothing below is worth doing, and reporting it as an error would
	// be a lie: the run reports its own ending - so this returns no error
	// as well as no event.
	if ctx.Err() != nil {
		log.Printf("MoveMouse canceled before moving for task %s", task.ID)
		return "", nil
	}

	// Move to the start position. Deliberately still ahead of the MouseSleep
	// assignment below: this is repositioning, not the movement the node
	// describes, so it runs at the default speed rather than the configured one -
	// which is what it did when it sat higher up, and is not something the
	// reordering should quietly change.
	robotgo.Move(int(startX), int(startY))

	// Calculate final speed with randomization if enabled.
	//
	// rand.Float64 reads the global source, which Go seeds randomly at startup
	// (since 1.20) and which is safe for concurrent use - the same source, and
	// the same reasoning, as the random delay node's draw in actions_delay.go.
	// The code this replaces built a generator per call from
	// time.Now().UnixNano(), which predates that automatic seeding: two mouse
	// moves reached inside the same clock tick seeded identically and drew the
	// same "random" speed as each other, which is the opposite of the
	// human-like jitter this is here to produce. A loop running one node twice
	// inside a tick does that as readily as two nodes running at once did.
	finalSpeed := speedValue
	if randomize {
		varianceAmount := speedValue * (variance / 100.0)
		finalSpeed += (rand.Float64()*2 - 1) * varianceAmount
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

	// The move itself is not interruptible, and is deliberately not made to
	// look as though it is.
	//
	// robotgo.MoveSmooth walks the cursor to the destination inside one
	// blocking cgo call. There is no channel, deadline or cancel hook that
	// reaches inside it, so the run's context is checked either side of the
	// move and never during it: a Stop pressed mid-flight lands when the cursor
	// arrives, not before.
	//
	// The obvious fake - run the move on a goroutine and return as soon as ctx
	// is done - was rejected rather than overlooked. It does not stop the
	// cursor; it only stops us waiting for it. The handler would return, and
	// with it release mouseMu, while robotgo is still dragging the cursor
	// across the screen, so the next run's clicks would land wherever that
	// abandoned move happened to have reached, and two generations of mouse
	// task would be writing MouseSleep at once - which is the data race mouseMu
	// exists to prevent. A move that finishes where it said it would is a
	// better outcome than one that keeps moving after everything that knew
	// about it has returned.
	//
	// This sits *above* the drag press rather than below it, which is not
	// cosmetic. A cancel landing between the two checks used to press the left
	// button and then return, and the deferred release below lifted it again -
	// and a press and a release at one position is a click, at the start
	// position, on a run the user had just stopped. The window is small but it
	// spans a real Move syscall and a rand draw. "Checked either side of the
	// move" survives the reorder: only the drag press, one cgo call, now sits
	// between this check and the move.
	if ctx.Err() != nil {
		log.Printf("MoveMouse canceled before the move for task %s", task.ID)
		return "", nil
	}

	if dragWhileMoving {
		if err := robotgo.MouseDown("left"); err != nil {
			log.Printf("MouseDown error: %v for task %s", err, task.ID)
			// Formatted once: the same value goes to the status panel and back
			// to the caller.
			failure := fmt.Errorf("MouseDown failed: %w", err)
			app.emitEvent("task-error", map[string]interface{}{
				"taskID": task.ID,
				"error":  failure.Error(),
			})
			return "", failure
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

	// The other side of the move. A Stop pressed while the cursor was
	// travelling is only observable now, so the node ends here rather than
	// going on to report a success it did not have.
	//
	// Returning is safe with a drag still held: the deferred release above
	// fires on this path exactly as it does on a panic, and the deferred
	// MouseSleep restore with it. Stopping a run must not leave the left button
	// physically down over whatever window the cursor came to rest on.
	if ctx.Err() != nil {
		log.Printf("MoveMouse canceled during the move for task %s", task.ID)
		return "", nil
	}

	// Release drag if active. Reported here, where a failure is worth telling
	// the user about; the deferred release above is only a backstop and stays
	// quiet once this has run.
	if dragging {
		dragging = false
		if err := robotgo.MouseUp("left"); err != nil {
			log.Printf("MouseUp error: %v for task %s", err, task.ID)
			failure := fmt.Errorf("MouseUp failed: %w", err)
			app.emitEvent("task-error", map[string]interface{}{
				"taskID": task.ID,
				"error":  failure.Error(),
			})
			return "", failure
		}
	}

	// robotgo.MouseSleep is reset to its default by the deferred restore above.

	// Where the move ended, if the node asked for it to be remembered. Stored
	// here rather than at the top of the handler, and deliberately: every path
	// above returns without storing, so a move that was refused, cut short by a
	// Stop or never made stores nothing at all and leaves the name unset. That
	// is the distinction the isSet operator rests on - "did that move happen?"
	// has to be answerable.
	//
	// The end coordinates the node resolved, not a fresh robotgo.Location():
	// they are where the move was asked to finish, and a second reading would
	// differ if the user has moved the physical mouse since.
	storePosition(state, task.Data, endX, endY)

	app.emitEvent("task-success", map[string]interface{}{
		"taskID": task.ID,
		"type":   "MoveMouse",
	})
	return "", nil
}

// clickAction performs one click of a click node and reports whether it ran to
// completion. A press-and-hold that Stop cut short reports false; a plain click
// is indivisible and always reports true.
type clickAction func() bool

// clickLoop performs count clicks, pausing pause between consecutive ones. It
// reports how many clicks it got through and whether it got through all of
// them; a false completed means the run was stopped part-way.
//
// Split out of executeMouseClickTask because that handler drives the real mouse
// and so cannot be unit tested, while this - how many clicks happen, where the
// pause goes, and what a Stop part-way through leaves behind - is precisely
// what the cancellation work changed. The click is a parameter, so a test can
// count clicks without any of them being real.
//
// The context is consulted before every click as well as through every pause.
// Through the pause alone is not enough: waitInterruptible returns at once for
// a non-positive duration without looking at the context, so a fifty-click node
// with no delay configured would run all fifty after a Stop.
//
// performed and completed are separate answers because the last click can fail
// to be either. A single-click node whose hold was cut short performed its one
// click and still did not do what it was asked, which "performed == count"
// alone cannot say.
func clickLoop(ctx context.Context, count int, pause time.Duration, click clickAction) (performed int, completed bool) {
	for i := 0; i < count; i++ {
		if ctx.Err() != nil {
			return performed, false
		}

		ranToCompletion := click()
		performed++
		if !ranToCompletion {
			return performed, false
		}

		// No pause after the last click. The delay is *between* clicks, so a
		// trailing one would only hold mouseMu against every other mouse node
		// for the length of a wait nobody asked for.
		if i < count-1 && !waitInterruptible(ctx, pause) {
			return performed, false
		}
	}

	return performed, true
}

// executeMouseClickTask performs the configured clicks and scrolling.
//
// ctx is the context of the run this task belongs to; see executeMouseMoveTask
// for why it is a parameter.
func executeMouseClickTask(ctx context.Context, task Task, _ *runState, app *App) (string, error) {
	log.Printf("Click task starting - Data: %+v", task.Data)

	// robotgo.Click/Toggle/Scroll all read the MouseSleep global, so clicking
	// has to take the same lock as moving.
	mouseMu.Lock()
	defer mouseMu.Unlock()

	// Get buttonType
	buttonType, ok := task.Data["buttonType"].(string)
	if !ok {
		return "", clickFailed(task, app, fmt.Sprintf("Invalid buttonType value: %v", task.Data["buttonType"]))
	}

	// Map buttonType to robotgo button
	if buttonType != "left" && buttonType != "right" && buttonType != "middle" {
		return "", clickFailed(task, app, fmt.Sprintf("Unsupported buttonType: %s", buttonType))
	}

	// Get numberOfClicks
	numberOfClicks, ok := task.Data["numberOfClicks"].(float64)
	if !ok {
		return "", clickFailed(task, app, fmt.Sprintf("Invalid numberOfClicks value: %v", task.Data["numberOfClicks"]))
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

	// A Stop that landed while the token was on an earlier node, or while this
	// one waited on mouseMu, ends the node here - the same check, in the same
	// place, as executeMouseMoveTask's.
	//
	// Not covered by the per-click and per-direction checks below: three
	// configurations reach neither of them and fell through to reporting success
	// on a run that was already over. A node with no clicks and no usable scroll
	// config is one of them, and a node with clicks but no scroll still had every
	// path *after* clickLoop unguarded.
	if ctx.Err() != nil {
		log.Printf("Click canceled before clicking for task %s", task.ID)
		return "", nil
	}

	// Perform the click actions
	if numberOfClicks > 0 {
		log.Printf("Performing %v clicks with %v delay and %v press duration",
			numberOfClicks, clickDuration, pressDuration)

		performed, completed := clickLoop(ctx, int(numberOfClicks), clickDuration, func() bool {
			if !releaseAfterPress {
				robotgo.Click(buttonType)
				return true
			}

			// Logged rather than returned, like every other robotgo failure
			// in this file: the handler cannot return an error, and one
			// click of a run failing is not a reason to abandon the rest.
			if err := robotgo.MouseDown(buttonType); err != nil {
				log.Printf("MouseDown error: %v for task %s", err, task.ID)
			}

			held := waitInterruptible(ctx, pressDuration)

			// Released even if the press above failed, and released just the
			// same when Stop cut the hold short. A button left physically down
			// is the one outcome worth avoiding here - see the deferred release
			// in executeMouseMoveTask - and releasing one that is already up
			// costs nothing.
			if err := robotgo.MouseUp(buttonType); err != nil {
				log.Printf("MouseUp error: %v for task %s", err, task.ID)
			}

			return held
		})

		if !completed {
			// The user pressed Stop (or the run was torn down). Return without
			// an error event: the run reports its own ending. Without a
			// success event either - the node did not do what it was asked, and
			// the status panel should not claim it did.
			log.Printf("Click canceled after %d of %d clicks for task %s",
				performed, int(numberOfClicks), task.ID)
			return "", nil
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
			// Checked per direction as well as through the pause below, so a
			// node listing both axes stops at whichever one the Stop landed on
			// rather than finishing the list.
			if ctx.Err() != nil {
				log.Printf("Scroll canceled for task %s", task.ID)
				return "", nil
			}

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

			if !waitInterruptible(ctx, scrollSettleDelay) {
				log.Printf("Scroll canceled for task %s", task.ID)
				return "", nil
			}
		}
	}

	app.emitEvent("task-success", map[string]interface{}{
		"taskID": task.ID,
		"type":   "Click",
	})
	return "", nil
}
