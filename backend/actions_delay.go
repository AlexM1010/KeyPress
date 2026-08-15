// actions_delay.go
//
// Handler for the delay node type: fixed and randomized waits.

package backend

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"math/rand"
	"time"
)

// Delay kinds understood by the handler, matching the node's Fixed/Random
// button group. They are compared exactly as spelled: the frontend writes these
// two strings and nothing else, so a payload carrying anything different is a
// file that has been edited into a state the app never produces, and saying so
// is more use than guessing which of the two was meant.
const (
	delayKindFixed  = "Fixed"
	delayKindRandom = "Random"
)

// maxDelayMs is the ceiling on a single delay node, at 24 hours.
//
// Unlike the keyboard node's press duration there is no slider ceiling to
// mirror. The node's time inputs take any positive number and convert up
// through seconds to minutes, so a long wait is something the UI genuinely lets
// a user ask for, and a clamp has no business second-guessing it. Nor is
// hanging the reason for one: waitInterruptible watches the run's context, so
// however long the wait, Stop still ends it at once and no worker is wedged.
//
// What forces a ceiling anyway is the arithmetic. time.Duration is int64
// nanoseconds and tops out around 292 years, so `time.Duration(ms) *
// time.Millisecond` on the sort of value a corrupted or hand-edited save file
// can carry overflows and WRAPS - and a wrapped negative duration is <= 0,
// which waitInterruptible returns from immediately. The absurd delay does not
// freeze the macro; it silently becomes no delay at all, and the next node
// fires straight into an application that was given no time to be ready. That
// is the failure worth spending a constant on.
//
// 24 hours is above any wait a person would sit through in one run of a macro
// (the largest unit the node offers is minutes, and this is 1440 of them) and
// still five orders of magnitude below where the multiplication could wrap, so
// the conversion is total for every value that reaches it. Something that means
// to wait longer than a day is not a delay node, it is a scheduled run.
const maxDelayMs = 24 * 60 * 60 * 1000

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

// delayConfig is the validated form of a delay node's data payload.
type delayConfig struct {
	// kind is one of the delayKind* constants.
	kind string

	// fixed is the whole wait, in delayKindFixed. Unused by Random.
	fixed time.Duration

	// min and max bound the wait, in delayKindRandom, with min <= max. Unused
	// by Fixed.
	min time.Duration
	max time.Duration
}

// parseDelayMs reads one of the node's time fields. Numbers arrive as float64
// because the payload came through encoding/json. field names the field for the
// error message, so the caller is told which of the three was wrong.
//
// A negative value is refused rather than clamped to zero. It is not a wait
// anybody can have meant, and quietly repairing it hides the more interesting
// question of how the file came to say that; refusing also puts an end to the
// worst version of the old behaviour, where `time.Duration(-5000) *
// time.Millisecond` reached waitInterruptible, counted as already elapsed, and
// ran as a delay node that did not delay.
//
// A value over the ceiling IS clamped rather than refused, which is the same
// bargain parsePressDuration strikes: the wait is nonsense either way, but a
// macro that runs with a shorter wait than the file asked for leaves the user
// something to look at, where a macro that refuses to start leaves them
// nothing.
func parseDelayMs(field string, raw interface{}) (time.Duration, error) {
	ms, ok := raw.(float64)
	if !ok {
		return 0, fmt.Errorf("invalid %s value: %v", field, raw)
	}

	// Neither NaN nor an infinity can actually come out of encoding/json - JSON
	// has no spelling for either, and a literal too big for a float64 is a
	// decode error before it ever gets here. They are checked for anyway
	// because they are the one class of value that compares false against both
	// bounds below and so would sail through the clamp, and converting either
	// to a time.Duration is behaviour the Go spec leaves undefined.
	if math.IsNaN(ms) || math.IsInf(ms, 0) {
		return 0, fmt.Errorf("invalid %s value: %v", field, raw)
	}
	if ms < 0 {
		return 0, fmt.Errorf("%s cannot be negative: %v", field, ms)
	}
	if ms > maxDelayMs {
		ms = maxDelayMs
	}

	return time.Duration(ms) * time.Millisecond, nil
}

// parseDelayConfig validates a task payload.
//
// It takes no App and consults neither the clock nor a random source, so what a
// payload means can be settled in a test without running a macro. Choosing the
// wait a Random config describes is duration's job instead, below.
func parseDelayConfig(data map[string]interface{}) (delayConfig, error) {
	kind, ok := data["delayType"].(string)
	if !ok {
		return delayConfig{}, fmt.Errorf("invalid delayType value: %v", data["delayType"])
	}

	switch kind {
	case delayKindFixed:
		fixed, err := parseDelayMs("time", data["time"])
		if err != nil {
			return delayConfig{}, err
		}
		return delayConfig{kind: kind, fixed: fixed}, nil

	case delayKindRandom:
		// One field at a time, so the message names the bound that is actually
		// wrong. The version this replaces validated both at once and reported
		// both values whichever had failed, which read as though the pair were
		// at fault when usually only one was.
		min, err := parseDelayMs("minTime", data["minTime"])
		if err != nil {
			return delayConfig{}, err
		}
		max, err := parseDelayMs("maxTime", data["maxTime"])
		if err != nil {
			return delayConfig{}, err
		}
		if min > max {
			return delayConfig{}, errors.New("minTime cannot be greater than maxTime")
		}
		return delayConfig{kind: kind, min: min, max: max}, nil

	default:
		return delayConfig{}, fmt.Errorf("unsupported delayType: %s", kind)
	}
}

// duration is the wait cfg describes: the fixed one, or a fresh draw from
// [min, max) for a random one.
//
// rand.Float64 reads the global source, which Go seeds randomly at startup
// (since 1.20) and which is safe for concurrent use - and concurrency is not
// hypothetical here, because delay tasks run on a worker pool several at a
// time. The code this replaces built a generator per call from
// time.Now().UnixNano(), which predates that automatic seeding and is now
// strictly worse than the global one: it allocates on every execution, and two
// delay nodes the pool starts inside the same clock tick would seed identically
// and draw the same "random" wait as each other.
func (cfg delayConfig) duration() time.Duration {
	if cfg.kind == delayKindRandom {
		// Drawn in Duration space rather than in float milliseconds, so the
		// result cannot land outside bounds parseDelayConfig has already
		// checked and clamped.
		return cfg.min + time.Duration(rand.Float64()*float64(cfg.max-cfg.min))
	}
	return cfg.fixed
}

// executeDelayTask waits for the configured fixed or random duration.
func executeDelayTask(task Task, app *App) {
	log.Printf("Delay task starting - Data: %+v", task.Data)

	// Handlers cannot return an error, so every failure is reported the same way
	// the other actions_*.go handlers report theirs.
	fail := func(err error) {
		log.Printf("Delay error: %s for task %s", err, task.ID)
		app.emitEvent("task-error", map[string]interface{}{
			"taskID": task.ID,
			"error":  err.Error(),
		})
	}

	cfg, err := parseDelayConfig(task.Data)
	if err != nil {
		fail(err)
		return
	}

	// Capture this run's context once, up front: it is the generation the
	// worker running us belongs to, so Stop's cancel unblocks the wait below
	// and Stop's wg.Wait is never held up by it.
	ctx := app.taskQueue.Context()

	// Logged as durations that have been through the parser rather than as the
	// raw numbers in the payload, so a value maxDelayMs cut down says what will
	// really be waited instead of leaving the clamp looking like a no-op.
	duration := cfg.duration()
	if cfg.kind == delayKindRandom {
		log.Printf("Executing random delay between %v and %v. Selected delay: %v", cfg.min, cfg.max, duration)
	} else {
		log.Printf("Executing fixed delay of %v", duration)
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
