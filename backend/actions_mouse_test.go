// actions_mouse_test.go
//
// The mouse handlers themselves drive the real cursor, so what is tested here
// is the part that reads a saved node - which is where a macro actually goes
// wrong, and the part that used to panic on anything it did not expect - plus
// clickLoop, which was split out of executeMouseClickTask for exactly this
// reason: it decides what a Stop part-way through a click node leaves behind,
// and it can answer that without a mouse.
//
// The clickLoop tests run inside a testing/synctest bubble. Their pauses are
// then on the fake clock, so a fifty-click node a second apart costs the suite
// nothing, and "the loop was parked in a pause when the run stopped" is a fact
// about the program rather than a race between the test and a real timer. See
// newBubbleTestApp in execution_test.go for the bubble's two constraints; these
// tests build no App at all, so only the leak check on the outer t applies.

package backend

import (
	"context"
	"testing"
	"testing/synctest"
	"time"
)

func TestNumberInReadsAJSONNumber(t *testing.T) {
	// Everything numeric in a saved macro arrives as a float64, because that is
	// what encoding/json decodes a JSON number into. An int in the map is
	// therefore not a case that can occur off disk - and is deliberately not
	// accepted, so a test fixture that hand-writes one fails here rather than
	// passing while the real payload would not.
	data := map[string]interface{}{"x": float64(120), "y": 0.5, "int": 3, "text": "120"}

	if got, ok := numberIn(data, "x"); !ok || got != 120 {
		t.Fatalf("numberIn(x) = %v, %v; want 120, true", got, ok)
	}
	if got, ok := numberIn(data, "y"); !ok || got != 0.5 {
		t.Fatalf("numberIn(y) = %v, %v; want 0.5, true", got, ok)
	}
	if _, ok := numberIn(data, "int"); ok {
		t.Fatal("numberIn(int) accepted an int; JSON never produces one")
	}
	if _, ok := numberIn(data, "text"); ok {
		t.Fatal("numberIn(text) accepted a string")
	}
	if _, ok := numberIn(data, "absent"); ok {
		t.Fatal("numberIn(absent) reported a missing key as usable")
	}
}

func TestResolvePositionReadsFixedCoordinates(t *testing.T) {
	pos := map[string]interface{}{
		"type":        "Fixed",
		"coordinates": map[string]interface{}{"x": float64(120), "y": float64(240)},
	}

	// The current cursor is passed but must be ignored for a Fixed position.
	x, y, ok := resolvePosition(pos, 999, 999)
	if !ok || x != 120 || y != 240 {
		t.Fatalf("resolvePosition = %v, %v, %v; want 120, 240, true", x, y, ok)
	}
}

func TestResolvePositionTakesTheCursorItIsGiven(t *testing.T) {
	// The property the ordering in `executeMouseMoveTask` depends on: "Mouse"
	// resolves against the reading passed in, not against a live one taken here.
	// Both ends are resolved from a single reading taken before anything moves,
	// so an end position of "Mouse" means where the cursor was when the task
	// started - not wherever the move to the start position just put it.
	x, y, ok := resolvePosition(map[string]interface{}{"type": "Mouse"}, 42, 84)
	if !ok || x != 42 || y != 84 {
		t.Fatalf("resolvePosition = %v, %v, %v; want 42, 84, true", x, y, ok)
	}

	// And it does not need coordinates to do it - a "Mouse" end never has any.
	if _, _, ok := resolvePosition(map[string]interface{}{"type": "Mouse"}, 0, 0); !ok {
		t.Fatal("a Mouse position was refused for having no coordinates")
	}
}

func TestResolvePositionRefusesCoordinatesItCannotRead(t *testing.T) {
	for name, pos := range map[string]map[string]interface{}{
		"no coordinates at all": {"type": "Fixed"},
		"coordinates of the wrong shape": {
			"type": "Fixed", "coordinates": "120,240",
		},
		"missing y": {
			"type": "Fixed", "coordinates": map[string]interface{}{"x": float64(120)},
		},
		"x as a string": {
			"type": "Fixed", "coordinates": map[string]interface{}{"x": "120", "y": float64(240)},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, ok := resolvePosition(pos, 0, 0); ok {
				t.Fatal("accepted a position it cannot have read")
			}
		})
	}
}

// A payload in the shape the Mouse Move node saves, which each test then
// damages in one specific way.
func mouseMovePayload() map[string]interface{} {
	return map[string]interface{}{
		"speed": map[string]interface{}{
			"type":      "Human",
			"value":     float64(300),
			"randomize": true,
			"variance":  float64(35),
		},
		"pathType":        "Human",
		"dragWhileMoving": true,
	}
}

func TestReadMouseMoveSettingsReadsAWholePayload(t *testing.T) {
	settings, reason := readMouseMoveSettings(mouseMovePayload())
	if reason != "" {
		t.Fatalf("readMouseMoveSettings rejected a good payload: %s", reason)
	}

	if settings.speedType != "Human" || settings.pathType != "Human" {
		t.Fatalf("speedType/pathType = %q/%q; want Human/Human", settings.speedType, settings.pathType)
	}
	if settings.speedValue != 300 || settings.variance != 35 {
		t.Fatalf("speedValue/variance = %v/%v; want 300/35", settings.speedValue, settings.variance)
	}
	if !settings.randomize || !settings.dragWhileMoving {
		t.Fatalf("randomize/dragWhileMoving = %v/%v; want true/true", settings.randomize, settings.dragWhileMoving)
	}
}

func TestReadMouseMoveSettingsReportsWhatItCannotGuess(t *testing.T) {
	// Each of these used to be a bare type assertion, so each of these payloads
	// panicked rather than reporting anything. The message has to name the field
	// as well as refuse the run: "panic: interface conversion" told the user
	// which macro died but nothing about which field to fix.
	for _, tc := range []struct {
		name   string
		damage func(map[string]interface{})
		want   string
	}{
		{"no speed at all", func(d map[string]interface{}) { delete(d, "speed") }, "Invalid or missing speed settings"},
		{"speed of the wrong shape", func(d map[string]interface{}) { d["speed"] = "fast" }, "Invalid or missing speed settings"},
		{"no speed type", func(d map[string]interface{}) {
			delete(d["speed"].(map[string]interface{}), "type")
		}, "Invalid or missing speed type"},
		{"no speed value", func(d map[string]interface{}) {
			delete(d["speed"].(map[string]interface{}), "value")
		}, "Invalid or missing speed value"},
		{"speed value as a string", func(d map[string]interface{}) {
			d["speed"].(map[string]interface{})["value"] = "300"
		}, "Invalid or missing speed value"},
		{"no path type", func(d map[string]interface{}) { delete(d, "pathType") }, "Invalid or missing path type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := mouseMovePayload()
			tc.damage(data)

			if _, reason := readMouseMoveSettings(data); reason != tc.want {
				t.Fatalf("reason = %q; want %q", reason, tc.want)
			}
		})
	}
}

func TestReadMouseMoveSettingsFillsInWhatItCanAssume(t *testing.T) {
	// The other half of the line: a macro saved before these fields existed is
	// still a macro that means something, so it runs rather than being refused.
	// `randomize` off makes `variance` irrelevant, and no `dragWhileMoving` is
	// plainly no drag.
	data := mouseMovePayload()
	speed := data["speed"].(map[string]interface{})
	delete(speed, "randomize")
	delete(speed, "variance")
	delete(data, "dragWhileMoving")

	settings, reason := readMouseMoveSettings(data)
	if reason != "" {
		t.Fatalf("readMouseMoveSettings refused a payload it could complete: %s", reason)
	}
	if settings.randomize {
		t.Fatal("randomize defaulted to true")
	}
	if settings.variance != 0 {
		t.Fatalf("variance = %v; want 0", settings.variance)
	}
	if settings.dragWhileMoving {
		t.Fatal("dragWhileMoving defaulted to true - a macro that predates the field would drag")
	}
}

func TestClickLoopClicksEveryTimeAndPausesBetween(t *testing.T) {
	for _, tc := range []struct {
		name  string
		count int
		pause time.Duration
		// wantElapsed is the wait the loop should have spent: one pause between
		// each pair of clicks and none after the last. It is the assertion that
		// says where the delay goes, which is otherwise invisible.
		wantElapsed time.Duration
	}{
		{"a single click waits for nothing", 1, 100 * time.Millisecond, 0},
		{"three clicks pause twice", 3, 100 * time.Millisecond, 200 * time.Millisecond},
		{"fifty clicks pause forty-nine times", 50, time.Second, 49 * time.Second},
		{"no delay configured is no delay taken", 4, 0, 0},
		{"a count of zero clicks nothing", 0, time.Second, 0},
		// The node's own input cannot produce this, but a hand-edited save file
		// can, and `for i := 0; i < -1` is a loop that does not run rather than
		// one that runs forever. Pinned so it stays that way.
		{"a negative count is not a loop", -1, time.Second, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			verifyNoLeaks(t)

			synctest.Test(t, func(t *testing.T) {
				want := tc.count
				if want < 0 {
					want = 0
				}

				start := time.Now()
				clicks := 0
				performed, completed := clickLoop(context.Background(), tc.count, tc.pause, func() bool {
					clicks++
					return true
				})

				if !completed {
					t.Errorf("clickLoop reported an uncancelled run incomplete after %d clicks", performed)
				}
				if performed != want || clicks != want {
					t.Errorf("performed = %d and the click ran %d times; want %d of each", performed, clicks, want)
				}
				if elapsed := time.Since(start); elapsed != tc.wantElapsed {
					t.Errorf("the loop waited %v; want %v", elapsed, tc.wantElapsed)
				}
			})
		})
	}
}

func TestClickLoopStopsWhenTheRunDoes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		count int
		pause time.Duration
		// cancelAt is the index of the click that cancels the run as it runs.
		// Negative cancels before the loop is entered at all.
		cancelAt int
		// cutShort makes that click report itself interrupted, as a
		// press-and-hold does when waitInterruptible returns false. Without it
		// the click completes and the loop discovers the cancellation on its
		// way to the next one.
		cutShort      bool
		wantPerformed int
	}{
		{
			// Stop landed before the task's first click, or while it waited on
			// mouseMu.
			// Not one click of it should reach the machine.
			name:     "stopped before the first click",
			count:    5,
			pause:    100 * time.Millisecond,
			cancelAt: -1,
		},
		{
			name:          "stopped while parked in the pause between clicks",
			count:         5,
			pause:         100 * time.Millisecond,
			cancelAt:      1,
			wantPerformed: 2,
		},
		{
			// The check at the top of each iteration, which is the only thing
			// standing between a Stop and the remaining forty-eight clicks of a
			// node with no delay: a zero pause never consults the context.
			name:          "stopped mid-run with no delay to be interrupted",
			count:         50,
			pause:         0,
			cancelAt:      1,
			wantPerformed: 2,
		},
		{
			name:          "stopped inside a press-and-hold",
			count:         5,
			pause:         100 * time.Millisecond,
			cancelAt:      2,
			cutShort:      true,
			wantPerformed: 3,
		},
		{
			// The case performed and completed are two answers for: every click
			// the node asked for happened, and it still did not finish.
			name:          "a hold cut short on the only click still reports incomplete",
			count:         1,
			pause:         100 * time.Millisecond,
			cancelAt:      0,
			cutShort:      true,
			wantPerformed: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			verifyNoLeaks(t)

			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if tc.cancelAt < 0 {
					cancel()
				}

				clicks := 0
				performed, completed := clickLoop(ctx, tc.count, tc.pause, func() bool {
					i := clicks
					clicks++
					if i != tc.cancelAt {
						return true
					}
					cancel()
					return !tc.cutShort
				})

				if completed {
					t.Error("clickLoop reported a stopped run as complete")
				}
				if performed != tc.wantPerformed || clicks != tc.wantPerformed {
					t.Errorf("performed = %d and the click ran %d times; want %d of each",
						performed, clicks, tc.wantPerformed)
				}
			})
		})
	}
}
