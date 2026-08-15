# Reachability fixtures

Graphs, and the set of nodes a run can actually get to in each one.

**Two test suites read every file in this directory:**

- `backend/execution_test.go` - `TestReachableFromAgreesWithTheSharedFixtures`,
  which runs `reachableFrom` over each fixture.
- `frontend/src/lib/utils/nodeLabels.parity.test.ts`, which runs the TypeScript
  walk exported from `nodeLabels.ts` over the same files.

They exist because the walk is implemented twice - once in Go to decide which
nodes run, once in TypeScript to decide how the status panel names and orders
them. If the two ever disagree, the panel describes a run that did not happen:
it names the wrong nodes as skipped, or sorts a stall report wrongly. Nothing
else asserts they agree.

**Dropping a new `*.json` file in here extends both suites.** Neither test
enumerates the fixtures by name; both glob the directory. Nothing to edit.

## Format

One JSON object per file:

```jsonc
{
  // What this graph is for. Read by humans only, but printed on failure, so
  // write it as the claim the fixture makes.
  "description": "A join runs after both of its branches.",

  // The graph, in exactly the shape a saved macro has on disk - see FlowData
  // in backend/types.go. It unmarshals straight into FlowData, and the
  // frontend reads `nodes` and `edges` as-is.
  "flow": {
    "nodes": [
      { "id": "start", "type": "StartNode", "position": { "x": 0, "y": 0 }, "data": {} }
    ],
    "edges": [{ "id": "e1", "source": "start", "target": "a" }]
  },

  // Every node id the run can reach from the Start node, in any order. Both
  // suites compare it as a set.
  "reachable": ["start", "a"]
}
```

Notes for anyone adding a case:

- The Start node is **the first node whose `type` is `"StartNode"`**, which is
  what `findStartNode` in Go and the `nodes.find(...)` in `nodeLabels.ts` both
  do. A fixture with no such node expects `"reachable": []`.
- `position` matters to the frontend (it orders ties by canvas position) and is
  ignored by the walk itself. Give every node one anyway, so the fixture is a
  valid macro.
- `data` is only there to make the node a legal `Node`; the walk never looks at
  it.
- Node `type` is otherwise free. The Go engine would really execute a
  `KeyPressNode`, but nothing here executes anything - these fixtures are fed to
  the reachability walk directly, not to `startFlow`.
