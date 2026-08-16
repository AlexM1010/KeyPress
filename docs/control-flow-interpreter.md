# Design: a control-flow interpreter for macro execution

Status: **Phases 0.5, 1, 2, 3 and Phase 4's backend have landed.** `startFlow`
runs the interpreter; the dependency scheduler, the stall detector and the task
queue's dispatcher are gone; run state has writers and readers, and a Branch node
picks between two outputs. Phase 4 added the loop - as the **node pair** of
amendment 1 below, `LoopStartNode` and `LoopEndNode`, rather than the single node
the body of this document was first written to - along with the per-iteration
yield, the toggle-shaped `RunMacro`, the release of held keys on cancellation,
and the removal of the iteration budget. The editor half of the pair is still to
come; the sections below are written as the design it is being built to.

Phase 0 is settled. The app is unreleased, which retires the compatibility
question the diamond rule turned on; run state is a "store result as" field on
the nodes that already produce a result; and the loop node takes a count and an
until-condition together. Those decisions are written into the sections below
rather than kept as a changelog - this is a design document, not a history of
one.

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
edge. The run ends when the token has nowhere to go, or when the context is
cancelled - and, since the iteration budget went (item 5 below), there is no
third way.

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

### Fan-out stops being parallel, and stops being drawn with two wires

The old engine ran a node's outgoing edges concurrently - the queue ran every
task the flow made ready at once. Under a token there is one token.

**Decided: an output handle takes exactly one edge, and fan-out is an explicit
Sequence node.** This is Unreal Blueprints' rule. The document cites Blueprints
for the execution-pin/data-pin split but did not, at first, adopt its
single-wire constraint - which is precisely what makes execution order
unambiguous there, because there is never a set of wires whose order has to be
decided by anything.

So:

- `next == ""` - "the only output" - takes every outgoing edge, **in ascending
  `sourceHandle` order**, depth-first: the first target's whole subtree runs
  before the second target is started. For an ordinary node that is one edge and
  the token simply moves; for a Sequence node with handles `out-1`, `out-2`,
  `out-3` it is those three, in the order the user sees printed on the node.
- `next != ""` takes the single edge on that handle.
- `startFlow` refuses a flowchart that wires two edges to one handle, naming the
  node and the handle. The editor will not draw one, but a file can be
  hand-edited, and failing before anything has run beats failing halfway through
  typing into whatever the user had focused.

An earlier draft of this section proposed running every outgoing edge in target
id order, "which is the order `serializeMacro` already sorts by". That rationale
was false - `serializeMacro` sorts edges by `edge.id`, not by target - and the
ordering it defended is invisible to a user, because node ids are
`crypto.randomUUID()`. Ordering by handle needs no tie-break at all and is
user-controlled rather than inferred.

Every branch still runs, so a macro that fanned out still does everything it
did, once its fan-out is redrawn through a Sequence node. What changes is that
the branches no longer interleave.

That is a smaller loss than it sounds, and arguably a fix. `mouseMu` already
serialises every mouse task against every other, because you cannot move one
cursor to two places at once - so the concurrency was already largely theatre
for the actions this app performs. Where it was real (a delay running alongside
a keypress), it was also unpredictable, which is not a property you want in a
macro that types into somebody's editor.

The queue lost its three-worker cap after this was written, so on paper the
change is now from "as wide as the graph" to one, rather than from three to one.
The argument is unaffected. What widened was how many tasks may be queued on
`mouseMu` at once, not how many may touch the mouse.

The obvious next thought is that genuine concurrency should come back as an
explicit node - and it should not, at least not on any evidence this app
currently offers. There is one mouse and one keyboard; two branches cannot type
at once in any sense a user would want. The one real case, holding a button
*while* the cursor moves, already exists as `dragWhileMoving`, a property on
Mouse Move. That is the shape the genuine concurrency here takes: within an
action, not between branches. A Parallel node is left out until a concrete case
turns up that `dragWhileMoving` does not already cover.

### A node on two paths runs twice

Today a diamond - `A→B`, `A→C`, `B→D`, `C→D` - runs `D` **once**, after both
`B` and `C` finish, because `canEnqueue` waits for every prerequisite. Under a
token, `D` is reached twice and runs twice.

**Decided: per-arrival execution.** There was never much of a choice - "run each
node at most once" is precisely what a loop must violate, so keeping the old
behaviour and having loops are mutually exclusive. Blueprints resolve it the same
way: a node executes whenever it is triggered, and a user who needs a join
inserts an explicit synchronisation node.

This was written as the item to agree before any code, on the grounds that it is
the only change that can silently alter what a saved macro does to somebody's
real keyboard. The app is unreleased. There are no saved macros, nothing is
silently altered, and this stops being a migration question at all.

What survives is worth keeping on its own merits, but as a **lint rather than a
safeguard**. At run start, flag every node reachable from Start by more than one
path and report it through the status panel using the machinery that already
exists for this shape of warning (`warnAboutSkippedNodes` in `execution.go`,
which reports "wired-up but unreachable" nodes). It ships in Phase 2 as
`execution-nodes-multipath`.

What it catches is **two unrelated branches joining into one node**: an easy
accident, whose consequence - the join, and everything after it, running twice -
is invisible on the canvas. It is deliberately not phrased as a warning about
diamonds as such. With one wire per output handle a diamond cannot be drawn by
accident at all; it takes a Sequence node wired to a common target, which is
something a user might well mean.

Precisely: a node with two or more incoming edges whose source is reachable from
Start, counting only edges that are not back-edges. Excluding back-edges is not
decoration - Phase 4 makes a loop an edge pointing backwards, and a loop header
has two incoming edges by construction, so without the exclusion every loop a
user draws would be reported as a mistake.

## The gap that is not a node: run state

Nothing currently carries a value between nodes. There are no variables, and a
node's `data` payload is its own configuration, not somewhere to put a result -
nodes share only the graph. That is fine for a straight line of actions and it
is the binding constraint on everything above:

- A **loop** can only be a fixed count. "Retry until it works, up to five times"
  needs somewhere to keep the five and the count so far.
- A **branch** can only test the world as it is this instant, never anything
  remembered. "Did that click work?" is not expressible.
- Nothing can carry a value forward - a colour that was found, a cursor position
  worth returning to, a counter.

Every comparable tool has this: Blueprint variables, n8n's item data,
AutoHotkey's variables. It is the whole of **Phase 0** now that the diamond rule
is settled, because it changes what a Loop node and a Branch node are, and
retrofitting it after the interpreter ships means opening both again.

It does not have to be much, and it should not be. The proposal is the smallest
thing that answers the three cases above:

- A **per-run** map of name to string-or-number, created when a run starts and
  discarded when it ends. Not persisted, not shared between macros, not visible
  to the frontend except as a debugging read-out. A macro that wants to remember
  something across runs is a different feature and a much larger one.
- **Set** by an optional "store result as" field on the nodes that already
  produce a result: Wait For Color knows the colour it matched, Mouse Move knows
  where it ended. Decided in favour of this over a dedicated Set Variable node.
  The value exists either way, and a field on the node that produced it adds no
  node type, no handler, and no second place to look when asking where a value
  came from. A Set Variable node earns its place when something wants to write a
  value nothing produced - a literal, or an expression - which is a later feature
  resting on a different argument.
- **Read** by conditions, and by nothing else to begin with. Interpolating
  variables into a Type Text node is the obvious next want and the obvious next
  scope creep; it should wait until the condition case is working.

The run state lives on the run, not on the App, for the same reason the
generation token exists: a stopped macro's leftovers must not be visible to the
next one (`33797cb`). Phase 2 created it - `runState` in `interpreter.go`, a
field on the `run` value, discarded with it - so that Phase 3 added writers and
readers rather than deciding where it lives.

As built in Phase 3: the field is `storeResultAs` on the node's payload, a
string, and absent or empty stores nothing. **A node that stored nothing leaves
the name unset rather than empty**, which is the distinction the `isSet`
operator is built on and what makes "did the colour ever appear?" a question a
macro can ask. Wait For Color stores the colour it matched as the six-digit
lower-case hex `robotgo.GetPixelColor` returns, no `#` - the form `parseHexColor`
already accepts on the way in, so a stored colour compares equal to one typed
into a node. Mouse Move stores where it ended as **two entries, `<name>.x` and
`<name>.y`, as numbers**: the map holds one value per name, and a single packed
string was rejected because nothing can compare one - every condition wanting
either number would have to take it apart again, and the taking-apart would be a
second piece of syntax nobody wrote down.

Handlers are *handed* the run state - `executeTask(ctx, task, state, app)` -
rather than reaching for it, which is the argument that moved `ctx` to a
parameter in Phase 1: a handler given its state can be tested with a prepared
one, and there is no path by which a handler could reach the state of a run that
is not its own. The accessor surface is two methods, `set` and `value`, and
`value`'s second result is what tells unset from empty.

## The node inventory

What the model needs, what it does not, and what is already there and should not
be.

### Already there and dead: `SVGNode`

`nodeTypes.ts` registers `svgNode`, the palette does not offer it, and
`tasks.go` has no case for it - so one cannot be created, and one that somehow
existed in a save file would hit `default:` and report "Unknown task type". It is
costing maintenance for nothing; it needed its `$$Props` repaired during the
Svelte Flow 1.x upgrade. Delete it, independently of any of this.

### Needed by the model

- **Sequence** - the only way to fan out, now that an output handle takes one
  edge. Several output handles, no configuration, and a handler that runs
  nothing and returns `""`: the walk's own rule - "the only output" takes every
  outgoing edge, in handle order - does all of it, so adding an output is a
  frontend change and nothing else. Landed in Phase 2 on the backend side.
- **Branch** - takes a condition, picks an output. Landed in Phase 3 as
  `BranchNode`, with output handles `true` and `false` and a payload of
  `{ variable, operator, value }`; the condition language is the fixed set of
  comparisons written up under "What this design does not decide" below. Useless
  without condition *sources*, which means the run state above plus non-blocking
  world queries: "is the pixel at (x, y) this colour" as opposed to today's
  blocking Wait For Color, "is this window focused". The cheapest first one was
  the timeout output on Wait For Color, since that node already knew how to
  answer the question and had nowhere to say no; it shipped with the Branch node.

  That output is **conditional on the graph**, which is the part worth
  remembering. With an edge drawn from the node's `timeout` handle, a timeout is
  a handled branch: `task-success`, no `task-error`, and the token goes where the
  macro said. With nothing wired to it, the behaviour is what it always was, down
  to the wording of the error - an unhandled timeout has to stay as loud as it is
  or a macro whose colour never appears silently does nothing. Handlers learn
  which of their outputs are wired from `Task.WiredOutputs`, filled in by the walk
  when it builds the task; Phase 4's Loop node wants the same answer for its
  `done` output, which is why it is a list of handles rather than a flag named
  after this one.

  A **match** leaves by `""` - "the only output", which takes every outgoing edge
  whatever its handle is named - so every macro drawn before this node had two
  outputs, all of whose edges carry `sourceHandle: "right"`, keeps working
  untouched. The one exception is a node with a `timeout` edge actually drawn:
  there `""` would take that edge *as well*, running the fallback after every
  success, so once the graph has two outputs to tell apart the match names its
  own handle (`right`). See `colorMatchOutput` in `actions_color.go`.
- **Loop** - a count *and* an until-condition, taken together rather than
  count-first. The count is the budget, the condition is the exit, and a loop
  carrying both is "retry up to five times until the pixel turns green", which is
  the case that motivates any of this. Either alone is a special case of the pair
  - no condition means "until the count runs out", no count means "until the
  condition holds".

  Two things this paragraph originally said are no longer true, and both are
  decided below rather than here. It said *one* node and a back-edge the user
  draws: amendment 1 makes it **two nodes, `LoopStartNode` and `LoopEndNode`,
  with the back edge drawn by the editor**. And it said a loop with neither a
  count nor a condition is a mistake the editor should refuse: **it is the
  canonical macro instead** - "click until I say stop", ended by the toggle - and
  the AutoHotkey threads this document cites are almost entirely about writing
  exactly that. All four combinations are legal, the condition is checked before
  each iteration (a while loop, not a do-while), and a count of zero runs the
  body zero times.
- **Stop** - early exit. Once branches exist, "if X, we are done" needs somewhere
  to go; today the only way a macro ends is running out of edges.

Break and Continue are deliberately absent until nested loops prove they are
needed.

### Wanted, and nothing to do with this migration

- **Type a string.** `KeyPressNode` is one keystroke by design - `maxlength="1"`,
  and its comment says so - so typing `hello` is five nodes. This is the largest
  usability gap in the app and it is independent of everything here. The existing
  per-layout `character` resolution is exactly what a string needs, applied N
  times, so the hard part is already solved.
- **Call another macro.** Genuinely valuable with a graph editor, and it
  introduces recursion - which used to interact with the iteration budget and now
  has nothing to bound it at all, since the budget is gone. That is part of what
  is to be designed rather than added.

### Deliberately not nodes

- **Concurrency**, for the reason given under fan-out above.
- **Repetition of a subgraph** is a Loop node; repetition *within one action*
  stays a property. `numberOfClicks` on Mouse Click is correct where it is - it
  is one action with its own `clickDelay` timing. A repeat count on every node
  would be that one idea smeared across six components.
- **Error handling** should be an output, not a Try/Catch wrapper. The graph
  already has outputs; an optional "on error" edge from any node is cheaper and
  more visible than a construct that contains other nodes.
- **Comments** should be canvas annotations, not nodes. Worth having, but they
  must never reach `executeTask` - a node type the backend has to know about in
  order to ignore is one that can go wrong.

## Semantics to pin

Each of these should be a test before it is an implementation.

1. **A node's outcome.** Handlers return `(next string, err error)` where `next`
   is the `sourceHandle` to leave by, and `""` means "the only output". Existing
   handlers all return `""`. They keep emitting `task-success` / `task-error`
   unchanged, so the frontend's status panel and run marks need no changes.
2. **No matching edge** ends that path normally. It is how a macro finishes, not
   an error.
3. **A node's several outputs** run depth-first in handle order, as above - one
   edge per handle, and a graph that breaks that is refused before the run
   starts rather than ordered by some rule the user cannot see.
4. **Cancellation is checked between every node**, not only inside handlers. A
   loop of instantaneous nodes must still stop promptly.
5. ~~**An iteration budget** bounds a run.~~ **Removed in Phase 4, and not
   coming back.** It was `iterationBudget` in `interpreter.go`, 100000 node
   executions, reported through `execution-budget-exceeded` with the id of the
   node the token was on. It was written on the theory that a loop with no way
   out has to end somehow - and a loop with no way out is now a feature, so what
   the budget actually did was kill an intentionally endless macro after a few
   hours, with an error, having done nothing wrong. A run therefore ends in
   exactly two ways: the token runs out of edges, or the context is cancelled.
   What bounds a *runaway* loop instead is four things that do not punish a
   correct one - the minimum per-iteration yield of item 6, which caps the rate
   of a loop drawn as the node pair; the walk driver's wall-clock bound of item
   6a, which caps the rate of every cycle that does not go through one; the
   toggle-shaped `RunMacro`, which gives the user an exit; and item 4's
   cancellation check between every node, which makes that exit prompt.
   Amendment 2 below is where this was argued and settled.

   The second of those four was missing until after Phase 4 landed, and its
   absence made the other three read as more than they were: a cycle that never
   reaches a yield point is not bounded by having declared one.
6. **A minimum yield per loop iteration**, so a loop with no Delay node in it
   cannot peg a core and flood the OS input queue faster than the target
   application can drain it.
6a. **A wall-clock bound on walking without yielding**, which is item 6 for the
   cycles the loop node does not govern. There are two of them and neither is
   exotic: a cycle with no Loop Start in it at all - an ordinary edge drawn
   backwards, which `validateOutputHandles` does not refuse and the multipath
   lint deliberately excludes as a back-edge - and a cycle through a Loop Start
   that leaves by `done` every time, where the frame is popped on the way out
   and the next arrival has no previous iteration to measure a yield from (a
   count of zero, a condition that already holds, or an unwired `body`). Both
   spun at full speed.

   The shape is Scratch's, as `control-flow-next-steps.md` recommends under
   "Yield discipline": the declared yield points stay where they are, and the
   walk driver yields once if more than `maxWalkWithoutYield` has passed since it
   last yielded or blocked - 500ms, which is Scratch's `WARP_TIME`. Anything that
   really blocked resets it, so a macro with delays in it pays nothing;
   `maxWalkWithoutYield` and `walkYield` are in `backend/execution.go` with the
   reasoning.
7. **Stop interrupts a node in progress.** A 30-second Wait For Color must not
   keep a loop alive well past the user's panic-press.

   The delay, colour and keyboard handlers already take the queue context - ten
   references in `actions_delay.go`, six in `actions_keyboard.go`, three in
   `actions_color.go`. **The mouse handlers do not reference it at all** -
   `actions_mouse.go` has zero - so a `MoveSmooth` crossing the screen at a human
   speed runs to completion whatever the user presses. That is tolerable in a
   macro that ends by itself and much less so in one that loops, where the stop
   can only ever land between iterations.

   Promoted to a blocker, and the first code to write. It was filed here as a
   Phase 4 prerequisite; with loops actually being built, it is the difference
   between a runaway macro the user can stop and one they cannot, which is not
   something to find out in Phase 4. It depends on nothing else here and improves
   the engine as it stands.

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
generation token in `Runner` is what makes the toggle safe, and it stays.

One new question the toggle raises: the keystroke that stops the macro is itself
a keystroke, arriving while the macro may be holding modifiers down. Stopping
should release anything the run left held - which is the same guarantee the
deferred drag release in `actions_mouse.go` gives for the mouse button.

## What the code becomes

The interpreter **deleted more than it added**. In `backend/`, as of Phase 2:

| Before | After |
|---|---|
| `dependencies` map, `dependentsOf`, `prerequisitesOf`, `canEnqueue` | gone - the token knows where it is |
| `completed` set + `completedMux`, `nodeMap` + `graphMux` | gone; the run owns its own graph |
| `reportStall` / `pendingNodeIDs` / `execution-stalled` | gone; replaced in Phase 2 by the iteration budget, which Phase 4 then removed as well |
| static `reachableFrom` prune of the dependency map | kept, but only as a **lint** for the UI, not as runtime pruning |
| `handleCompletions`, `notifyCh`, `notifyTaskCompletion`, `drainNotifications` | gone; the walk is the loop |
| `TaskQueue` dispatcher, task channel, `Enqueue`, worker limits | gone; `Go` launches the walk, and the generation token, context and WaitGroup stay - as `Runner` |

`reachableFrom` and its eight `testdata/reachability` fixtures keep earning their
place: "no path from Start" is still exactly the question the editor wants to
answer to mark an orphaned node, and it is now purely a frontend-facing lint. The
existing tests for it stay valid.

`TaskQueue` kept its name and its file for the phase that changed what it does,
so that the behavioural change was not buried in a rename. That rename has since
landed on its own: the type is `Runner` and the file is `backend/runner.go`. It
is a generation token, that generation's context and a WaitGroup, and nothing
about what it does changed with the name.

One consequence worth stating, because it changed an event without changing the
event list: **the run announces its own ending, once, from `finishRun`**, and
`StopExecution` announces nothing. Under the old engine the completions
goroutine and `StopExecution` both emitted `execution-stopped` and nothing
ordered them; with the walk waited for, the two landed back to back in the same
millisecond, which the status panel turns into two messages with the same
`Date.now()` id - a duplicate key in a keyed `{#each}`. The walk is the side that
knows *how* a run ended, so it is the side that says so.

The one constraint the deletion may not relax: **the walk goroutine is tracked
by the runner's `WaitGroup`**. That is what `dispatch`'s deferred `running.Wait()`
used to buy, and it is the whole of `Stop`'s promise - when `Stop` returns,
nothing of the stopped generation is still touching the mouse. It also forces
the locking: `StopExecution` holds `execMutex` across that wait, so the walk
clears the execution state through an atomic and takes no lock of the App's on
its way out.

### Shutdown is terminal, and stop is not

The restartable generation is right for the user pressing stop and wrong for
teardown, and for a while `ServiceShutdown` used the one for the other. `Stop`
installs a **fresh generation** on its way out - that is what makes the next
macro work - so a shutdown built on it left the runner startable. A global
hotkey, a tray click or a `StartExecution` landing in that window began a walk
with nothing left alive to stop it: the shape of `33797cb` one level up, a run
starting after the last one was supposed to be the last.

`Runner.Close` is the terminal operation. It cancels the current generation and
waits for the walk **exactly as `Stop` does** - the promise about the mouse is
not weakened by the new path - and then installs nothing and sets `closed`. `Go`
refuses on a closed runner and says so, which is why the fix lives there rather
than in the hotkey callback: every entry point a macro can start from funnels
through `startFlow` and so through `Go`, so one refusal covers the hotkey, the
tray, the frontend and whatever is added next. `startFlow` returns the refusal
rather than swallowing it, so `RunMacro` reports it in the tray's dialog instead
of leaving the user to assume a run began. `Close` is idempotent, and a `Stop`
that lands after it does not resurrect the runner.

`ServiceShutdown` briefly released the global shortcuts itself, as belt and
braces on top of that refusal. That call has been removed: Wails' own `cleanup()`
calls `GlobalShortcut.UnregisterAll()` *before* the `InvokeSync` that reaches the
shutdown hook, so every chord was already released by the time it ran and each
`Unregister` logged a "not registered" error on every clean exit. It was also a
deadlock waiting to happen. The hook runs **on the main thread**, and
`syncHotkeys` holds `hotkeyMux` across `Register`/`Unregister`, both of which
marshal to the main thread and block on it - so a `SaveFile` in flight could hold
the lock while parked on a message loop that the shutdown hook, waiting for that
same lock, was the only thing left to pump. The invariant is now written down
next to `hotkeyMux`: **it may be held across a main-thread marshal, so it must
never be acquired from the main thread.** `execMutex` is the contrast, and the
reason `ServiceShutdown` may take it: everything it is held across - the tray's
direct `SetMenuItemInfo`, `emitEvent`'s hand-off to Wails' mailbox - marshals
nothing.

`StopExecution` declines on a closed runner for the same reason `startFlow`
does. A stop click already in flight when the user quits races `ServiceShutdown`
for `execMutex`, and if it loses it would otherwise put twenty-odd
`SetMenuItemInfo` calls into a menu the main thread is already destroying. It
declines rather than closing: stop has to leave the runner usable for the user's
next macro.

## Migration path

Each phase leaves the tree green - `npm run check`, `npm test`, `go vet`,
`go test -race` - and is independently revertible, in the same way the Svelte 5
work was.

**Phase 0 - decided, no code.** Per-arrival execution, run state as a "store
result as" field, and a loop node taking a count and an until-condition. All
three are written into the sections above.

**Phase 0.5 - give the mouse handlers the context. Landed.** The one piece of
code that belongs before the refactor starts, for the reason under item 7 above.
Small, self-contained, and worth having whether or not the rest of this is ever
built.

`SVGNode` can be deleted at any point and has no dependency on any of this.

**Phase 1 - outcomes, on the current engine. Landed.** Handlers return
`(next, err)` while the DAG scheduler still ignores `next`. Pure refactor, no
behaviour change, fully testable. This was the largest mechanical diff and the
least risky one.

**Phase 2 - the interpreter, behind the existing entry point. Landed.** The walk,
the iteration budget - since removed, see item 5 - and the cancellation checks,
with `startFlow` switched to it. Fan-out became sequential and handle-ordered here; a node on two paths
started running twice here; the multipath lint and the Sequence node's handler
shipped here. The reachability fixtures gained a sibling set in
`backend/testdata/walk`, pinning order, repeats and loop termination, read by
both this suite and the frontend's.

**Phase 3 - branches. Landed.** A conditional node type: multi-output handles on
the frontend (`useUpdateNodeInternals`), a handler returning a named output, and
the run state it reads its condition from. No scheduler changes were needed and
none were made - Phase 2 had already made this expressible, and the walk is
untouched apart from the task it hands a handler. The timeout output on Wait For
Color shipped with it, as the cheapest first condition and one needing no new
world queries. The backend additions are `BranchNode` (`actions_branch.go`, with
the "store result as" writers beside it), `Task.WiredOutputs`, and the run state
threaded to every handler.

**Phase 4 - loops and the toggle. The backend has landed.** A loop taking a
count and an until-condition, the minimum yield, `RunMacro` becoming
toggle-shaped, the release of held keys when a run is cancelled, and the removal
of the iteration budget. Also no scheduler changes: a loop is an edge, and by
this point an edge is all it needs to be. That held even once the loop became a
**pair** of nodes (amendment 1): `interpreter.go` gained nothing for it, because
the second half of the pair is an ordinary node whose ordinary outgoing edge
happens to point backwards.

What shipped in the backend, and is the contract the editor is built to:
`LoopStartNode` (target handle `left`, source handles `body` and `done`, owner of
the count and the until-condition) and `LoopEndNode` (target handle `left`, one
source handle `back`, a handler that runs nothing and returns `""`). A Stop node
is still outstanding, and so is the editor: creating the pair together, drawing
the `back` edge, and refusing to leave half a pair behind.

Phases 3 and 4 are where the user-visible features arrive, and both are small
because Phase 2 did the work.

## Test plan

The failing tests to write first, in the style of `execution_test.go`, mostly as
JSON fixtures beside the reachability ones. Everything except the loop and
toggle items is written and green; those two belong to Phase 4.

- a linear macro visits its nodes in order, once each
- a Sequence node visits its branches depth-first, in handle order, which the
  fixture makes disagree with both canvas order and id order
- a diamond visits the join **twice**, and the lint names it
- a loop with an empty body - a Loop Start wired straight to its Loop End -
  turns its count many times, and a loop with neither a count nor a condition
  stops on cancellation within one iteration rather than by itself
- ~~a loop with an iteration budget of *n* stops at *n* and reports it~~ -
  withdrawn with the budget; there is no such test to write
- a loop with only a count runs exactly that many times; one with only an
  until-condition stops the first time the condition holds; one with both stops
  at whichever comes first, tested in both orders; a count of zero, and a
  condition that already holds, run the body no times at all
- a value written by "store result as" is readable by a later node's condition,
  and a node that stored nothing leaves the name unset rather than empty
- cancellation between two instantaneous nodes stops the run before the second
- a branch node's `next` selects exactly one outgoing edge, and an unknown
  `next` ends that path rather than panicking
- the timeout output of Wait For Color in both wirings: a handled branch when a
  `timeout` edge is drawn, and the same error it always reported when none is
- the comparison table, row by row, including the unset-variable rule and the
  numeric-versus-string switch
- the toggle: a second `RunMacro` while running stops it, and a third starts a
  fresh run with a new generation token
- run state does not survive a run: a macro that sets a variable, is stopped
  mid-way, and is started again sees it unset. This is the same hazard as
  `33797cb` and deserves the same suspicion - leftovers from a stopped macro
  being visible to the next one is exactly the bug that was fixed there

The claim that "the frontend's existing 144 tests should need no changes in
Phases 1-2" was wrong, and it is worth saying why rather than deleting it.
`nodeLabels.ts` orders the status panel by longest-path Kahn *explicitly because*
`canEnqueue` waited for every prerequisite - it is written against the engine
Phase 2 deletes, and its own comments say so. A node reached twice now has no
single position in a longest-path order at all. The ordering the panel wants is
the walk's, which is why `backend/testdata/walk` is shared with it.

## What this design does not decide

- ~~**Nested loop semantics**, and with them whether Break and Continue are worth
  having. Only answerable once loops exist.~~ **Answered by the pair** - see
  amendment 1. Nested loops are frames on a stack, an inner one dying with its
  loop; Break and Continue would act on the *innermost enclosing pair*, which is
  the top of that stack. Neither is built, and neither should be until a macro
  wants one: the answer existing is what this bullet was waiting for.
- ~~**What a condition is written in.**~~ **Decided in Phase 3: a small fixed
  set of comparisons in the node's own UI**, and not an expression language. It
  is the smaller of the two options this document named, and generalising is
  still best done once one real condition exists to generalise from - which is
  now the standing argument against adding a seventh operator rather than
  against having settled the question.

  A condition is `{ variable, operator, value }`. The operators are `equals`,
  `notEquals`, `greaterThan`, `lessThan`, `contains` and `isSet`, and the
  semantics are `conditionHolds` in `backend/actions_branch.go`:

  - **An unset variable makes the condition false** for every operator except
    `isSet`, which is how a macro asks the question directly. It is not an
    error - "did that click work?" is a question a macro is entitled to ask
    before anything has answered it.
  - **If both sides parse as numbers, they compare numerically; otherwise as
    strings.** That is what makes `greaterThan` mean what a user expects of a
    stored count while still working on text, and why `"3"` and `3` are the same
    value.
  - **`contains` is a substring test on the string forms**, always, even when
    both sides are numbers - the only reading that does not depend on how the
    value got into the run state.
  - **`greaterThan` / `lessThan` on two non-numeric strings are lexicographic**,
    by Go's own byte-wise comparison, so case matters and `"Z"` sorts before
    `"a"`.
  - **An unknown operator is a malformed payload**, reported like any other -
    `task-error` plus the returned error - rather than a branch silently
    guessed at. The node's UI offers six operators, so a seventh is a file that
    could not have been drawn.

  What is still open is the *next* condition source, not the language. A
  non-blocking world query - "is the pixel at (x, y) this colour", "is this
  window focused" - is what a Branch node needs to test anything the run state
  was not told, and nothing in Phase 3 added one.

## Known and accepted

Behaviours a reader will eventually find and take for bugs. Each is real, each
is understood, and each is written here so that finding it again is a decision
being re-read rather than a surprise. None is worth the change it would take
today; every one names what that change would be.

- **`macro-started` is emitted after `startFlow` has already launched the walk**
  (`RunMacro`, `backend/execution.go`). The frontend can therefore receive
  `task-started` for the first node - or, for a two-node macro, the whole run's
  `execution-completed` - *before* it is told which macro these events belong to,
  and so may attribute them to whatever is on the canvas. Pre-existing, and
  narrow, but the toggle made `RunMacro` the primary entry point rather than the
  tray's alone. The fix is one of two: emit `macro-started` before `startFlow`
  and emit a retraction on refusal, or carry the macro id on the task events
  themselves and let the panel filter - the second is the honest one, and it is a
  frontend change as much as a backend one.

- **A press landing between `execution-completed` and `setExecuting(false)` is
  consumed as a stop.** `finishRun` announces the ending first and clears the
  flag second, deliberately (see its comment), so in that window the toggle still
  sees a run in progress: the user presses to start a macro, `stopIfRunning`
  reports true, and nothing starts. Microseconds wide, and the alternative
  ordering is worse - it lets a new run begin before the last one has been
  reported, which wipes the new run's marks. A press that appears to do nothing is
  recoverable by pressing again.

- **`tray.setExecuting` still races `refreshMacros`** on the fields of Wails'
  `MenuItem` structs: one writes their enabled state from the walk goroutine, the
  other rewrites the menu from whichever goroutine saved a project. It predates
  the interpreter and lives in Wails' own structs rather than in anything this
  package owns. What Phase 4 changed is the width of the window - it used to be
  the seconds a macro ran for, and an endless macro makes it indefinite. The fix
  is a mutex around the tray's menu writes, which is the tray's to own.

- **A cancelled colour wait can still report a timeout.** The `select` in
  `executeColorPickerTask` has `ctx.Done()` and the deadline as two cases, and Go
  picks uniformly at random when both are ready, so a stop that lands exactly as
  a 30-second wait expires can take the timeout branch and emit `task-error`. The
  walk discards the handler's answer - it checks the context the moment the
  handler returns - so the only consequence is one spurious error line in the
  status panel of a run that was stopped anyway. The fix is a `ctx.Err()` check
  before the timeout branch commits.

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

## Amendments from the prior-art survey

Appended after the fact, from reading the source of the tools that already
shipped this - see `control-flow-next-steps.md` for the full survey, the file
paths behind each claim, and the post-Phase-4 material. Everything above this
line stands as written, except where the "Loop" bullet of the node inventory and
item 5 of "Semantics to pin" now point down here.

Where the three stand, now that Phase 4's backend has shipped: **amendment 1 was
adopted**, with one deviation recorded in it; **amendment 2 was declined**, and
the reasoning is kept there rather than deleted; **amendment 3 is still a
decision waiting for a feature**, since per-node retries do not exist yet.

Note that the sentence at the end of "Why not behavior trees" - "a looping
construct that a back-edge gives us for free" - is the thing amendment 1 revises.
The back edge is still what the interpreter follows. What changes is who draws
it.

**One finding of the survey is already spent.** It looked hard at fan-out
ordering, on the strength of the earlier draft's proposal to run outgoing edges
in target id order. That draft has since been replaced by one edge per handle
plus an explicit Sequence node, and the survey's conclusion is that the
replacement is the strongest of the three available answers rather than a
compromise. n8n orders its execution stack by canvas position under
`executionOrder: 'v1'`, having deliberately replaced the ordering it first
shipped; that is better than ordering by an id the user cannot see, but it is
still a rule *inferred* from the drawing. Blueprints instead makes order
*declared*, through the ordered pins of a Sequence node - which is what the
section above now specifies, arrived at independently. No change is proposed.

### 1. The back edge should be owned by a node pair, not drawn by the user

**Adopted, and built.** Revises "The node inventory" (Loop) and, with it, the two
questions left open under "What this design does not decide".

What changes is that the user never draws a backwards edge. What does not change
is the walk: it still runs a node, asks which output to leave by, and follows an
edge on that handle.

Keeping both of those true is a constraint on how the pair is built, and it is
worth being exact about, because there are two ways to do it and only one leaves
`interpreter.go` alone. Automa's way is for the second block to name its partner
directly - `nextBlockId: [{ id: currentLoop.blockId }]` - which in this codebase
would mean widening the handler contract from `next string`, a handle, to
something that can also name a node. That is a real cost now that the walk is
shipped code rather than a proposal.

**The cheaper way: the pair is an editor construct, and the back edge is a real
edge the editor draws.** Creating a Loop Start creates its Loop End, and with it
an ordinary edge from Loop End's `back` handle to Loop Start. The user sees a
matched pair and never wires the cycle; the interpreter sees an edge on a handle
and needs to know nothing about pairing. ~~`done` lives on Loop End~~ - see the
deviation recorded under "As built" below; both pins ended up on Loop Start.

The construction itself is not novel. Automa pairs two blocks by a shared
`data.loopId` and has the second synthesise the jump - `nextBlockId: [{ id:
currentLoop.blockId }]`. Blueprints hides the back edge inside a Macro that
auto-expands at compile time. LabVIEW, UiPath, OpenRPA and Scratch use
containment instead. Blender added looping to a mature node graph in 4.0 and
chose a paired-node zone. n8n is the one mainstream tool that makes the user wire
the back edge, and Loop Over Items is reliably its most confusing node.

**Proposal: Loop Start and Loop End, paired by an id, with the count and the
until-condition living on the pair as already decided.** Three things follow, and
they are the reason:

- **Nested loops and Break/Continue stop being open questions.** Both are
  deferred above as unanswerable until loops exist. With a pair the enclosing
  loop is a syntactic fact, so "which loop does Break exit" has an answer. With
  bare back edges it does not - overlapping cycles have no innermost.
- **The loop body becomes a nameable set.** "Iteration 3 of 5", highlighting the
  body, scoping a variable to the loop: each needs the body to be a set of nodes
  rather than whatever happened to get wired. This is what LabVIEW's tunnels and
  shift registers are for.
- **It is the same mechanism as a group.** A loop is a pair that iterates, a
  group is a pair that does not, and a called macro is a pair with its own run
  state. One concept with three settings rather than three features - which
  matters because recording wants to emit groups, and "call another macro" above
  wants a subgraph.

UiPath's own workflow-design guidance makes the negative case in a line: arrows
that can point anywhere resemble the unstructured GoTo, and make large workflows
prone to chaotic interweaving. That is what a user-drawn back edge invites at
scale.

#### As built

Phase 4's backend ships exactly this, with one deviation from the sketch above.

- **`LoopStartNode`** - target handle `left`; source handles **`body`** and
  **`done`**. It owns the count and the until-condition, and it makes the
  decision on every arrival: continue into `body`, or leave by `done`.
- **`LoopEndNode`** - target handle `left`; one source handle, **`back`**, which
  the editor wires to its partner Loop Start. Its handler runs nothing and
  returns `""`. It has exactly one outgoing edge, so `""` - "the only output" -
  carries the token back to the Loop Start with no special case anywhere.

**The deviation: both pins live on Loop Start, not `done` on Loop End.** Loop
Start is where the condition is evaluated, so it is where the branch belongs -
and it is what Blueprints does, whose For Loop carries `Loop Body` and
`Completed` as pins on one node. Putting `done` on the Loop End would have forced
a *second* editor-owned edge, from the start to the end, purely to route the exit
past the body: two invisible edges to keep consistent instead of one, and a
`done` output on the node that is furthest from the condition deciding it.

**The backend does not know the pair exists.** Frames are keyed by the Loop
Start's node id (`loopFrame`, `enterLoop`), and if the frontend keeps a `loopId`
or a partner id in either node's `data` the backend ignores it - stated in a
comment on `executeLoopEndTask`, because a future reader would otherwise assume
the pairing is validated somewhere. That ignorance is the property that kept
`interpreter.go` untouched, and it is what makes the degradations fall out for
free rather than needing rules:

- a Loop End whose `back` edge is missing - partner deleted, file hand-edited -
  ends that path normally, like any unwired output;
- a Loop Start whose `body` is unwired leaves by `done`, so an empty loop does
  nothing and the rest of the macro carries on (this one *is* a rule, and the one
  place the loop reads `WiredOutputs`: returning `body` into nothing would end
  the whole run in the middle of the loop);
- neither is an error. The editor is expected to refuse to leave half a pair
  behind, and the backend does not depend on it having succeeded.

**The frame unwind stays.** The pair narrows the problem it guards against
without removing it: a Branch inside a body, wired to something outside the loop,
still escapes without passing through `done`, so arriving at a Loop Start still
pops every frame above its own.

**Break and Continue are now answerable, and are still not built.** With a pair,
the enclosing loop is a syntactic fact, so the answer is *the innermost enclosing
pair* - which is what the frame stack already holds, top-most first. That is the
whole of the design work this amendment claimed to unlock; the implementation is
deliberately left until a macro wants one.

### 2. Loops need a per-loop bound, not only the run-wide budget

**Declined. The iteration budget is gone and is not coming back, and a per-loop
cap turned out to be the count.** Recorded rather than deleted, because "why is
there no bound on a run?" is a question a reader will have, and the answer is
that it was considered.

*What it proposed.* Keep the iteration budget exactly as specified - the
backstop, which catches a runaway with no loop node in it at all - and add a
maximum on the loop node itself, as Automa does alongside its `index` and `data`.
The difference was to be entirely in what the user is told: a run-wide budget
reports that the macro ran too long, a per-loop cap reports *which loop* failed
to terminate, and the status panel already knows how to name a node. n8n has only
the global form, implemented by noticing that two consecutive attempts looked
identical and throwing "Stopped execution because it seems to be in an endless
loop", which is both a heuristic and unhelpful.

*Why the budget went anyway.* The survey assumes loops terminate. Every tool it
read is an automation runner whose jobs are meant to finish, so none of them ever
weighs the case that decided this one: **a macro that is intentionally endless.**
"Click once a second until I press the hotkey again" is the canonical thing
Keypress is for - it is what the AutoHotkey threads under Sources are almost
entirely about, and it is why `RunMacro` became a toggle in the same phase. A
budget of 100000 node executions kills that macro after a few hours, with an
error, having done nothing wrong; and there is no larger number that fixes it,
because the macro is supposed to run until the user says otherwise. A bound that
a correct program cannot satisfy is not a backstop, it is a bug with a
configuration value. What replaced it bounds the *rate* rather than the total:
the minimum per-iteration yield, the toggle, and the cancellation check between
every node.

*Why the per-loop cap went too.* The narrower point survives on its own merits -
naming *which* loop failed beats naming the run. But once a loop's count is
optional, "a per-loop cap" and "the count" are the same field with two names. A
loop with a count already stops at it and says so, by node id; a loop with no
count is one the user deliberately wrote as endless, and a second field capping
it would be the run-wide budget again, per node, refusing the same macro. There
is nothing left for a cap to do that the count does not already do.

*What was left of the concern, and is now answered.* Nothing bounded a runaway
that has no loop node in it - a Branch wired into a cycle of its own, or, when it
exists, a macro that calls itself. The per-iteration yield does not cover those:
it is the Loop Start node's, so a cycle with no Loop Start in it spun as fast as
the machine, and so did one through a Loop Start that leaves by `done` every
time. The answer named here - a bound on the *rate* of an arbitrary cycle rather
than on the length of a run, the shape of Scratch's `WARP_TIME` - is now built,
and is item 6a of the semantics above. It is still a different mechanism from
either half of this amendment: it bounds no loop and refuses no macro, it only
makes the walk give a turn back.

### 3. Retries exhaust before the on-error output fires

Adds to "Deliberately not nodes", where error handling is ruled to be an output
rather than a Try/Catch wrapper.

That ruling is right, and Automa agrees with it in the plainest possible way -
its error path is just another output index on the block. But a per-node retry
count is the cheapest robustness win available and will be wanted, and the moment
it exists the two settings interact.

n8n shipped that interaction wrong. Enabling *Retry On Fail* while *On Error* is
set to either Continue option causes the retry count and wait to be silently
ignored: the node reports its error on the first failure and the retries never
happen. It is an open bug rather than a subtlety, and it is the kind that is
found by a user rather than a test.

**Decided: retries exhaust first, then the on-error output fires. The presence or
absence of an on-error edge must not change how many times a node is attempted.**
Two sentences of design and one fixture.

### Additional fixtures

Beside those in the test plan above. The first four are the backend's and are
written; the rest wait on features that do not exist.

- a bounded loop runs its body its count many times and leaves by `done`, with
  the Loop End visited once per turn - `backend/testdata/walk`, shared with the
  frontend's parity suite
- nested pairs count independently, the inner frame dying first, and a Branch
  wired out of an inner body still unwinds the frames above the loop it re-enters
- a Loop End whose partner has been deleted ends that path rather than panicking;
  a Loop Start whose `body` is unwired leaves by `done`; neither is an error
- a count of zero and a condition that already holds run the body no times.
  These two are Go-only rather than shared fixtures: they strand the body and the
  Loop End, which the frontend's *static* labeller still numbers, and the parity
  suite compares those two orderings exactly. See the README in
  `backend/testdata/walk`.
- creating a Loop Start creates its Loop End and the back edge between them, the
  editor refuses to leave one half of a pair behind, and `serializeMacro`
  round-trips the pairing - **the editor's, not written**
- ~~a nested loop's Break exits the innermost enclosing pair~~ - the answer is
  now decided (it does), but Break is not built and this fixture is not written
- ~~a per-loop cap stops that loop and names it, while the run-wide budget still
  catches a runaway containing no loop node~~ - withdrawn with amendment 2; there
  is neither a cap nor a budget to test. What replaced the second half of it is
  tested instead: a cycle with no Loop Start in it, and one through a Loop Start
  that always leaves by `done`, both yield on the walk driver's wall-clock bound,
  and a run whose nodes block for themselves is charged nothing for it - on the
  synctest clock, in `execution_test.go`
- a Sequence in a loop body whose *first* handle carries on round the loop leaves
  the branch on its second handle unrun, and the walk's stack is the same depth at
  every arrival at the Loop Start. Go-only: the frontend's walk has no notion of
  the iteration scope this rests on, so it cannot be a shared fixture until it
  does - see `loopFrame.stackDepth`
- a node with a retry count of 3 and an on-error edge is attempted 3 times before
  the edge is taken, and the same node without the edge is still attempted 3
  times - **waiting on per-node retries**
