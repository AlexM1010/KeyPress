# Design: a control-flow interpreter for macro execution

Status: proposed, not implemented. Nothing in `backend/` has been changed for it.

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

Today a node with two outgoing edges runs both, concurrently - the queue runs
every task the flow makes ready at once. Under a token there is one token.

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
safeguard**. On load, walk the graph and flag every node reachable from Start by
more than one path, and report it through the status panel using the machinery
that already exists for this shape of warning (`warnAboutSkippedNodes` in
`execution.go`, which reports "wired-up but unreachable" nodes). A diamond is far
more often a drawing mistake than an intent to run something twice, and saying so
is useful whether or not anyone's macro predates it. It no longer gates Phase 2,
and can ship whenever it is convenient.

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
next one (`33797cb`).

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

- **Branch** - takes a condition, picks an output. Useless without condition
  *sources*, which means the run state above plus non-blocking world queries:
  "is the pixel at (x, y) this colour" as opposed to today's blocking Wait For
  Color, "is this window focused". The cheapest first one is a timeout output on
  Wait For Color, since that node already knows how to answer the question and
  currently has nowhere to say no.
- **Loop** - a count *and* an until-condition, taken together rather than
  count-first. They share one node and one back-edge: the count is the budget,
  the condition is the exit, and a loop carrying both is "retry up to five times
  until the pixel turns green", which is the case that motivates any of this.
  Either alone is a special case of the pair - no condition means "until the
  count runs out", no count means "until the condition holds" - and a loop with
  neither is a mistake the editor should refuse rather than a construct to
  support.
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
  introduces recursion, which interacts directly with the iteration budget. Worth
  designing rather than adding.

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
| `TaskQueue` dispatcher, unlimited concurrency | one runner; the generation token and context stay |

`reachableFrom` and its eight `testdata/reachability` fixtures keep earning their
place: "no path from Start" is still exactly the question the editor wants to
answer to mark an orphaned node, and it is now purely a frontend-facing lint. The
existing tests for it stay valid.

## Migration path

Each phase leaves the tree green - `npm run check`, `npm test`, `go vet`,
`go test -race` - and is independently revertible, in the same way the Svelte 5
work was.

**Phase 0 - decided, no code.** Per-arrival execution, run state as a "store
result as" field, and a loop node taking a count and an until-condition. All
three are written into the sections above.

**Phase 0.5 - give the mouse handlers the context.** The one piece of code that
belongs before the refactor starts, for the reason under item 7 above. Small,
self-contained, and worth having whether or not the rest of this is ever built.

`SVGNode` can be deleted at any point and has no dependency on any of this.

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
frontend (`useUpdateNodeInternals`), a handler returning a named output, and the
run state it reads its condition from. No scheduler changes - Phase 2 already
made this expressible. A timeout output on Wait For Color is the cheapest first
condition and needs no new world queries.

**Phase 4 - loops and the toggle.** A loop node taking a count and an
until-condition, a Stop node, the minimum yield, and `RunMacro` becoming toggle-shaped. Also no
scheduler changes: a loop is an edge, and by this point an edge is all it needs
to be.

Phases 3 and 4 are where the user-visible features arrive, and both are small
because Phase 2 did the work.

## Test plan

The failing tests to write first, in the style of `execution_test.go`, mostly as
JSON fixtures beside the reachability ones:

- a linear macro visits its nodes in order, once each
- a node with two outgoing edges visits both branches, depth-first, in a stable
  order that does not depend on map iteration
- a diamond visits the join **twice**, and the load-time lint names it
- a self-edge loops, and stops on cancellation within one iteration
- a loop with an iteration budget of *n* stops at *n* and reports it
- a loop with only a count runs exactly that many times; one with only an
  until-condition stops the first time the condition holds; one with both stops
  at whichever comes first, tested in both orders
- a value written by "store result as" is readable by a later node's condition,
  and a node that stored nothing leaves the name unset rather than empty
- cancellation between two instantaneous nodes stops the run before the second
- a branch node's `next` selects exactly one outgoing edge, and an unknown
  `next` ends that path rather than panicking
- the toggle: a second `RunMacro` while running stops it, and a third starts a
  fresh run with a new generation token
- run state does not survive a run: a macro that sets a variable, is stopped
  mid-way, and is started again sees it unset. This is the same hazard as
  `33797cb` and deserves the same suspicion - leftovers from a stopped macro
  being visible to the next one is exactly the bug that was fixed there

The frontend's existing 144 tests should need no changes in Phases 1-2; they
cover node payloads, which this does not touch.

## What this design does not decide

- **Nested loop semantics**, and with them whether Break and Continue are worth
  having. Only answerable once loops exist.
- **What a condition is written in.** An until-condition needs a way to say "this
  variable equals that", and whether that is a small fixed set of comparisons in
  the node's own UI or something more expressive is a Phase 3 question, best
  answered once one real condition exists to generalise from.
- **Whether the queue is deleted or kept.** With the Parallel node ruled out
  there is less reason to keep it, but the generation token is needed regardless
  and the queue costs little, so this can be decided late.

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
