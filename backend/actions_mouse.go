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
		err := "Invalid or missing position configurations"
		log.Printf("MoveMouse error: %s for task %s", err, task.ID)
		app.emitEvent("task-error", map[string]interface{}{
			"taskID": task.ID,
			"error":  err,
		})
		return
	}

	// Get current mouse position for 'Mouse' type positions
	currentX, currentY := robotgo.Location()

	// Resolve start position
	var startX, startY float64
	if startPos["type"] == "Mouse" {
		startX, startY = float64(currentX), float64(currentY)
	} else {
		coords, ok := startPos["coordinates"].(map[string]interface{})
		if !ok {
			err := "Invalid start coordinates"
			log.Printf("MoveMouse error: %s for task %s", err, task.ID)
			app.emitEvent("task-error", map[string]interface{}{
				"taskID": task.ID,
				"error":  err,
			})
			return
		}
		startX = coords["x"].(float64)
		startY = coords["y"].(float64)
	}

	// Move to start position if not already there
	robotgo.Move(int(startX), int(startY))

	// Resolve end position
	var endX, endY float64
	if endPos["type"] == "Mouse" {
		endX, endY = float64(currentX), float64(currentY)
	} else {
		coords, ok := endPos["coordinates"].(map[string]interface{})
		if !ok {
			err := "Invalid end coordinates"
			log.Printf("MoveMouse error: %s for task %s", err, task.ID)
			app.emitEvent("task-error", map[string]interface{}{
				"taskID": task.ID,
				"error":  err,
			})
			return
		}
		endX = coords["x"].(float64)
		endY = coords["y"].(float64)
	}

	// Extract movement settings
	speed := task.Data["speed"].(map[string]interface{})
	speedType := speed["type"].(string)
	speedValue := speed["value"].(float64)
	randomize := speed["randomize"].(bool)
	variance := speed["variance"].(float64)
	pathType := task.Data["pathType"].(string)
	dragWhileMoving := task.Data["dragWhileMoving"].(bool)

	// Calculate final speed with randomization if enabled
	finalSpeed := speedValue
	if randomize {
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		varianceAmount := speedValue * (variance / 100.0)
		finalSpeed += (r.Float64()*2 - 1) * varianceAmount
	}

	// Set mouse movement speed based on configuration
	robotgo.MouseSleep = int(finalSpeed) // Convert to appropriate sleep value

	// Start drag if required
	if dragWhileMoving {
		if err := robotgo.MouseDown("left"); err != nil {
			log.Printf("MouseDown error: %v for task %s", err, task.ID)
			app.emitEvent("task-error", map[string]interface{}{
				"taskID": task.ID,
				"error":  fmt.Sprintf("MouseDown failed: %v", err),
			})
			return
		}
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

	// Release drag if active
	if dragWhileMoving {
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

	// Get clickDelay
	clickDelay, ok := task.Data["clickDelay"].(float64)
	if !ok {
		clickDelay = 0.1 // Default delay of 100ms
	}
	clickDuration := time.Duration(clickDelay) * time.Millisecond

	// Get pressReleaseDelay and releaseAfterPress
	pressReleaseDelay, ok := task.Data["pressReleaseDelay"].(float64)
	if !ok {
		pressReleaseDelay = 0.1 // Default press duration of 100ms
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

	// Handle scrolling if configured
	if len(scrollDirections) > 0 && hasScrollLines && scrollLines > 0 {
		for _, dir := range scrollDirections {
			direction, ok := dir.(string)
			if !ok {
				continue
			}

			scrollAmount := int(scrollLines)
			log.Printf("Scrolling %s with %v lines", direction, scrollAmount)

			switch direction {
			case "Vertical":
				// For vertical scrolling, positive is down, negative is up
				robotgo.ScrollDir(scrollAmount, "down")
			case "Horizontal":
				// For horizontal scrolling, we use the x,y coordinates method
				// Positive scrollAmount moves right, negative moves left
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
