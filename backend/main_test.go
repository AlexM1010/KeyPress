// main_test.go
//
// Goroutine leak detection for the whole package.
//
// This app installs global keyboard and mouse hooks and runs every macro on a
// goroutine of its own - the walk (App.walk in execution.go), launched through
// Runner.Go and tracked by that runner's WaitGroup. A goroutine that outlives
// the run that started it - on stop, on rebind, on shutdown - is the failure
// mode this package is most exposed to, and nothing else in the suite would
// notice one.

package backend

import (
	"testing"

	"go.uber.org/goleak"
)

// leakIgnores lists goroutines that are running by the time the tests finish
// but are not leaks - process-lifetime background workers with no point at
// which they could have been shut down. One entry per source, each saying what
// it is, so a future addition has to justify itself.
//
// It is empty, and that is not an oversight. The obvious candidates - robotgo's
// cgo hook threads and the Wails runtime - never start under test, and the
// reason is a rule rather than luck: **nothing in this suite reaches robotgo.**
// newTestApp builds an App with a nil Wails application, and every task a test
// executes is one that cannot get there - a StartNode (see the header of
// execution_test.go), a loop node, whose two handlers read a payload and count,
// or a handler whose one call out has been given a seam and stubbed. There are
// two of those seams today: keyUp in actions_keyboard.go, which every release
// including sendKeystroke's goes through, and the clickAction clickLoop is
// handed in actions_mouse.go. goleak's own defaults already cover the Go
// runtime's signal and testing goroutines.
//
// A test that does reach one of those libraries will most likely fail *here*,
// as a leak with a stack pointing somewhere unrelated to what it changed - so
// the first question to ask of a new failure in this file is which test started
// touching the machine. The fix is that, or a narrow IgnoreTopFunction for one
// specific goroutine - never a blanket ignore, and never anything in
// Keypress/backend: one of our own goroutines outliving its run is the bug this
// file exists to report.
var leakIgnores = []goleak.Option{}

// TestMain runs the package's tests and then fails if any goroutine they
// started is still alive.
//
// This is the backstop. It catches a goroutine that survives the entire package
// run no matter which test started it, including one leaked by a test that does
// not go through newTestApp. verifyNoLeaks below is the sharper instrument;
// this one guarantees nothing slips past it.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m, leakIgnores...)
}

// verifyNoLeaks fails t if it finishes with a goroutine still running.
//
// Worth having on top of VerifyTestMain for two reasons. It names the test that
// leaked - VerifyTestMain reports the goroutine's stack after every test has
// finished, so attributing it means reading the stack and guessing. And it
// catches a goroutine that leaks per-run but is later unblocked by something a
// subsequent test does, which VerifyTestMain cannot see at all: a worker parked
// on a channel that a later test's Stop happens to cancel is a real leak of the
// first run that has vanished by the time the package exits.
//
// Registered as a Cleanup rather than deferred by each caller so it cannot be
// forgotten, and registered by newTestApp *before* the runner's Stop cleanup:
// Cleanup runs last-in-first-out, so this then runs after the runner has been
// told to shut down, which is the state we want to assert about.
//
// Shutdown here is not asynchronous at all, and that is worth keeping true.
// Runner.Stop cancels the generation and blocks on wg.Wait, and the walk is
// in that WaitGroup, so a stopped run's goroutine is gone before this runs -
// there is nothing left for goleak's retry loop to paper over. A future
// goroutine that Stop does not wait for would be relying on that retry, which is
// the smell to catch rather than the mechanism to lean on. No sleep is added
// anywhere to make this pass.
func verifyNoLeaks(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { goleak.VerifyNone(t, leakIgnores...) })
}
