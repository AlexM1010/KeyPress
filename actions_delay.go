// actions_delay.go
//
// Handler for the delay node type: fixed and randomized waits.

package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"time"
)

// waitInterruptible blocks for d and reports whether the wait ran to
// completion. A canceled ctx cuts it short and returns false.
//
// This exists because a plain time.Sleep is invisible to cancellation: Stop
// cancels the run's context and then waits on the worker WaitGroup, so a
// sleeping task would hold Stop up for the whole remaining delay - a ten minute
// delay node froze the Stop button for ten minutes.
//
// A non-positive duration returns at once without consulting ctx, matching what
// time.Sleep did for those values.
func waitInterruptible(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// executeDelayTask waits for the configured fixed or random duration.
func executeDelayTask(task Task, app *App) {
	log.Printf("Delay task starting - Data: %+v", task.Data)

	// Get delayType
	delayType, ok := task.Data["delayType"].(string)
	if !ok {
		err := fmt.Sprintf("Invalid delayType value: %v", task.Data["delayType"])
		log.Printf("Delay error: %s for task %s", err, task.ID)
		app.emitEvent("task-error", map[string]interface{}{
			"taskID": task.ID,
			"error":  err,
		})
		return
	}

	// Capture this run's context once, up front: it is the generation the
	// worker running us belongs to, so Stop's cancel unblocks the wait below
	// and Stop's wg.Wait is never held up by it.
	ctx := app.taskQueue.Context()

	var duration time.Duration

	switch delayType {
	case "Fixed":
		// Get time
		timeFloat, ok := task.Data["time"].(float64)
		if !ok {
			err := fmt.Sprintf("Invalid time value: %v", task.Data["time"])
			log.Printf("Delay error: %s for task %s", err, task.ID)
			app.emitEvent("task-error", map[string]interface{}{
				"taskID": task.ID,
				"error":  err,
			})
			return
		}
		duration = time.Duration(timeFloat) * time.Millisecond
		log.Printf("Executing fixed delay of %v milliseconds", timeFloat)

	case "Random":
		// Get minTime and maxTime
		minTimeFloat, ok1 := task.Data["minTime"].(float64)
		maxTimeFloat, ok2 := task.Data["maxTime"].(float64)
		if !ok1 || !ok2 {
			err := fmt.Sprintf("Invalid minTime or maxTime values: minTime=%v, maxTime=%v", task.Data["minTime"], task.Data["maxTime"])
			log.Printf("Delay error: %s for task %s", err, task.ID)
			app.emitEvent("task-error", map[string]interface{}{
				"taskID": task.ID,
				"error":  err,
			})
			return
		}
		if minTimeFloat > maxTimeFloat {
			err := "minTime cannot be greater than maxTime"
			log.Printf("Delay error: %s for task %s", err, task.ID)
			app.emitEvent("task-error", map[string]interface{}{
				"taskID": task.ID,
				"error":  err,
			})
			return
		}

		// Generate random delay using a local random generator
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		delay := minTimeFloat + r.Float64()*(maxTimeFloat-minTimeFloat)
		duration = time.Duration(delay) * time.Millisecond
		log.Printf("Executing random delay between %v and %v milliseconds. Selected delay: %v milliseconds", minTimeFloat, maxTimeFloat, delay)

	default:
		err := fmt.Sprintf("Unsupported delayType: %s", delayType)
		log.Printf("Delay error: %s for task %s", err, task.ID)
		app.emitEvent("task-error", map[string]interface{}{
			"taskID": task.ID,
			"error":  err,
		})
		return
	}

	if !waitInterruptible(ctx, duration) {
		// The user pressed Stop (or the run was torn down). Return without an
		// error event: StopExecution already reports the stop, and the worker
		// must be free to exit.
		log.Printf("Delay canceled for task %s", task.ID)
		return
	}

	app.emitEvent("task-success", map[string]interface{}{
		"taskID": task.ID,
		"type":   "Delay",
	})
}
