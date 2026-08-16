# Walk fixtures

Graphs, the order the execution token runs their nodes in, and the nodes the
multipath lint flags in each one.

The sibling directory `../reachability` answers a different question - "which
nodes can a run get to at all" - and is read by a different pair of tests. This
one is about the walk itself: order, repeats, and how a loop ends.

**Two test suites read every file in this directory:**

- `backend/execution_test.go` - `TestTheWalkAgreesWithTheSharedFixtures`, which
  drives `run.step` over each fixture and collects what it ran.
- the frontend's parity test, which runs the TypeScript walk over the same
  files.

They exist for the same reason the reachability fixtures do: the walk is
described in two places, and if the two disagree the status panel narrates a run
that did not happen.

**Dropping a new `*.json` file in here extends both suites.** Neither test
enumerates the fixtures by name; both glob the directory. Nothing to edit.

## Format

One JSON object per file:

```jsonc
{
  // The claim the fixture makes. Printed on failure, so write it as a claim
  // rather than as a label.
  "description": "A linear macro runs its nodes in order, once each.",

  // The graph, in exactly the shape a saved macro has on disk - see FlowData
  // in backend/types.go.
  "flow": {
    "nodes": [
      { "id": "start", "type": "StartNode", "position": { "x": 0, "y": 0 }, "data": {} }
    ],
    "edges": [
      { "id": "e1", "source": "start", "target": "a", "sourceHandle": "right" }
    ]
  },

  // The node ids the token runs, in order, WITH repeats. A node reached twice
  // appears twice - that is the point of this list, since per-arrival execution
  // is what the interpreter changed.
  "visits": ["start", "a"],

  // The ids the multipath lint flags, in flowchart order. Omit or [] for none.
  "multipath": [],

  // Optional. The output handle a node leaves by, for the nodes that pick one;
  // anything not listed leaves by "", which means "the only output". Both
  // suites drive the walk with an executor that reads this instead of running
  // anything, so a fixture states the handle its node picked rather than
  // arranging for a handler to pick it. Two node types really do pick one: a
  // BranchNode leaves by "true" or "false", and a ColorPickerNode leaves by
  // "timeout" when it timed out and the macro drew that edge.
  //
  // A LoopStartNode is not one of them and cannot be: "body" or "done" is the
  // answer to "how many times have I been here", which is what these fixtures
  // exist to pin, and a fixture that stated "body" would loop forever. The Go
  // suite runs that node's real handler instead. A LoopEndNode needs no entry
  // either - it leaves by "", which is what an unlisted node gets anyway.
  "outputs": { "branch": "out-2" }
}
```

There is no `budget` field. There used to be: a run-wide iteration budget
stopped a walk after 100000 nodes, and a fixture containing a loop had to lower
it so that `visits` could be a finite list. Phase 4 removed the budget - a loop
with no way out is a feature now, not a runaway - so **a fixture with a loop in
it must be one that ends by its own count or condition**, or the suite hangs
rather than failing. The frontend's copy of this file format still declares
`budget?: number`; nothing sets it.

Notes for anyone adding a case:

- The Start node is **the first node whose `type` is `"StartNode"`**, which is
  what `findStartNode` in Go does. The walk begins there.
- **Fan-out is by output handle.** An output handle takes exactly one edge, and
  a node fans out by being a Sequence node with several handles; the walk takes
  a node's outgoing edges in ascending `sourceHandle` order, depth-first, so the
  first target's whole subtree runs before the second target starts. `position`
  does **not** affect the walk - give every node one anyway, so the fixture is a
  valid macro that the frontend could draw, but do not encode an expectation in
  it. Neither does node id.
- `next == ""` takes **every** outgoing edge whatever its handle is named, which
  is why every fixture here can carry the `sourceHandle: "right"` that the app
  really draws. A named `next` takes only the edge on that handle, and a `next`
  naming a handle with no edge ends that path normally.
- Node `type` is free, and nothing here is really executed: both suites drive
  the walk with an executor of their own, so a fixture full of
  `MouseClickNode`s moves no mouse. (This is why these fixtures do not follow
  `execution_test.go`'s "every test node is a StartNode" rule - nothing reaches
  robotgo from here.)
- `data` is only there to make the node a legal `Node`.
- **A loop is a pair of nodes and the editor owns the edge between them.** A
  `LoopStartNode` has target handle `left` and source handles `body` and `done`;
  its `LoopEndNode` has target handle `left` and one source handle, `back`,
  wired to the start. Both halves carry the editor's `loopId` in `data` here, to
  document that the backend ignores it: frames are keyed by the Loop Start's
  node id, and nothing in either walk looks a partner up. Draw a fixture's loop
  the way the editor would, so that these files stay macros someone could have
  drawn.
- **A fixture whose loop skips its body cannot live here.** The frontend's half
  of the parity suite compares the ids in first-arrival order against the ones
  its *static* labeller numbers, and those two lists have to be equal for a
  fixture that names no `outputs`. A count of zero, or a condition that already
  holds, strands the body and the Loop End: numbered by the labeller, never
  reached by the token. Those cases are pinned in Go instead, in
  `actions_loop_test.go`. A fixture that does name `outputs` is only checked for
  containment, so it may strand nodes on the branch not taken.
- The multipath lint counts a node's incoming edges from sources reachable from
  Start, ignoring back-edges and ignoring anything wired into the Start node
  itself. Back-edges are classified by a depth-first search from Start that
  follows each node's outgoing edges **in the order the `edges` array lists
  them**; an edge whose target is on that search's stack is a back-edge. Order
  your `edges` array accordingly if a fixture depends on it.
