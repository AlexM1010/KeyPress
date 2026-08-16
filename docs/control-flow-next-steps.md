# Guide: where the interpreter goes after Phase 4

Status: companion to `control-flow-interpreter.md`. Nothing here changes the
decisions in that document except where it says so explicitly, and those
exceptions are all cheaper to make before Phase 4 ships than after.

This is written from the source of the tools that already solved this, not from
their marketing. File paths are given so the claims can be checked.

## The short version

The token walk is right. Every visual language that has both branches and loops
converges on it, and the ones that refused - ComfyUI most visibly - spend their
lives routing around the refusal. `control-flow-interpreter.md` gets the core
model, the rejection of behaviour trees, and the error-handling-as-an-output
call correct, and it gets them for the right reasons.

Three decisions in it are contradicted by what actually shipped elsewhere, and
all three bear on Phase 4, which has not landed. A fourth - how a node's several
outputs are ordered - the document had already settled, on better grounds than
the survey would have proposed; see the note under the table.

**Since written, Phase 4's backend has landed and two of the three are settled.**
Recommendation 1 was adopted: the loop is `LoopStartNode` and `LoopEndNode`, and
the editor draws the back edge. Recommendation 2 was **declined** - the iteration
budget was removed rather than kept, because this app's canonical macro is
deliberately endless, and once a loop's count is optional a per-loop cap is that
count under another name. The full reasoning is amendment 2 of
`control-flow-interpreter.md`, which is where these decisions live. Recommendation
3 still stands and still waits on per-node retries. **The removal of the budget
also invalidates an argument made further down this file, under "The missing
chapter is debugging" - see the note there.**

## What the field actually does

| Tool | Traversal | Loops | Fan-out |
|---|---|---|---|
| Unreal Blueprints | single token, execution pins | back-edge, hidden inside a Macro | explicit Sequence node, ordered pins |
| Automa | worker per branch, cloned state | ID-paired start/breakpoint blocks | spawns a worker per connection |
| n8n | `nodeExecutionStack` queue | back-edge, `loop`/`done` outputs | queue order, **by canvas position** |
| Node-RED | messages, many in flight | back-edge | clones the message per branch |
| Scratch | thread per script, cooperative | C-block containment | n/a - one body |
| LabVIEW | dataflow | loop **structures** with tunnels | dataflow |
| UiPath / OpenRPA | WF activity tree | While/ForEach **containers** | hierarchical |
| ComfyUI | DAG, no cycles | none; custom nodes fake it | dataflow |

Four notes on that table, each of which is a recommendation below.

**Nobody successful makes the user draw the back edge.** Blueprints puts it
inside a Macro that auto-expands at compile time. LabVIEW, UiPath, OpenRPA and
Scratch use containment. Blender added looping to a mature node graph in 4.0 and
chose a **zone** - paired Repeat Input and Repeat Output nodes, with a scope rule
that nodes inside cannot send outputs outside. Automa pairs two blocks by a
shared `data.loopId` and has the second one synthesise the jump:

```js
// src/workflowEngine/blocksHandler/handlerLoopBreakpoint.js
const currentLoop = this.loopList[block.data.loopId];
return { nextBlockId: [{ id: currentLoop.blockId }] };
```

n8n is the one mainstream tool that does make you wire the back edge yourself,
and Loop Over Items is reliably its most confusing node.

**Ordering a node's outputs: declared beats inferred, and the document already
got there.** With `executionOrder === 'v1'` n8n orders its execution stack
top-left first, chosen deliberately over the ordering it first shipped, because
users read the canvas rather than the internals. That is better than ordering by
an id the user cannot see - but it is still a rule inferred from the drawing.
Blueprints makes the order *declared* instead, through the ordered pins of a
Sequence node, and that is what `control-flow-interpreter.md` now specifies: one
edge per handle, fan-out through an explicit Sequence, handles in the order
printed on the node. Nothing to change; recorded here because it is the one place
the survey went looking for a fault and found the strongest of the three answers
already in place.

**Fan-out is a data-cloning problem everywhere it is parallel.** Node-RED clones
the message to every branch but the first. Automa clones worker state into each
spawned branch. Both exist because concurrent branches share mutable data.

**Loop bounds are per-loop, not per-run.** Automa's loop state carries `maxLoop`
alongside `index` and `data`. n8n has no per-loop bound and instead detects
runaway execution globally by noticing that two consecutive attempts looked the
same, throwing "Stopped execution because it seems to be in an endless loop" -
a heuristic, and an unhelpful message. (Neither form was adopted here; see
recommendation 2 below and amendment 2 of `control-flow-interpreter.md`. Every
tool in this table is an automation runner whose jobs are meant to finish, which
is why none of them weighs the macro that is *meant* to run until the user stops
it.)

## Change before Phase 4 ships

### 1. Let a node pair own the back edge

**Adopted, and built** - `LoopStartNode` and `LoopEndNode`. One deviation from
the sketch below: both `body` and `done` are pins on the Loop Start, because that
is where the condition is evaluated, and putting `done` on the Loop End would
force a second editor-owned edge to route the exit past the body. Recorded under
"As built" in amendment 1 of `control-flow-interpreter.md`.

The document's position is that a loop is an edge pointing backwards and nothing
special is needed. That is true of the *interpreter*, and it should stay true.
It does not have to be true of the *palette*.

**Proposal: Loop Start and Loop End are a matched pair, created together, with an
ordinary edge between them that the editor draws.** The user never wires the
cycle; the walk still asks a handler which output to leave by and follows an edge
on that handle, so nothing in `interpreter.go` moves. The loop body becomes a
nameable set of nodes rather than whatever happened to get wired.

Automa's version, quoted above, has the second block name its partner directly.
That is the same idea but a more expensive one here: it would widen the handler
contract from `next string`, a handle, to something that can also name a node,
and the walk is shipped code now rather than a proposal. Drawing a real edge gets
the same user-facing result for nothing - structurally what Blueprints does by
hiding the back edge inside a Macro that auto-expands at compile time.

Three things fall out of it, and they are the reason to do it:

- **Nested loops and Break/Continue stop being open questions.** The document
  defers both as unanswerable until loops exist. With a pair, the enclosing loop
  is a syntactic fact, so "which loop does Break exit" has an answer. With bare
  back edges it does not: overlapping cycles have no innermost.
- **A loop body can be drawn, counted and scoped.** "Iteration 3 of 5", highlight
  the body, scope a variable to the loop - all need the body to be a set. This is
  what LabVIEW's tunnels and shift registers are for and why Blender's zone has a
  containment rule.
- **It is the same mechanism as a group.** Recording wants to emit groups; "call
  another macro" wants a subgraph. A loop that is a pair, a group that is a pair
  with no iteration, and a subflow that is a pair with its own run state are one
  concept with three settings, not three features.

UiPath's own documentation makes the negative case in one line: arrows that can
point anywhere resemble the unstructured GoTo, and make large workflows prone to
chaotic interweaving. That is the failure mode a user-drawn back edge invites.

### 2. Bound loops per loop, not only per run

**Declined**, and the run-wide budget was removed rather than kept. An
intentionally endless macro is the case this survey never weighs and the one this
app exists for, and a per-loop cap on a loop whose count is already optional is
that count under a second name. Amendment 2 of `control-flow-interpreter.md`
records the decision and what is left of the concern - nothing bounds a cycle
with no Loop Start in it. The proposal as it stood:

Keep the run-wide iteration budget as a backstop. Add a per-loop maximum, as
Automa does. The difference is entirely in what the user is told: a run-wide
budget reports "this macro ran too long", a per-loop cap reports *which loop*
failed to terminate, and the status panel already knows how to name a node.

### 3. Pin the order of retry and the error output

The document rules that error handling is an output rather than a Try/Catch
wrapper. That is the right call - Automa does the same thing, selecting the error
connection by output index:

```js
// src/workflowEngine/WorkflowWorker.js
const nextBlocks = this.getBlockConnections(
  block.id, blockOnError.toDo === 'continue' ? 1 : 'fallback'
);
```

But the moment a per-node retry count is added - and it should be, it is the
cheapest robustness win there is - the two settings interact, and n8n shipped
that interaction wrong. In n8n, enabling *Retry On Fail* while *On Error* is set
to either Continue option causes the retry settings to be silently ignored: the
node returns its error on the first failure and the retry count and wait are
never applied. It is an open bug, not a subtlety.

**Decided, and worth a test before an implementation: retries exhaust first, then
the on-error output fires.** The presence or absence of an on-error edge must not
change how many times a node is attempted. Those are two sentences in the design
and one fixture; they are a confusing product without them.

## After Phase 4

### The missing chapter is debugging

`control-flow-interpreter.md` does not mention debugging once, and it is the
single largest thing the interpreter unlocks. Under the dependency DAG, "what did
this run do" was a set of concurrent completions. Under one token it is a list.

Blueprints is the reference implementation and worth copying closely: breakpoints
that persist between sessions, the stopped node highlighted, **the execution wires
travelled coloured in**, watch values on pins, and a call-stack window with step
controls.

Keypress is most of the way there already without having planned to be. There is
per-node glow (`nodeGlow.ts`), per-node success and error events, and a label map
that names nodes the way the canvas does (`nodeLabels.ts`). What is missing is
that the run does not record where the token went.

**Proposal: the walk appends to a trace - node id, arrival index, outcome, and
duration - and the trace is the debugger.** Replaying it colours the path. Its
length per node is the loop's iteration count. Pausing before an append is a
breakpoint; appending one entry at a time is step mode. All of it is reading a
list, and none of it needs a second execution path that can disagree with the
first.

~~The cost is bounded: the iteration budget already caps how long the trace can
get.~~ **No longer true, and this is the one thing to fix before building the
trace.** Phase 4 removed the iteration budget, so a run has no bound on how many
nodes it may execute - an endless loop is a supported macro now, and a trace with
one entry per arrival grows without limit for as long as it runs, at whatever
rate the minimum per-iteration yield allows. Roughly a hundred entries a second
per loop, indefinitely, is not a cost anything can absorb. **A trace therefore
needs a bound of its own**: a ring buffer of the last N arrivals is the obvious
one and keeps the debugger's actual use - "what did it just do" - while a
per-node arrival *count* alongside it keeps "iteration 3 of 5" exact without
storing 3 entries. Whatever the choice, it has to be made deliberately rather
than inherited from a limit that no longer exists.

### Yield discipline: copy Scratch's shape, not a sleep

Item 6 of the document's semantics - a minimum yield per loop iteration - is
right and underspecified. Scratch's sequencer is the well-tested version of this
idea, and its shape is worth borrowing rather than reinventing:

- yield points are a **fixed, named set**, not "wherever it seemed slow": every
  loop boundary, every wait, every blocking primitive
- a script that does not yield is bounded anyway - `WARP_TIME = 500` ms caps how
  long execution may run without giving control back
- the scheduler works to a budget, 75% of the stepping interval, rather than
  running until finished

Keypress's version is smaller, because there is one token rather than many
threads, but the two properties to keep are the same: yield points are declared
rather than discovered, and there is a wall-clock bound on running without one.
The thing being protected is not a frame rate - it is the OS input queue, which a
tight loop of instantaneous nodes will fill faster than the target application
drains it.

**Adopted, both halves.** The declared point is `minLoopIterationYield` in
`backend/actions_loop.go`; the bound is `maxWalkWithoutYield` in
`backend/execution.go`, 500ms after Scratch's `WARP_TIME`, applied in the walk
driver and reset by anything that really blocked. The second half was what the
first was quietly being credited with: a cycle with no Loop Start in it, or one
through a Loop Start that always leaves by `done`, reaches no declared yield
point at all and used to spin at full speed. See item 6a of the semantics in
`control-flow-interpreter.md`.

### Write down that sequential fan-out is what avoids the cloning problem

The document justifies sequential fan-out on the grounds that the concurrency was
mostly theatre. True, but it undersells it. Node-RED clones every message on
fan-out and Automa clones worker state per branch, both because parallel branches
share mutable data and neither wants branch A's writes visible to branch B.

Under one token, the run-state map is shared by construction and there is nothing
to clone, because branches cannot interleave. That is a real property worth
recording next to the Parallel-node refusal - it is what a Parallel node would
cost, stated in advance, rather than a discovery made while building one.

### Recording needs a reduction pass that Automa never had to write

From the earlier survey, and now with the reason: Automa's recorder gets away
without merging because it has a DOM. It debounces at capture - 300 ms on text
input, 500 ms on scroll - and reads the field's finished value off `target.value`.
`addBlock.js` performs no merging or deduplication whatsoever; consecutive
keystroke blocks are merely tagged with a shared `groupId`.

A desktop recorder has none of that. There is no `target.value` to read, only a
stream of key events, so the merge that Automa outsources to the browser has to
be written here. Two consequences:

- **"Type a string" is a recording prerequisite**, not the unrelated usability
  item it is filed as today. Without it the merge has nowhere to put its result
  and `hello@example.com` records as eighteen nodes.
- Automa's `groupId` is worth stealing regardless. Tagging the nodes that came
  from one recorded gesture is most of what "record and see it as a group of
  nodes" means, and it costs a field.

## Anti-goals

Godot shipped VisualScript, then removed it in 4.0. The post-mortem is blunt: it
never gained traction, the path to improve it was never clear, and the approach
taken from the start was simply not the right one. Users who wanted visual
scripting kept choosing GDScript, because it was easier to learn than the graph
was.

The distinction that makes Keypress not that: Godot's graph was a general-purpose
programming language drawn with boxes, competing with a text language that was
better at the same job. Keypress's graph is domain-specific - it draws input
events against a screen - and there is no text language it is losing to.

That distinction is only preserved by declining things. The pressure will come as
individually reasonable requests: expressions in fields, types on values,
arbitrary variables, user-defined functions. Each is a step from "a picture of
what will happen to my mouse" toward "a programming language, but worse". The
document's existing restraint - no Parallel node, Break and Continue deferred,
run state deliberately tiny, comments not nodes - is the correct instinct and
should be treated as policy rather than as a set of not-yet-done items.

## Additions to the test plan

Beside the fixtures already listed in `control-flow-interpreter.md`:

- creating a Loop Start creates its Loop End and the back edge between them, and
  `serializeMacro` round-trips the pairing
- a Loop End whose partner has been deleted ends that path rather than panicking,
  and the editor refuses to leave one half of a pair behind in the first place
- a nested loop's Break exits the innermost enclosing pair, not the outermost
- ~~a per-loop cap stops that loop and names it, while the run-wide budget still
  catches a runaway with no loop node in it at all~~ - withdrawn with
  recommendation 2; there is neither a cap nor a budget to test
- a node with a retry count of 3 and an on-error edge is attempted 3 times before
  the edge is taken, and the same node without the edge is still attempted 3 times
- the trace records one entry per arrival, so a loop of 4 iterations over 3 nodes
  produces 12 entries in walk order - and an endless loop does not grow it without
  limit, because the trace is bounded by something of its own now that the
  iteration budget is gone

## Sources

Read rather than cited from documentation, except where noted:

- [AutomaApp/automa](https://github.com/AutomaApp/automa) - `src/workflowEngine/WorkflowWorker.js`, `blocksHandler/handlerLoopData.js`, `blocksHandler/handlerLoopBreakpoint.js`, `src/content/services/recordWorkflow/recordEvents.js`, `addBlock.js`
- [scratchfoundation/scratch-vm](https://github.com/scratchfoundation/scratch-vm) - `src/engine/sequencer.js`
- [n8n-io/n8n](https://github.com/n8n-io/n8n) - `packages/core/src/execution-engine/workflow-execute.ts`; the retry/on-error conflict is [issue #10763](https://github.com/n8n-io/n8n/issues/10763)
- [Node-RED message cloning](https://nodered.org/blog/2019/09/13/cloning-messages)
- [Blender Repeat Zone](https://docs.blender.org/manual/en/latest/modeling/geometry_nodes/utilities/repeat_zone.html) and the [implementing PR](https://projects.blender.org/blender/blender/pulls/109164)
- [Blueprint debugging](https://dev.epicgames.com/documentation/en-us/unreal-engine/blueprint-debugger-in-unreal-engine) and [Macros](https://dev.epicgames.com/documentation/unreal-engine/macros-in-unreal-engine)
- [UiPath workflow design](https://docs.uipath.com/studio/standalone/2024.10/user-guide/workflow-design) - the GoTo warning
- [open-rpa/openrpa](https://github.com/open-rpa/openrpa) - Windows Workflow Foundation activity tree
- [Godot 4.0 will discontinue VisualScript](https://godotengine.org/article/godot-4-will-discontinue-visual-scripting/)
