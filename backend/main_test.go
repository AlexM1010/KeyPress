// main_test.go
//
// Goroutine leak detection for the whole package.
//
// This app installs global keyboard and mouse hooks and runs a restartable
// worker pool (taskqueue.go) alongside a completions goroutine
// (App.handleCompletions in execution.go) that the pool's WaitGroup does not
// track. A goroutine that outlives the run that started it - on stop, on
// rebind, on shutdown - is the failure mode this package is most exposed to,
// and nothing else in the suite would notice one.

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
// cgo hook threads and the Wails runtime - never start under test: every task
// in the suite is a StartNode, the one node type that does not reach robotgo
// (see the header of execution_test.go), and newTestApp builds an App with a
// nil Wails application. goleak's own defaults already cover the Go runtime's
// signal and testing goroutines. If a future test does reach one of those
// libraries it will fail here, and the fix is to add a narrow
// IgnoreTopFunction for that specific goroutine - never a blanket ignore, and
// never anything in Keypress/backend: one of our own goroutines outliving its
// run is the bug this file exists to report.
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
// forgotten, and registered by newTestApp *before* the queue's Stop cleanup:
// Cleanup runs last-in-first-out, so this then runs after the queue has been
// told to shut down, which is the state we want to assert about.
//
// Shutdown here is not asynchronous in a way that needs papering over.
// TaskQueue.Stop cancels the generation and blocks on wg.Wait, so its workers
// are gone before this runs. handleCompletions is not in that WaitGroup and so
// does race Stop by a small margin - which is exactly what goleak's built-in
// retry loop is for: it re-samples with backoff for about a second before
// failing, long enough for a goroutine already selecting on a cancelled context
// to exit, and far short of hiding a goroutine that is genuinely stuck. No
// sleep is added anywhere to make this pass.
func verifyNoLeaks(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { goleak.VerifyNone(t, leakIgnores...) })
}
