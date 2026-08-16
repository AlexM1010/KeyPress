// actions_keyboard_test.go
//
// What a Hold action leaves behind, and what a stop does about it.
//
// The keystroke handler itself drives the real keyboard, so what is tested here
// is the bookkeeping around it: which keys the run believes are still down, and
// the release a cancelled run performs. robotgo.KeyUp is reached through the
// keyUp variable precisely so that release can be observed without pressing
// anything on the machine the suite is running on.

package backend

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// releasedKeys swaps the package's keyUp for one that records instead of
// pressing, and returns the record. Restored afterwards, so a test that forgets
// cannot leave the rest of the suite stubbed.
func releasedKeys(t *testing.T) *[]string {
	t.Helper()

	released := []string{}
	previous := keyUp
	keyUp = func(key string, args ...interface{}) error {
		modifiers := make([]string, 0, len(args))
		for _, arg := range args {
			name, _ := arg.(string)
			modifiers = append(modifiers, name)
		}
		released = append(released, describeCombo(key, modifiers))
		return nil
	}
	t.Cleanup(func() { keyUp = previous })
	return &released
}

// A key is down or it is not, so holding the same combination twice is one held
// thing. The identity is the key *and* its modifiers together - ctrl+a and a
// are two different keystrokes to release.
func TestHoldingAKeyRecordsItOnce(t *testing.T) {
	state := newRunState()

	state.holdKey("a", []string{"ctrl"})
	state.holdKey("a", []string{"ctrl"})
	state.holdKey("a", nil)

	got := make([]string, 0, len(state.held))
	for _, held := range state.held {
		got = append(got, held.combo)
	}
	if want := []string{"ctrl+a", "a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("held = %v, want %v", got, want)
	}
}

// A Release node naming the same combination its Hold did clears the record. One
// naming a different combination does not, and that asymmetry is deliberate:
// robotgo.KeyUp on a key that is already up is a no-op, where a modifier
// forgotten because something that looked like its release went past is a Ctrl
// left down over every window the user then touches.
func TestReleasingAKeyClearsOnlyTheSameCombination(t *testing.T) {
	state := newRunState()
	state.holdKey("a", []string{"ctrl"})

	state.releaseKey("a", nil)
	if len(state.held) != 1 {
		t.Errorf("releasing a plain 'a' cleared the record of ctrl+a: %v", state.held)
	}

	state.releaseKey("a", []string{"ctrl"})
	if len(state.held) != 0 {
		t.Errorf("held = %v, want the release to have cleared it", state.held)
	}
}

// Released last-held-first, so a modifier held before the key it modifies is
// the last thing to go, and the record is empty afterwards.
func TestReleaseHeldKeysLetsGoOfEverythingInReverse(t *testing.T) {
	released := releasedKeys(t)

	state := newRunState()
	state.holdKey("shift", nil)
	state.holdKey("a", []string{"ctrl"})

	state.releaseHeldKeys()

	if want := []string{"ctrl+a", "shift"}; !reflect.DeepEqual(*released, want) {
		t.Errorf("released %v, want %v", *released, want)
	}
	if len(state.held) != 0 {
		t.Errorf("held = %v after releasing everything", state.held)
	}
}

// A key that will not come up is exactly when the others matter most, so a
// failure is logged and the rest are still released.
func TestReleaseHeldKeysCarriesOnPastAFailure(t *testing.T) {
	logs := captureLogs(t)

	released := []string{}
	previous := keyUp
	keyUp = func(key string, _ ...interface{}) error {
		released = append(released, key)
		if key == "a" {
			return errors.New("the driver said no")
		}
		return nil
	}
	t.Cleanup(func() { keyUp = previous })

	state := newRunState()
	state.holdKey("shift", nil)
	state.holdKey("a", nil)

	state.releaseHeldKeys()

	if want := []string{"a", "shift"}; !reflect.DeepEqual(released, want) {
		t.Errorf("released %v, want the failure not to have stopped the rest: %v", released, want)
	}
	if !logs.contains("Could not release a") {
		t.Errorf("the failure was not logged:\n%s", logs)
	}
}

// The guarantee itself: a run that was stopped lets go of whatever it left
// held. This matters far more with a toggle-shaped RunMacro than it did before
// - the keystroke that stops the macro arrives while the macro may be holding
// modifiers down.
func TestACancelledRunReleasesWhatItLeftHeld(t *testing.T) {
	app := newTestApp(t)
	released := releasedKeys(t)

	r := newRun(inertFlow([]string{"start"}, nil), "start", inertExec)
	r.state.holdKey("ctrl", nil)
	r.outcome = runCancelled

	app.finishRun(r)

	if want := []string{"ctrl"}; !reflect.DeepEqual(*released, want) {
		t.Errorf("released %v, want %v", *released, want)
	}
}

// A panic in the walk is an ending too, and the one that used to skip the
// release entirely: the recover cleared the executing flag and returned without
// reaching finishRun, so a Ctrl the macro was holding stayed down over every
// window the user touched next - the exact failure this feature exists to
// prevent, on the path where the run stopped for a reason the user did not
// choose.
//
// The panic is raised from the run's own executor rather than from a handler:
// executeTask recovers a handler's panic and turns it into a task error, so what
// is left for App.walk's recover to catch is a bug in the walk itself, which is
// what this stands in for.
func TestAPanickedWalkLetsGoOfWhatTheMacroHeld(t *testing.T) {
	app := newTestApp(t)
	logs := captureLogs(t)
	released := releasedKeys(t)
	app.setExecuting(true)

	r := newRun(inertFlow([]string{"start"}, nil), "start", func(_ context.Context, _ Task, state *runState) (string, error) {
		state.holdKey("ctrl", nil)
		panic("the walk broke with a key down")
	})

	app.walk(context.Background(), r)

	if want := []string{"ctrl"}; !reflect.DeepEqual(*released, want) {
		t.Errorf("released %v, want %v", *released, want)
	}
	if r.outcome != runPanicked {
		t.Errorf("outcome = %v, want the run recorded as having panicked", r.outcome)
	}
	if !logs.contains(emittedEvent("execution-error")) {
		t.Errorf("the panic was not reported to the frontend:\n%s", logs)
	}
	// One ending, not two: execution-error is what the frontend clears its run
	// marks on, so finishRun must not announce the run again.
	if logs.contains(emittedEvent("execution-stopped")) || logs.contains(emittedEvent("execution-completed")) {
		t.Errorf("a panicked run announced an ending as well as its error:\n%s", logs)
	}
	if app.GetIsExecuting() {
		t.Error("a panicked run left the app reporting itself executing")
	}
}

// And a run that ended normally does not. A macro that deliberately leaves a
// key held is the documented behaviour of the Hold action - it is half of a
// gesture, and the other half may be the point of the macro - so changing that
// is a separate decision from making stop safe.
func TestARunThatFinishedLeavesAHeldKeyAlone(t *testing.T) {
	app := newTestApp(t)
	released := releasedKeys(t)

	r := newRun(inertFlow([]string{"start"}, nil), "start", inertExec)
	r.state.holdKey("ctrl", nil)
	r.outcome = runFinished

	app.finishRun(r)

	if len(*released) != 0 {
		t.Errorf("a run that finished normally released %v", *released)
	}
}

// The two halves in one place: a Hold records, a Release un-records, and what
// is left over at a stop is the difference. Driven through sendKeystroke rather
// than the handler, because the handler is what reaches the real keyboard - and
// only the Release branch of sendKeystroke does, which the stub covers.
//
// "Which the stub covers" is asserted rather than assumed, and that is the point
// of the last check here. That branch used to call robotgo.KeyUp directly, so
// these two releases pressed two real keys on whatever machine ran the suite
// while the seam sat unused beside them; the recorded combos are what says it is
// really the seam being reached now. See the comment on keyUp.
func TestSendKeystrokeRecordsAHoldAndForgetsItOnRelease(t *testing.T) {
	released := releasedKeys(t)
	state := newRunState()

	if _, err := sendKeystroke(context.Background(), state, keyActionRelease, "a", nil, 0); err != nil {
		t.Fatalf("release: %v", err)
	}
	if len(state.held) != 0 {
		t.Errorf("a Release with no matching Hold recorded something: %v", state.held)
	}

	// The Hold branch calls robotgo.KeyDown, which is not stubbed and would
	// press a real key, so the recording half is asserted through holdKey - the
	// one line sendKeystroke adds after a successful KeyDown.
	state.holdKey("a", []string{"ctrl"})
	if _, err := sendKeystroke(context.Background(), state, keyActionRelease, "a", []string{"ctrl"}, 0); err != nil {
		t.Fatalf("release: %v", err)
	}
	if len(state.held) != 0 {
		t.Errorf("held = %v, want the release to have cleared it", state.held)
	}

	if want := []string{"a", "ctrl+a"}; !reflect.DeepEqual(*released, want) {
		t.Errorf("the releases reached %v, want both of them through the keyUp seam: %v", *released, want)
	}
}
