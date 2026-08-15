// frontend\src\lib\utils\nodeLabels.ts
//
// Naming the nodes of a flow, and putting lists of them into the order the run
// walks the graph. Used by `Flow.svelte`, which holds the label map for the
// duration of one run and hands it back to the two lookups at the bottom of
// this file.

/**
 * Human-readable name for each node type, keyed by the registry key from
 * `customNodes/nodeTypes.ts` - the string that ends up in a saved node's
 * `type` and that the Go dispatcher in `tasks.go` switches on.
 *
 * The values are copies of the node components' own `title` prop defaults
 * (`StartNode.svelte`, `DelayNode.svelte`, `MouseClickNode.svelte`,
 * `MouseMoveNode.svelte`, `ColorPickerNode.svelte`, `KeyPressNode.svelte`),
 * so the status panel calls a node exactly what the node's own header calls
 * it on the canvas. Those components are the source of truth: when one of
 * them renames itself, this table follows. A type missing from here still
 * gets a label - see `buildNodeLabels`.
 */
export const NODE_TYPE_TITLES: Record<string, string> = {
  StartNode: "Start",
  DelayNode: "Delay",
  MouseClickNode: "Mouse Click",
  MouseMoveNode: "Mouse Move",
  ColorPickerNode: "Wait For Color",
  KeyPressNode: "Keypress",
};

/** Shown when a reported node is not in the graph the run was started from. */
export const UNKNOWN_NODE_LABEL = "Unknown node";

/**
 * The subset of a node this file needs to label it. Deliberately structural
 * rather than `FlowNode`: `toObject()` hands back Svelte Flow's own `Node`,
 * whose `type` is optional.
 */
export type LabellableNode = {
  id: string;
  type?: string;
  position: { x: number; y: number };
};

/**
 * The subset of an edge this file needs. Structural for the same reason:
 * `toObject()` returns Svelte Flow's `Edge`, and only the direction matters
 * here.
 */
export type LabellableEdge = {
  source: string;
  target: string;
};

/**
 * What a node is called on screen, plus its place in the sequence.
 *
 * `step` is the number in the label, kept separately so lists of reported
 * ids can be put back into flow order without parsing the text.
 */
export type NodeLabel = {
  label: string;
  step: number;
};

/**
 * A flow reduced to the shape both passes below work over. Built once, by
 * `shapeGraph`, so that "what the run can reach" and "what order the nodes come
 * in" can never read the same flowchart two different ways.
 */
type FlowGraph = {
  /** Every node of the flow, by id. */
  byId: Map<string, LabellableNode>;
  /** The node the run begins at, absent if the flow has none. */
  startId: string | undefined;
  /** Forward adjacency: which nodes each node hands on to. */
  successors: Map<string, string[]>;
  /** The same edges backwards: which nodes each node waits for. */
  predecessors: Map<string, string[]>;
  /**
   * Every node that takes part in an edge, recorded before any edge is dropped
   * from the adjacency, so it matches the backend's own "connected" test in
   * `warnAboutSkippedNodes`.
   */
  wired: Set<string>;
};

/**
 * Shapes the raw nodes and edges into a `FlowGraph`, keeping only the edges
 * that actually join two nodes of this flow and dropping the ones pointing
 * *into* the Start node - which `StartExecution` enqueues unconditionally, so
 * they are not prerequisites and must not be read as any.
 */
function shapeGraph(
  nodes: LabellableNode[],
  edges: LabellableEdge[]
): FlowGraph {
  const byId = new Map<string, LabellableNode>();
  for (const node of nodes) byId.set(node.id, node);

  const startId = nodes.find((node) => node.type === "StartNode")?.id;

  const successors = new Map<string, string[]>();
  const predecessors = new Map<string, string[]>();
  const wired = new Set<string>();
  const link = (map: Map<string, string[]>, from: string, to: string) => {
    const list = map.get(from);
    if (list) list.push(to);
    else map.set(from, [to]);
  };
  for (const edge of edges) {
    if (!byId.has(edge.source) || !byId.has(edge.target)) continue;
    wired.add(edge.source);
    wired.add(edge.target);
    // The Start node runs regardless of what points at it.
    if (edge.target === startId) continue;
    link(successors, edge.source, edge.target);
    link(predecessors, edge.target, edge.source);
  }

  return { byId, startId, successors, predecessors, wired };
}

/**
 * What the run can get to, walked exactly like `reachableFrom` in
 * `backend/execution.go`: forwards from the Start node, each node expanded at
 * most once so a cycle terminates the walk instead of spinning in it, and edges
 * to ids that are not nodes of this flow ignored (`shapeGraph` has already
 * dropped those).
 *
 * "Exactly like" is asserted rather than hoped for. Both walks are run over the
 * shared fixtures in `backend/testdata/reachability` - see the README there -
 * by `nodeLabels.parity.test.ts` on this side and
 * `TestReachableFromAgreesWithTheSharedFixtures` on the Go side. If they ever
 * disagree the status panel starts describing a run that did not happen, naming
 * the wrong nodes as skipped or sorting a stall report wrongly, and those two
 * tests are what catches it.
 */
function walkForwards({ startId, successors }: FlowGraph): Set<string> {
  const reachable = new Set<string>();
  if (startId === undefined) return reachable;

  reachable.add(startId);
  const queue = [startId];
  for (let i = 0; i < queue.length; i++) {
    for (const next of successors.get(queue[i]) ?? []) {
      if (reachable.has(next)) continue;
      reachable.add(next);
      queue.push(next);
    }
  }
  return reachable;
}

/**
 * The set of nodes a run of this flow would actually visit - the frontend's
 * answer to the question `reachableFrom` answers in Go.
 *
 * `buildNodeLabels` does not call this: it needs the shaped graph for its
 * ordering pass as well, so it shapes the flow once and walks it itself. Both
 * routes go through the same `shapeGraph` and `walkForwards`, so this function
 * cannot answer differently from the labelling - which is the point, since it
 * is what the parity test against the Go engine runs.
 */
export function reachableNodes(
  nodes: LabellableNode[],
  edges: LabellableEdge[]
): Set<string> {
  return walkForwards(shapeGraph(nodes, edges));
}

/**
 * Names every node by its type - `Start`, `Delay`, `Mouse Click` - and records
 * its position in the order the run walks the graph. That position is never
 * displayed; it exists so a message listing several nodes can be sorted into
 * the order they would have run (see `inFlowOrder`). Two delays therefore read
 * alike in the panel: the canvas highlight and the id tooltip are what tell
 * them apart.
 *
 * The order is built to agree with the engine (`execution.go`) rather than to
 * merely look plausible:
 *
 * - Edges are directed `source -> target`, and the run starts at the Start
 *   node, which `StartExecution` enqueues unconditionally - so edges pointing
 *   *into* Start are ignored here, exactly as they are there.
 * - A node's prerequisites are its predecessors that the run can also reach;
 *   `StartExecution` prunes edges out of dead branches for the same reason,
 *   which is why prerequisites are counted within a group rather than
 *   globally.
 * - `canEnqueue` waits for *every* prerequisite, so a join must be ordered
 *   after all of its inputs. Plain breadth-first would place it as soon as
 *   the first input was seen, so depth here is longest-path: a node sits one
 *   past the deepest node feeding it. Nodes that come ready together - the
 *   branches out of one node - are then ordered top to bottom by canvas
 *   position (y, then x, then id, purely so it is deterministic).
 *
 * Cycles: the depth pass is Kahn's algorithm, which only ever releases a node
 * once its last prerequisite has been released, so a node inside a cycle is
 * never released and the walk terminates instead of spinning. Whatever is
 * left over at the end is precisely the cyclic part - the engine would report
 * it as a stall - and it is appended in canvas order so it is still named.
 *
 * Nodes the run cannot reach still need a step, since they are named in the
 * skipped-nodes warning and that list is sorted by it. They continue the
 * sequence after everything reachable, one dead branch at a time:
 * each weakly connected group of unreachable-but-wired nodes is walked in the
 * same longest-path order, groups taken top to bottom by their highest node.
 * Nodes in no edge at all are last - the backend never reports them, and they
 * are usually just something the user has only dropped on the canvas so far.
 */
export function buildNodeLabels(
  nodes: LabellableNode[],
  edges: LabellableEdge[]
): Map<string, NodeLabel> {
  const graph = shapeGraph(nodes, edges);
  const { byId, successors, predecessors, wired } = graph;

  const positionOf = (id: string) => byId.get(id)?.position ?? { x: 0, y: 0 };
  const canvasOrder = (a: string, b: string) => {
    const pa = positionOf(a);
    const pb = positionOf(b);
    return pa.y - pb.y || pa.x - pb.x || a.localeCompare(b);
  };

  const reachable = walkForwards(graph);

  /**
   * One set of nodes in longest-path order, prerequisites counted only within
   * the set. Anything the walk cannot release (a cycle, or a node behind one)
   * is appended in canvas order rather than dropped or looped over.
   */
  const orderGroup = (members: Set<string>): string[] => {
    const waiting = new Map<string, number>();
    for (const id of members) {
      let count = 0;
      for (const pred of predecessors.get(id) ?? []) {
        if (members.has(pred)) count += 1;
      }
      waiting.set(id, count);
    }

    const depth = new Map<string, number>();
    const released: string[] = [];
    for (const id of members) {
      if (waiting.get(id) === 0) {
        depth.set(id, 0);
        released.push(id);
      }
    }
    for (let i = 0; i < released.length; i++) {
      const current = released[i];
      const nextDepth = (depth.get(current) ?? 0) + 1;
      for (const next of successors.get(current) ?? []) {
        if (!members.has(next)) continue;
        const best = depth.get(next);
        if (best === undefined || best < nextDepth) depth.set(next, nextDepth);
        const left = (waiting.get(next) ?? 0) - 1;
        waiting.set(next, left);
        if (left === 0) released.push(next);
      }
    }

    const ordered = released
      .slice()
      .sort(
        (a, b) => (depth.get(a) ?? 0) - (depth.get(b) ?? 0) || canvasOrder(a, b)
      );
    const releasedSet = new Set(released);
    const stuck = [...members]
      .filter((id) => !releasedSet.has(id))
      .sort(canvasOrder);
    return [...ordered, ...stuck];
  };

  const order: string[] = [];
  if (reachable.size > 0) order.push(...orderGroup(reachable));

  // Dead branches, one weakly connected group at a time.
  const deadEnds = [...byId.keys()].filter(
    (id) => !reachable.has(id) && wired.has(id)
  );
  const grouped = new Set<string>();
  for (const seed of deadEnds.slice().sort(canvasOrder)) {
    if (grouped.has(seed)) continue;
    const group = new Set<string>([seed]);
    grouped.add(seed);
    const frontier = [seed];
    for (let i = 0; i < frontier.length; i++) {
      const current = frontier[i];
      const neighbours = [
        ...(successors.get(current) ?? []),
        ...(predecessors.get(current) ?? []),
      ];
      for (const neighbour of neighbours) {
        if (reachable.has(neighbour) || group.has(neighbour)) continue;
        group.add(neighbour);
        grouped.add(neighbour);
        frontier.push(neighbour);
      }
    }
    order.push(...orderGroup(group));
  }

  // Anything in no edge at all.
  order.push(
    ...[...byId.keys()]
      .filter((id) => !reachable.has(id) && !wired.has(id))
      .sort(canvasOrder)
  );

  const labels = new Map<string, NodeLabel>();
  order.forEach((id, index) => {
    // An unregistered type has no title of its own. Its registry key is poor
    // UI text, but it still tells the user far more than a raw id.
    const type = byId.get(id)?.type ?? "";
    const title = NODE_TYPE_TITLES[type] ?? (type || UNKNOWN_NODE_LABEL);
    // The step is still computed and kept, but deliberately not shown: it is
    // what puts multi-node messages into flow order below. Status messages
    // name a node by type alone.
    labels.set(id, { label: title, step: index + 1 });
  });
  return labels;
}

/**
 * Label for a node id reported by the backend. Falls back to an obviously
 * degraded placeholder rather than `undefined` if the id is not in the graph
 * this run started from (a node deleted mid-run, or an event left over from a
 * run this component did not start).
 *
 * `labels` is a parameter rather than a module-level map: it is the snapshot
 * `Flow.svelte` froze when the run started, and it belongs to that component's
 * state - see the thin wrapper there that closes over it.
 */
export function nodeLabel(
  labels: Map<string, NodeLabel>,
  nodeId: string
): string {
  return labels.get(nodeId)?.label ?? UNKNOWN_NODE_LABEL;
}

/**
 * Puts a list of reported ids back into flow order, so a message listing
 * several nodes reads in the order the flow would have run them.
 *
 * Sorting the finished labels as text would not do: labels are node types, so
 * a graph with three delays yields three identical strings and text order says
 * nothing about when they run. Unknown ids go last, in a stable order of their
 * own.
 *
 * Takes the label map for the same reason `nodeLabel` does.
 */
export function inFlowOrder(
  labels: Map<string, NodeLabel>,
  nodeIds: string[]
): string[] {
  const step = (id: string) => labels.get(id)?.step ?? Number.MAX_SAFE_INTEGER;
  return nodeIds.slice().sort((a, b) => step(a) - step(b) || a.localeCompare(b));
}
