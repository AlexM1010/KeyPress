# Design: a control-flow interpreter for macro execution

Status: proposed, not implemented. Nothing in `backend/` has been changed for it.

## Why

The engine today is a **dependency DAG**. A node runs once every one of its
prerequisites has completed (`canEnqueue`, `execution.go`), and completion fans
out to every dependent at once. That model answers the question "what is ready?"

A macro is not that question. A macro is "do this, then that, and repeat until
the pixel turns green" - an imperative program the user drew. The two questions
we now want to ask of it, conditional branches and loops, are both control flow,
and neither fits:

- A **branch** means a node picks *one* of its outputs. A dependency graph has
  no notion of an edge not being taken, so it needs runtime skip propagation and
  a rule for what a join fed by a dead branch does. That is machinery invented
  to express something the model cannot say.
- A **loop** is a cycle, and a DAG is acyclic by definition. This is not a gap
  in the ecosystem to be filled with a library: `Azure/go-workflow` returns
  `ErrCycleDependency` from preflight and documents it as "a fundamental design
  constraint, not a limitation". Every dependency-graph engine says the same,
  because it must - "run when your prerequisites are done" has no meaning when a
  node is its own prerequisite.

The tools that do have both separate the two ideas. Unreal Blueprints split
white **execution pins** (where the program counter goes next) from **data
pins** (evaluated lazily, only when read). n8n's Loop Over Items node has a
`loop` output and a `done` output, which is a real cycle in the graph. That is
the shape this app needs, and it is the shape it has been approximating: the
"skipped nodes" warning, the static reachability prune, and the stall detector
all exist to make a dataflow scheduler behave like control flow.

So: stop scheduling, and start walking.

## The model

One goroutine holds an **execution token**. It runs the node the token is on,
asks that node which output to leave by, and moves the token along the matching
edge. The run ends when the token has nowhere to go, when the iteration budget
is spent, or when the context is cancelled.

```
for token := start; token != nil; {
    outcome := run(token.node)          // the existing handler, returning an outcome
    if ctx.Err() != nil { return }      // checked between every node, not only inside them
    token = follow(token.node, outcome.next)
}
```

Three consequences fall straight out, and they are the whole point:

- **Loops** are an edge pointing backwards. Nothing special is needed; the token
  arrives at a node it has already visited and runs it again.
- **Branches** are a node returning which output it took. No skip propagation, no
  dead-branch pruning, no join semantics to invent.
- **Cycle detection stops being an error.** `reachableFrom`'s comment about a
  cycle terminating the walk, and `reportStall`'s "a cycle, or a node whose
  prerequisites can never be satisfied", both describe a hazard that becomes a
  feature.

The graph already carries what this needs. Edges persist `sourceHandle`
(`serializeMacro`), which is exactly "which output does this edge leave from".
Nothing about the save format changes for branching; a multi-output node is a
frontend concern (`useUpdateNodeInternals`) plus a handler that returns a name.

## What this changes about macros that already exist

This is the part to get right, because it can silently alter what a saved macro
does to someone's real keyboard. Two behaviours change.

### Fan-out stops being parallel

Today a node with two outgoing edges runs both, concurrently - the queue has
`defaultWorkerCount` workers. Under a token there is one token.

**Proposal: run them in sequence, depth-first, in a deterministic order** (by
target node id, which is the order `serializeMacro` already sorts by, so it is
stable across saves). Every branch still runs, so a macro that fanned out still
does everything it did. What changes is that the branches no longer interleave.

That is a smaller loss than it sounds, and arguably a fix. `mouseMu` already
serialises every mouse task against every other, because you cannot move one
cursor to two places at once - so the concurrency was already largely theatre
for the actions this app performs. Where it was real (a delay running alongside
a keypress), it was also unpredictable, which is not a property you want in a
macro that types into somebody's editor.

If genuine concurrency is ever wanted, it should be an explicit node that says
so, not an emergent property of having drawn two edges.

### A node on two paths runs twice

This is the breaking one. Today a diamond - `A→B`, `A→C`, `B→D`, `C→D` - runs
`D` **once**, after both `B` and `C` finish, because `canEnqueue` waits for every
prerequisite. Under a token, `D` is reached twice and runs twice.

There is no way to keep the old behaviour and have loops: "run each node at most
once" is precisely what a loop must violate. Blueprints resolve this by letting
a node execute whenever it is triggered and making the user insert an explicit
synchronisation node when they need one.

**Proposal: adopt per-arrival execution, and detect the graphs it would change.**
On load, walk the graph and flag every node reachable from Start by more than one
path. Report it through the status panel using the machinery that already exists
for this exact shape of warning (`warnAboutSkippedNodes` in `execution.go`, which
already reports "wired-up but unreachable" nodes). A user with a diamond gets
told, in the same place they already get told about skipped nodes, that it will
now run twice and where.

This is the single most important item to agree before any code is written. It
is the only change that can make an existing macro do something different rather
than something differently-ordered.

## Semantics to pin

Each of these should be a test before it is an implementation.

1. **A node's outcome.** Handlers return `(next string, err error)` where `next`
   is the `sourceHandle` to leave by, and `""` means "the only output". Existing
   handlers all return `""`. They keep emitting `task-success` / `task-error`
   unchanged, so the frontend's status panel and run marks need no changes.
2. **No matching edge** ends that path normally. It is how a macro finishes, not
   an error.
3. **Several edges from one handle** run depth-first in sorted target order, as
   above.
4. **Cancellation is checked between every node**, not only inside handlers. A
   loop of instantaneous nodes must still stop promptly.
5. **An iteration budget** bounds a run: a maximum number of node executions,
   generous enough never to be hit by a real macro, low enough that a runaway
   loop ends. Exceeding it stops the run and reports it as an error, in the same
   channel as a stall does today.
6. **A minimum yield per loop iteration**, so a loop with no Delay node in it
   cannot peg a core and flood the OS input queue faster than the target
   application can drain it.
7. **Stop interrupts a node in progress.** A 30-second Wait For Color must not
   keep a loop alive well past the user's panic-press.

   The delay, colour and keyboard handlers already take the queue context.
   **The mouse handlers do not reference it at all** - `actions_mouse.go` has
   zero context references today - so a `MoveSmooth` crossing the screen at a
   human speed runs to completion whatever the user presses. That is tolerable
   in a macro that ends by itself and much less so in one that loops, since the
   stop can only ever land between iterations. Giving the mouse handlers the
   same treatment the others already have is a prerequisite of Phase 4, not a
   nicety, and it is worth doing on its own account before any of this.

## The toggle hotkey

Today the hotkey path is `RunMacro` → `startFlow`, and `startFlow` refuses a
second run (`TestStartFlowRefusesASecondRun`). Pressing the hotkey again while a
macro is running does nothing.

**Proposal:** `RunMacro` becomes toggle-shaped - if a run is in progress, stop it
and return; otherwise start. This is AutoHotkey's canonical `Toggle := !Toggle`
pattern, which those forums reach for constantly, expressed with the context
cancellation this app already has instead of a shared flag.

The re-entrancy hazard those threads keep hitting - a second press landing inside
the first press's still-running loop - is the bug already fixed here in
`33797cb` ("stop a stopped macro's nodes running inside the next one"). The
generation token in `TaskQueue` is what makes the toggle safe, and it stays.

One new question the toggle raises: the keystroke that stops the macro is itself
a keystroke, arriving while the macro may be holding modifiers down. Stopping
should release anything the run left held - which is the same guarantee the
deferred drag release in `actions_mouse.go` gives for the mouse button.

## What the code becomes

The interpreter **deletes more than it adds**. Roughly, in `backend/`:

| Today | After |
|---|---|
| `dependencies` map, `dependentsOf`, `prerequisitesOf`, `canEnqueue` | gone - the token knows where it is |
| `completed` set + `completedMux` | gone |
| `reportStall` / `execution-stalled` | gone; replaced by the iteration budget |
| static `reachableFrom` prune of the dependency map | kept, but only as a **lint** for the UI, not as runtime pruning |
| `handleCompletions` goroutine | shrinks to the walk loop |
| `TaskQueue` worker pool (`defaultWorkerCount`) | one runner; the generation token and context stay |

`reachableFrom` and its eight `testdata/reachability` fixtures keep earning their
place: "no path from Start" is still exactly the question the editor wants to
answer to mark an orphaned node, and it is now purely a frontend-facing lint. The
existing tests for it stay valid.

## Migration path

Each phase leaves the tree green - `npm run check`, `npm test`, `go vet`,
`go test -race` - and is independently revertible, in the same way the Svelte 5
work was.

**Phase 0 - decide the diamond rule.** No code. Agree the per-arrival semantics
and the load-time warning above, because everything downstream assumes it.

**Phase 1 - outcomes, on the current engine.** Change handlers to return
`(next, err)` while the DAG scheduler still ignores `next`. Pure refactor, no
behaviour change, fully testable. This is the largest mechanical diff and the
least risky one.

**Phase 2 - the interpreter, behind the existing entry point.** Write the walk,
with the iteration budget and the cancellation checks, and switch `startFlow` to
it. Fan-out becomes sequential here; diamonds start double-running here; the
load-time warning ships here. The reachability fixtures get a sibling set of
walk fixtures pinning order and loop termination.

**Phase 3 - branches.** A conditional node type: multi-output handles on the
frontend (`useUpdateNodeInternals`), a handler returning a named output. No
scheduler changes - Phase 2 already made this expressible.

**Phase 4 - loops and the toggle.** A loop node (count, or until-condition), the
minimum yield, and `RunMacro` becoming toggle-shaped. Also no scheduler changes:
a loop is an edge, and by this point an edge is all it needs to be.

Phases 3 and 4 are where the user-visible features arrive, and both are small
because Phase 2 did the work.

## Test plan

The failing tests to write first, in the style of `execution_test.go`, mostly as
JSON fixtures beside the reachability ones:

- a linear macro visits its nodes in order, once each
- a node with two outgoing edges visits both branches, depth-first, in a stable
  order that does not depend on map iteration
- a diamond visits the join **twice**, and the load-time check names it
- a self-edge loops, and stops on cancellation within one iteration
- a loop with an iteration budget of *n* stops at *n* and reports it
- cancellation between two instantaneous nodes stops the run before the second
- a branch node's `next` selects exactly one outgoing edge, and an unknown
  `next` ends that path rather than panicking
- the toggle: a second `RunMacro` while running stops it, and a third starts a
  fresh run with a new generation token

The frontend's existing 144 tests should need no changes in Phases 1-2; they
cover node payloads, which this does not touch.

## What this design does not decide

- **The loop node's shape** - fixed count, until-condition, or both. Phase 4.
- **Whether the Wait For Color node grows a timeout branch.** It is the nearest
  thing to a condition the app already has, and "on timeout, take the other
  path" would be the cheapest first conditional - but it is a Phase 3 question.
- **Nested loop semantics**, which only need answering once loops exist.
- **Whether `defaultWorkerCount` and the pool are deleted or kept** for a future
  explicit Parallel node. Keeping the queue costs little and the generation token
  is needed regardless.

## Sources

- [Azure/go-workflow](https://github.com/Azure/go-workflow) - `ErrCycleDependency`; representative of every Go DAG engine surveyed
- [Unreal Engine flow control](https://docs.unrealengine.com/4.26/en-US/ProgrammingAndScripting/Blueprints/UserGuide/FlowControl) - execution pins vs data pins
- [n8n Loop Over Items](https://docs.n8n.io/integrations/builtin/core-nodes/n8n-nodes-base.splitinbatches) and [looping](https://docs.n8n.io/flow-logic/looping/) - a cycle with `loop` and `done` outputs
- [go-behaviortree](https://github.com/joeycumines/go-behaviortree), [go-behave](https://github.com/askft/go-behave) - the rejected alternative; see below
- [AutoHotkey toggle-loop threads](https://www.autohotkey.com/boards/viewtopic.php?t=24501) - the toggle pattern and its re-entrancy hazard

**Why not behavior trees.** They give loops, conditionals and interruption
natively, and were the serious alternative. They were rejected because the tick
model - re-traverse from the root at some frequency, with nodes reporting
Running - fits polling AI well and fits blocking actions badly. Almost every node
here holds a key down for 50ms or waits 30 seconds for a pixel; expressing those
as tick-and-report-Running means fighting the paradigm on every single node, for
a looping construct that a back-edge gives us for free.
