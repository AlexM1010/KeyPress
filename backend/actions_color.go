// actions_color.go
//
// Handler for the colour node type: block until the pixel at a screen position
// matches a target colour, or give up after a timeout.

package backend

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/go-vgo/robotgo"
)

const (
	// colorPollInterval is how often the screen pixel is sampled while waiting.
	// A ticker rather than a busy loop: a tight loop would peg a core for as
	// long as the wait lasts, and a colour wait can last minutes.
	colorPollInterval = 100 * time.Millisecond

	// Fallbacks for payloads written before these fields existed.
	defaultColorTolerance = 10
	defaultColorTimeoutMs = 30000

	// maxChannelValue is the largest per-channel difference there can be, so a
	// tolerance at or above it matches anything.
	maxChannelValue = 255

	// colorTimeoutHandle is the output a Wait For Color node leaves by when the
	// colour never appeared *and* the macro drew an edge from that handle to
	// say where a timeout should go. See colorTimedOut.
	colorTimeoutHandle = "timeout"

	// colorMatchHandle is the node's other output: where the token goes when
	// the colour did appear. It is "right" because that is the source handle
	// every edge in every macro this app has ever saved carries - Svelte Flow's
	// default - so the match output is the one that was always there and the
	// timeout output is the new one. See colorMatchOutput.
	colorMatchHandle = "right"
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
// returns a pointer to a package-level static array in C: two callers would
// race on it, and even one at a time it hands out memory the next call
// overwrites.
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

// hexOf formats a colour the way robotgo.GetPixelColor reports one: six
// lower-case hex digits, no "#".
//
// That form rather than the "#rrggbb" the frontend's colour input produces,
// because it is what a stored colour is compared against. parseHexColor accepts
// both on the way in, so a macro can be written either way, but a value put
// into the run state is compared as a string by the Branch node's equals - and
// a value that sometimes carries a "#" would compare unequal to itself.
//
// Formatted from the parsed channels rather than passing GetPixelColor's own
// string through, so a short form or an odd casing from any future source
// normalises to one spelling here.
func hexOf(c rgbColor) string {
	return fmt.Sprintf("%02x%02x%02x", c.r, c.g, c.b)
}

// colorMatchOutput is the output a Wait For Color node leaves by when the
// colour did appear, which depends on what the macro drew.
//
// With **nothing wired to the timeout handle** it is "" - "the only output" -
// which takes every outgoing edge whatever its handle is named. That is the
// load-bearing case: every macro saved before this node had a second output
// carries sourceHandle "right", and a named output would match none of them and
// strand every existing macro at this node.
//
// With a **timeout edge drawn**, "" would take that edge too - "the only
// output" means every edge, not every edge whose handle is empty - so a macro
// that said where to go if the colour never appeared would go there *as well*
// on the runs where it did appear, and run its fallback after every success.
// So the match names its handle once the graph has two of them to tell apart:
// the wired handle that is not the timeout one, which is "right" in anything
// the editor draws.
//
// The fallback, for a node whose only wired output is the timeout: name the
// handle the editor uses for a match. Nothing is wired to it, so the token has
// nowhere to go and that path ends normally - which is the right answer for a
// macro that only said what to do if the colour never turned up.
func colorMatchOutput(task Task) string {
	if !task.hasWiredOutput(colorTimeoutHandle) {
		return ""
	}
	for _, handle := range task.WiredOutputs {
		if handle != colorTimeoutHandle {
			return handle
		}
	}
	return colorMatchHandle
}

// colorTimedOut reports a Wait For Color node whose colour never appeared, and
// returns the (next, err) pair the handler hands back.
//
// The behaviour is conditional on the graph, which is the whole point of this
// node having a timeout output at all:
//
//   - With an edge drawn from the `timeout` handle, a timeout is a **handled
//     branch**. The macro asked what happens if the colour never appears and
//     said where to go, so this is not a failure: it emits task-success and
//     returns the handle, and the walk moves the token down that edge.
//   - With nothing wired to it, the behaviour is what it has always been, down
//     to the wording: task-error plus the same error returned. An unhandled
//     timeout has to stay as loud as it is, or a macro whose colour never turns
//     up silently does nothing and reports success for it.
//
// Which is why this asks the task rather than the payload. Whether a timeout is
// handled is a property of what the user drew, not of what they typed into the
// node - and the walk is the only thing that knows it (Task.WiredOutputs).
func colorTimedOut(task Task, cfg colorWaitConfig, app *App) (string, error) {
	message := fmt.Sprintf("Timed out after %v waiting for pixel (%d,%d) to match #%02x%02x%02x (tolerance %d)",
		cfg.timeout, cfg.x, cfg.y, cfg.target.r, cfg.target.g, cfg.target.b, cfg.tolerance)

	if task.hasWiredOutput(colorTimeoutHandle) {
		// Nothing is stored under a "store result as" name on this path, and
		// that is deliberate: no colour was matched, so there is no colour to
		// remember. The name stays unset, which is exactly what the isSet
		// operator is for - "did the colour ever appear?" is then a question a
		// later Branch node can ask.
		log.Printf("ColorPickerNode timed out for task %s, taking the %q output: %s", task.ID, colorTimeoutHandle, message)
		app.emitEvent("task-success", map[string]interface{}{
			"taskID": task.ID,
			"type":   "ColorPickerNode",
		})
		return colorTimeoutHandle, nil
	}

	// endPathHandle, not "". This is the *unhandled* timeout - the macro drew no
	// `timeout` edge - so the only edge this node has is the one meaning "the
	// colour appeared". "" would take it: a "wait for green, then click Buy"
	// clicked Buy after waiting thirty seconds and never seeing green, having
	// reported the timeout as an error on the way past. Gating that click is the
	// entire purpose of this node, so a timeout ends the path.
	//
	// This is narrower than "a failed handler ends its path", which is not the
	// rule - a Delay with an unreadable payload still lets the macro carry on,
	// exactly as it did under the dependency scheduler. The difference is that a
	// timeout is not a broken node: it is this node's other answer, and the macro
	// simply did not say where that answer goes.
	log.Printf("ColorPickerNode error: %s for task %s", message, task.ID)
	app.emitEvent("task-error", map[string]interface{}{
		"taskID": task.ID,
		"error":  message,
	})
	return endPathHandle, errors.New(message)
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
//
// ctx is the context of the run this task belongs to, so Stop's cancel unblocks
// the wait below and Stop's wg.Wait is never held up by it. It is a parameter
// rather than something read out of app.runner - see executeDelayTask for why
// the difference matters.
func executeColorPickerTask(ctx context.Context, task Task, state *runState, app *App) (string, error) {
	log.Printf("ColorPickerNode task starting - Data: %+v", task.Data)

	cfg, err := parseColorWaitConfig(task.Data)
	if err != nil {
		// Emitted and returned, so the status panel and the caller are told the
		// same thing by the same value.
		log.Printf("ColorPickerNode error: %s for task %s", err, task.ID)
		app.emitEvent("task-error", map[string]interface{}{
			"taskID": task.ID,
			"error":  err.Error(),
		})
		// The match output rather than a bare "", for the reason in
		// colorMatchOutput: a failed node still lets the run carry on down its
		// edge - that is what a single-output node has always done - but a
		// payload it could not read is not a timeout, so it must not take the
		// fallback the macro drew for one, and "" would take both.
		return colorMatchOutput(task), err
	}

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
			// The match output, for the same reason as the parse failure above:
			// a screen this node cannot read is not the timeout the macro drew
			// its fallback for.
			return colorMatchOutput(task), errors.New(errMsg)
		}

		if colorsMatch(actual, cfg.target, cfg.tolerance) {
			// The colour the node matched, if it was asked to remember it: the
			// pixel that was actually on screen, which is within tolerance of
			// the target rather than necessarily equal to it, and is therefore
			// the only one of the two worth storing.
			storeResult(state, task.Data, hexOf(actual))

			log.Printf("ColorPickerNode matched at (%d,%d) for task %s", cfg.x, cfg.y, task.ID)
			app.emitEvent("task-success", map[string]interface{}{
				"taskID": task.ID,
				"type":   "ColorPickerNode",
			})
			// "" for a node with no timeout edge, so every macro that already
			// exists keeps working exactly as it did; the match handle for one
			// that has both, so a success does not also take the fallback. See
			// colorMatchOutput, where the whole of that argument is written
			// down.
			return colorMatchOutput(task), nil
		}

		select {
		case <-ctx.Done():
			// The user pressed Stop (or the run was torn down). Return without
			// an error event: the run reports its own ending once it stops
			// walking, and the walk must be free to return. And without an error - cancellation
			// is the run ending rather than this node failing, and the caller
			// holds the context that says so.
			log.Printf("ColorPickerNode wait canceled for task %s", task.ID)
			return "", nil

		case <-deadline.C:
			return colorTimedOut(task, cfg, app)

		case <-ticker.C:
		}
	}
}
