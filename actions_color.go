// actions_color.go
//
// Handler for the colour node type: block until the pixel at a screen position
// matches a target colour, or give up after a timeout.

package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/go-vgo/robotgo"
)

const (
	// colorPollInterval is how often the screen pixel is sampled while waiting.
	// A ticker rather than a busy loop: a tight loop would peg a core and, with
	// only defaultWorkerCount workers, starve the rest of the pool.
	colorPollInterval = 100 * time.Millisecond

	// Fallbacks for payloads written before these fields existed.
	defaultColorTolerance = 10
	defaultColorTimeoutMs = 30000

	// maxChannelValue is the largest per-channel difference there can be, so a
	// tolerance at or above it matches anything.
	maxChannelValue = 255
)

// rgbColor is a parsed 24-bit colour.
type rgbColor struct {
	r, g, b int
}

// colorWaitConfig is the validated form of a ColorPickerNode's data payload.
type colorWaitConfig struct {
	target    rgbColor
	x, y      int
	tolerance int
	timeout   time.Duration
}

// parseHexColor parses "#rrggbb", "rrggbb" or the three digit short form into
// its channels. robotgo.GetPixelColor returns the bare six digit lower-case
// form; the frontend's <input type="color"> produces the "#" prefixed one.
//
// The channels are split by hand instead of via robotgo.HexToRgb: that helper
// returns a pointer to a package-level static array in C, so concurrent workers
// would race on it.
func parseHexColor(value string) (rgbColor, error) {
	hex := strings.TrimPrefix(strings.TrimSpace(value), "#")

	if len(hex) == 3 {
		// Expand "abc" to "aabbcc".
		var expanded strings.Builder
		for _, digit := range hex {
			expanded.WriteRune(digit)
			expanded.WriteRune(digit)
		}
		hex = expanded.String()
	}

	if len(hex) != 6 {
		return rgbColor{}, fmt.Errorf("invalid hex color %q", value)
	}

	channels := make([]int, 3)
	for i := range channels {
		channel, err := strconv.ParseUint(hex[i*2:i*2+2], 16, 8)
		if err != nil {
			return rgbColor{}, fmt.Errorf("invalid hex color %q", value)
		}
		channels[i] = int(channel)
	}

	return rgbColor{r: channels[0], g: channels[1], b: channels[2]}, nil
}

// absDiff returns |a - b|.
func absDiff(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}

// colorsMatch reports whether every channel of a and b is within tolerance.
func colorsMatch(a, b rgbColor, tolerance int) bool {
	return absDiff(a.r, b.r) <= tolerance &&
		absDiff(a.g, b.g) <= tolerance &&
		absDiff(a.b, b.b) <= tolerance
}

// parseColorWaitConfig validates a task payload. Numbers arrive as float64
// because the payload came through encoding/json.
func parseColorWaitConfig(data map[string]interface{}) (colorWaitConfig, error) {
	rawColor, ok := data["color"].(string)
	if !ok {
		return colorWaitConfig{}, fmt.Errorf("invalid color value: %v", data["color"])
	}
	target, err := parseHexColor(rawColor)
	if err != nil {
		return colorWaitConfig{}, err
	}

	x, okX := data["x"].(float64)
	y, okY := data["y"].(float64)
	if !okX || !okY {
		return colorWaitConfig{}, fmt.Errorf("invalid coordinates: x=%v, y=%v", data["x"], data["y"])
	}

	tolerance := float64(defaultColorTolerance)
	if value, ok := data["tolerance"].(float64); ok {
		tolerance = value
	}
	if tolerance < 0 {
		return colorWaitConfig{}, fmt.Errorf("tolerance cannot be negative: %v", tolerance)
	}
	if tolerance > maxChannelValue {
		tolerance = maxChannelValue
	}

	timeoutMs := float64(defaultColorTimeoutMs)
	if value, ok := data["timeoutMs"].(float64); ok {
		timeoutMs = value
	}
	if timeoutMs <= 0 {
		return colorWaitConfig{}, fmt.Errorf("timeoutMs must be greater than zero: %v", timeoutMs)
	}

	return colorWaitConfig{
		target:    target,
		x:         int(x),
		y:         int(y),
		tolerance: int(tolerance),
		timeout:   time.Duration(timeoutMs) * time.Millisecond,
	}, nil
}

// executeColorPickerTask blocks until the pixel at the configured position
// matches the target colour, then lets the flow continue.
//
// robotgo.GetPixelColor touches no robotgo global that a concurrent task also
// writes - it grabs a fresh 1x1 bitmap per call and frees it, and the hex
// formatting allocates its own buffer - so it deliberately does not take
// mouseMu. Taking that lock would serialise this potentially minutes-long wait
// against every mouse task in the pool.
func executeColorPickerTask(task Task, app *App) {
	log.Printf("ColorPickerNode task starting - Data: %+v", task.Data)

	cfg, err := parseColorWaitConfig(task.Data)
	if err != nil {
		log.Printf("ColorPickerNode error: %s for task %s", err, task.ID)
		app.emitEvent("task-error", map[string]interface{}{
			"taskID": task.ID,
			"error":  err.Error(),
		})
		return
	}

	// Capture this run's context once, up front: it is the generation the
	// worker running us belongs to, so Stop's cancel unblocks the wait below
	// and Stop's wg.Wait is never held up by it.
	ctx := app.taskQueue.Context()

	deadline := time.NewTimer(cfg.timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(colorPollInterval)
	defer ticker.Stop()

	log.Printf("Waiting for pixel (%d,%d) to match #%02x%02x%02x within %d, timeout %v",
		cfg.x, cfg.y, cfg.target.r, cfg.target.g, cfg.target.b, cfg.tolerance, cfg.timeout)

	for {
		// Sample before waiting, so an already-matching pixel returns at once.
		actual, err := parseHexColor(robotgo.GetPixelColor(cfg.x, cfg.y))
		if err != nil {
			errMsg := fmt.Sprintf("Could not read pixel at (%d,%d): %v", cfg.x, cfg.y, err)
			log.Printf("ColorPickerNode error: %s for task %s", errMsg, task.ID)
			app.emitEvent("task-error", map[string]interface{}{
				"taskID": task.ID,
				"error":  errMsg,
			})
			return
		}

		if colorsMatch(actual, cfg.target, cfg.tolerance) {
			log.Printf("ColorPickerNode matched at (%d,%d) for task %s", cfg.x, cfg.y, task.ID)
			app.emitEvent("task-success", map[string]interface{}{
				"taskID": task.ID,
				"type":   "ColorPickerNode",
			})
			return
		}

		select {
		case <-ctx.Done():
			// The user pressed Stop (or the run was torn down). Return without
			// an error event: StopExecution already reports the stop, and the
			// worker must be free to exit.
			log.Printf("ColorPickerNode wait canceled for task %s", task.ID)
			return

		case <-deadline.C:
			errMsg := fmt.Sprintf("Timed out after %v waiting for pixel (%d,%d) to match #%02x%02x%02x (tolerance %d)",
				cfg.timeout, cfg.x, cfg.y, cfg.target.r, cfg.target.g, cfg.target.b, cfg.tolerance)
			log.Printf("ColorPickerNode error: %s for task %s", errMsg, task.ID)
			app.emitEvent("task-error", map[string]interface{}{
				"taskID": task.ID,
				"error":  errMsg,
			})
			return

		case <-ticker.C:
		}
	}
}
